package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func (s *Server) handleAlertmanager(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		// Counted here because a refused token is otherwise a 401 nobody is
		// watching, and "the sender's secret is wrong" looks exactly like "the
		// sender stopped sending".
		s.metrics.WebhookReceived(ctx, "alertmanager", "refused")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload models.AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	for _, alert := range payload.Alerts {
		model := models.Alert{
			Source:      "alertmanager",
			Status:      alert.Status,
			Labels:      alert.Labels,
			Annotations: alert.Annotations,
			StartsAt:    alert.StartsAt,
			EndsAt:      alert.EndsAt,
			Generator:   alert.GeneratorURL,
			Fingerprint: alert.Fingerprint,
		}
		s.metrics.WebhookReceived(ctx, model.Source, model.Status)
		if err := s.processAlert(ctx, model); err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUniversal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		s.metrics.WebhookReceived(ctx, "universal", "refused")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload models.UniversalWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	model := models.Alert{
		Source:      "universal",
		Status:      payload.Status,
		Labels:      payload.Labels,
		Annotations: payload.Annotations,
		StartsAt:    payload.StartsAt,
		EndsAt:      payload.EndsAt,
		Generator:   payload.Generator,
		Fingerprint: payload.Fingerprint,
	}

	s.metrics.WebhookReceived(ctx, model.Source, model.Status)
	if err := s.processAlert(ctx, model); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) processAlert(ctx context.Context, alert models.Alert) error {
	if alert.Fingerprint == "" {
		alert.Fingerprint = hashFingerprint(alert)
	}
	if alert.Status != "firing" && alert.Status != "resolved" {
		return fmt.Errorf("unknown status: %s", alert.Status)
	}

	result, err := s.router.Plan(ctx, alert.Labels)
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	switch result.Reason {
	case routing.ReasonNoRoutes:
		return errors.New("no routes configured")
	case routing.ReasonNone:
		return errors.New("no matching route and no default route")
	}

	if alert.Status == "resolved" {
		return s.resolveAlert(ctx, alert, result.Deliveries)
	}

	// One channel refusing the message must not cost the others theirs, so every
	// delivery is attempted and the failures are reported together.
	var failures []error
	for _, delivery := range result.Deliveries {
		if err := s.deliver(ctx, alert, delivery); err != nil {
			failures = append(failures, fmt.Errorf("route %s: %w", delivery.RouteName, err))
		}
	}
	return errors.Join(failures...)
}

// errCardInFlight says another instance is inside its Graph call for this very
// card. It is an error rather than a silent success because the payload that
// lost the race may carry something the winner's did not: a 502 asks the sender
// to retry, and the retry updates the card the winner created.
var errCardInFlight = errors.New("another instance is posting this card")

// claimTTL is how long a claim may go uncompleted before another instance may
// take it over. It has to outlast a Graph call comfortably: too short and two
// instances post the same card, which is the thing claiming exists to prevent.
func (s *Server) claimTTL() time.Duration {
	graphTimeout := time.Duration(s.cfg.Graph.TimeoutSec) * time.Second
	if ttl := 3 * graphTimeout; ttl > 30*time.Second {
		return ttl
	}
	return 30 * time.Second
}

// deliver posts or updates the one card this delivery names.
//
// The card is claimed before the Graph call and the claim completed after it,
// with no transaction spanning the two: a transaction held across a network
// call pins a connection for as long as Microsoft takes to answer, and tells us
// nothing if the process dies holding it. The claim row does — it is the only
// record that a post was in flight, which is what lets the next attempt take
// over rather than wait forever or post a second card.
func (s *Server) deliver(ctx context.Context, alert models.Alert, delivery routing.Delivery) error {
	destination, msg, err := s.render(ctx, alert, delivery)
	if err != nil {
		return err
	}

	now := s.now()
	claim := models.AlertClaim{
		Fingerprint: alert.Fingerprint,
		TeamID:      destination.TeamID,
		ChannelID:   destination.ChannelID,
		Status:      alert.Status,
		Owner:       uuid.NewString(),
		At:          now,
		StaleBefore: now.Add(-s.claimTTL()),
	}

	card, outcome, err := s.store.ClaimActiveAlert(ctx, claim)
	if err != nil {
		return fmt.Errorf("claim active alert: %w", err)
	}

	switch outcome {
	case store.ClaimPosted:
		// The card for this channel already exists, so the alert is an update
		// to it rather than a second card.
		if err := s.graph.UpdateMessage(card.TeamID, card.ChannelID, card.MessageID, msg); err != nil {
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			return fmt.Errorf("graph update: %w", err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		return s.store.TouchActiveAlert(ctx, card, alert.Status, s.now())
	case store.ClaimHeld:
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		return errCardInFlight
	}

	messageID, err := s.graph.PostMessage(destination.TeamID, destination.ChannelID, msg)
	if err != nil {
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		// The claim is ours and nothing was posted under it, so it goes back
		// now rather than making the next attempt wait out the cutoff.
		if release := s.store.ReleaseActiveAlertClaim(ctx, claim); release != nil {
			logError("release alert claim", release)
		}
		return fmt.Errorf("graph post: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)

	if err := s.store.CompleteActiveAlertClaim(ctx, claim, messageID, s.now()); err != nil {
		if errors.Is(err, store.ErrClaimLost) {
			// The window this cannot close: the post succeeded, and by the time
			// it was recorded the row belonged to somebody else's card. Graph
			// has no idempotency key for a channel message, so the card just
			// posted cannot be adopted or withdrawn -- only reported.
			logError("orphaned card", fmt.Errorf("%s/%s message %s: %w",
				destination.TeamID, destination.ChannelID, messageID, err))
		}
		return err
	}
	return nil
}

// resolveAlert walks the cards that were posted rather than the plan, because
// the routes may have changed since: a card in a channel the plan no longer
// names still has to stop saying the alert is firing.
func (s *Server) resolveAlert(ctx context.Context, alert models.Alert, plan []routing.Delivery) error {
	active, err := s.store.ListActiveAlerts(ctx, alert.Fingerprint)
	if err != nil {
		return fmt.Errorf("active alert lookup: %w", err)
	}

	var failures []error
	for _, card := range active {
		// A claim in flight has no card yet, so there is nothing to edit and
		// nothing to forget: deleting it would strand the message the other
		// instance is about to post, still saying the alert fires. Reporting
		// it retryable is the same reasoning as the failed update below --
		// the sender's retry is what puts it right, by which time the card
		// exists and this becomes an ordinary resolve.
		if !card.Posted() {
			failures = append(failures, fmt.Errorf("channel %s: %w", card.ChannelID, errCardInFlight))
			continue
		}

		delivery, err := s.deliveryFor(ctx, card, plan)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		_, msg, err := s.render(ctx, alert, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// The row outlives a failed update on purpose: it is the only record that
		// this channel still holds a card claiming the alert fires, and the
		// sender's retry is what puts that right.
		if err := s.graph.UpdateMessage(card.TeamID, card.ChannelID, card.MessageID, msg); err != nil {
			failures = append(failures, fmt.Errorf("graph update: %w", err))
			continue
		}
		// Deleting by message id, so a resolve that raced a refire forgets the
		// card it just edited rather than the newer one that replaced it.
		if err := s.store.DeleteActiveAlertCard(ctx, card.Fingerprint, card.TeamID, card.ChannelID, card.MessageID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// The delivery that owns a card is the one pointing at its channel; a card the
// plan no longer covers is rendered with the first template the plan names,
// which is better than leaving it claiming the alert still fires.
func (s *Server) deliveryFor(ctx context.Context, card models.ActiveAlert, plan []routing.Delivery) (routing.Delivery, error) {
	for _, delivery := range plan {
		destination, err := s.store.GetDestination(ctx, delivery.DestinationID)
		if err != nil {
			continue
		}
		if destination.TeamID == card.TeamID && destination.ChannelID == card.ChannelID {
			return delivery, nil
		}
	}
	if len(plan) == 0 {
		return routing.Delivery{}, fmt.Errorf("no route renders the card in channel %s", card.ChannelID)
	}
	return plan[0], nil
}

func (s *Server) render(ctx context.Context, alert models.Alert, delivery routing.Delivery) (models.Destination, graph.Message, error) {
	template, err := s.store.GetTemplate(ctx, delivery.TemplateID)
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageTemplate)
		return models.Destination{}, graph.Message{}, fmt.Errorf("template: %w", err)
	}
	destination, err := s.store.GetDestination(ctx, delivery.DestinationID)
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageDestination)
		return models.Destination{}, graph.Message{}, fmt.Errorf("destination: %w", err)
	}

	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Alert: alert,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageRender)
		return models.Destination{}, graph.Message{}, fmt.Errorf("render: %w", err)
	}

	// The summary line is the template's to decide now; templates.RenderMessage
	// falls back to the one this service used to hardcode.
	msg := graph.Message{Title: rendered.Title, Text: rendered.Text}
	if len(rendered.Card) > 0 {
		msg.Cards = []json.RawMessage{rendered.Card}
	}
	return destination, msg, nil
}

// routeLabel is what a delivery is counted under. The name is what an operator
// recognises, but nothing requires a route to have one, and an empty attribute
// is a row in a dashboard that says nothing.
func routeLabel(delivery routing.Delivery) string {
	if delivery.RouteName != "" {
		return delivery.RouteName
	}
	return delivery.RouteID
}

func hashFingerprint(alert models.Alert) string {
	h := sha256.New()
	_, _ = h.Write([]byte(alert.Source))
	_, _ = h.Write([]byte(alert.Generator))
	_, _ = h.Write([]byte(alert.StartsAt.Format(time.RFC3339)))
	keys := make([]string, 0, len(alert.Labels))
	for key := range alert.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := alert.Labels[key]
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte("="))
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}
