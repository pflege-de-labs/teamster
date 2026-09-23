package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/teamsv2"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// The source these receipts are counted under, beside "alertmanager" and
// "universal".
const teamsV2Source = "teamsv2"

// A Teams message caps out well below this; the limit is here because the body
// arrives from an unauthenticated caller and has to be read before the token in
// the path can be checked.
const maxTeamsV2Bytes = 128 << 10

// slugPattern is what a path segment may be. Keeping it to this means a URL can
// be read out of an access log, typed into a sender's configuration, and
// compared without case folding.
var slugPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// handleTeamsV2 answers the URL a Microsoft Teams webhook used to have.
//
// It runs no routing: the sender has already decided which channel the message
// goes to, which is the whole contract of the webhook this replaces. It renders
// the endpoint's template when it names one, and otherwise sends the payload as
// given with a hint card after it (ADR 0040). Nothing is written to
// active_alerts, because there is no fingerprint and no status -- there is
// nothing later to update or resolve.
func (s *Server) handleTeamsV2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	endpoint, err := s.teamsV2Endpoint(r)
	if err != nil {
		var refusal teamsV2Refusal
		if errors.As(err, &refusal) {
			s.metrics.WebhookReceived(ctx, teamsV2Source, refusal.status)
			writeJSONError(w, refusal.code, refusal.message)
			return
		}
		s.metrics.WebhookReceived(ctx, teamsV2Source, "error")
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxTeamsV2Bytes))
	if err != nil {
		s.metrics.WebhookReceived(ctx, teamsV2Source, "rejected")
		writeJSONError(w, http.StatusRequestEntityTooLarge, "payload too large")
		return
	}

	msg, err := teamsv2.Parse(body)
	if err != nil {
		s.metrics.WebhookReceived(ctx, teamsV2Source, "rejected")
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.metrics.WebhookReceived(ctx, teamsV2Source, "accepted")

	label := teamsV2Label(endpoint.TeamSlug, endpoint.ChannelSlug)
	destination, err := s.store.GetDestination(ctx, endpoint.DestinationID)
	if err != nil {
		s.metrics.DeliveryRecorded(ctx, label, metrics.OutcomeFailed)
		writeJSONError(w, http.StatusBadGateway, fmt.Sprintf("destination: %v", err))
		return
	}

	out, err := s.teamsV2Message(ctx, endpoint, body, msg)
	if err != nil {
		s.metrics.DeliveryRecorded(ctx, label, metrics.OutcomeFailed)
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}
	if _, err := s.graph.PostMessage(destination.TeamID, destination.ChannelID, out); err != nil {
		s.metrics.DeliveryRecorded(ctx, label, metrics.OutcomeFailed)
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	s.metrics.DeliveryRecorded(ctx, label, metrics.OutcomePosted)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// teamsV2Message is what an endpoint posts: its template rendered against the
// parsed payload, or the payload itself followed by the hint card.
func (s *Server) teamsV2Message(ctx context.Context, endpoint models.WebhookEndpoint, body []byte, msg teamsv2.Message) (graph.Message, error) {
	if endpoint.TemplateID == "" {
		hint, err := templates.HintCard(s.cfg.Server.ExternalURL, "/admin?edit=webhooks&id="+url.QueryEscape(endpoint.ID)+"#webhooks")
		if err != nil {
			return graph.Message{}, fmt.Errorf("hint: %w", err)
		}
		return graph.Message{Title: msg.Title, Text: msg.Text, Cards: append(msg.Cards, hint)}, nil
	}

	template, err := s.store.GetTemplate(ctx, endpoint.TemplateID)
	if err != nil {
		s.metrics.RenderFailed(ctx, endpoint.TemplateID, metrics.StageTemplate)
		return graph.Message{}, fmt.Errorf("template: %w", err)
	}
	// Parse already accepted the body, so it decodes.
	var payload any
	_ = json.Unmarshal(body, &payload)
	alert := models.Alert{Source: teamsV2Source, Title: msg.Title, Text: msg.Text}
	if len(msg.Cards) > 0 {
		alert.Card = msg.Cards[0]
	}
	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Alert:   alert,
		Now:     s.now().Format(time.RFC3339),
		Payload: payload,
	})
	if err != nil {
		s.metrics.RenderFailed(ctx, endpoint.TemplateID, metrics.StageRender)
		return graph.Message{}, fmt.Errorf("render: %w", err)
	}
	return channelMessage(rendered), nil
}

// handleTeamsV2Unknown answers anything under /teamsv2/ that is not a complete
// endpoint URL. A sender that lost a path segment gets the same answer as one
// naming a channel nobody configured, because from outside they are the same
// mistake.
func (s *Server) handleTeamsV2Unknown(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.metrics.WebhookReceived(r.Context(), teamsV2Source, "unknown")
	writeJSONError(w, http.StatusNotFound, "unknown endpoint")
}

// teamsV2Refusal is an answer owed to the sender rather than a failure of this
// service, so it carries the status code and the word a receipt is counted
// under.
type teamsV2Refusal struct {
	code    int
	status  string
	message string
}

func (e teamsV2Refusal) Error() string { return e.message }

// teamsV2Endpoint resolves the path to the endpoint it names.
//
// An unconfigured slug pair is a 404 and a wrong token a 401, which tells an
// unauthenticated caller which pairs exist. That is deliberate: the token still
// gates posting, and somebody wiring up a sender needs to be able to tell "I
// typed the channel wrong" apart from "I typed the secret wrong".
func (s *Server) teamsV2Endpoint(r *http.Request) (models.WebhookEndpoint, error) {
	team := strings.ToLower(r.PathValue("team"))
	channel := strings.ToLower(r.PathValue("channel"))
	token := r.PathValue("token")

	notFound := teamsV2Refusal{code: http.StatusNotFound, status: "unknown", message: "unknown endpoint"}
	if !slugPattern.MatchString(team) || !slugPattern.MatchString(channel) {
		return models.WebhookEndpoint{}, notFound
	}

	endpoint, err := s.store.GetWebhookEndpointBySlug(r.Context(), team, channel)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return models.WebhookEndpoint{}, notFound
		}
		return models.WebhookEndpoint{}, err
	}

	if !safeEqualsHash(token, endpoint.TokenHash) {
		return models.WebhookEndpoint{}, teamsV2Refusal{
			code: http.StatusUnauthorized, status: "refused", message: "invalid token",
		}
	}
	return endpoint, nil
}

// teamsV2Label is what a delivery through this path is counted under. The slugs
// are a fixed, configured set, so they bound the cardinality the way a route
// name does on the alert path.
func teamsV2Label(team, channel string) string {
	return teamsV2Source + ":" + team + "/" + channel
}

// safeEqualsHash compares a token against a stored digest. safeEquals cannot:
// it hashes both sides, and only one side is in the clear here.
func safeEqualsHash(token, digest string) bool {
	sum := hashToken(token)
	return subtle.ConstantTimeCompare([]byte(sum), []byte(digest)) == 1
}

// hashToken is unsalted on purpose. The token is 32 random bytes generated
// here, not a password somebody chose, so there is no dictionary to stretch
// against and a digest keeps the database from being a list of live secrets.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// newWebhookToken is the secret a sender puts in its URL: 256 bits, so the URL
// is no easier to guess than the opaque one it replaces.
func newWebhookToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(raw), nil
}
