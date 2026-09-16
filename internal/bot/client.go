// Package bot talks to the Bot Framework Connector API: sending and updating
// activities in a conversation the bot has already been added to. It is the
// outbound half of alerts reaching a person's chat (ADR 0026); the inbound
// endpoint that receives conversationUpdate and message activities ships in a
// later change.
package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// Client sends and updates Bot Framework activities. Unlike graph.Client it
// carries no base URL: every call's address comes from the ConversationReference
// handed to it, because a bot's conversations are scattered across whichever
// regional service URL Bot Framework assigned each one.
type Client struct {
	httpClient *http.Client
}

// ConversationReference is what Bot Framework calls the coordinates of one
// conversation. TenantID and AADObjectID are not needed to send a message, but
// are needed to recreate a conversation reference that has gone stale -- e.g.
// the bot was reinstalled, or Bot Framework says the conversation no longer
// exists -- so dropping them now would make that a breaking API change later.
type ConversationReference struct {
	ServiceURL     string
	ConversationID string
	BotChannelID   string
	TenantID       string
	AADObjectID    string
}

// Message is a rendered template on its way to a person's chat. Same shape as
// graph.Message, but a distinct type: the two clients speak different wire
// formats and are otherwise unrelated.
type Message struct {
	Title string
	Text  string
	Card  json.RawMessage
}

// activityAttachment is a Bot Framework activity attachment. Unlike Graph's,
// it sits at the activity root rather than being referenced from the body by
// an <attachment id="..."> tag.
type activityAttachment struct {
	ContentType string          `json:"contentType"`
	Content     json.RawMessage `json:"content"`
}

// activity is the Bot Framework envelope. Text is plain text or markdown, not
// HTML: sending graph's HTML here would show literal "<p><b>" in the chat.
type activity struct {
	Type        string               `json:"type"`
	Text        string               `json:"text"`
	Attachments []activityAttachment `json:"attachments,omitempty"`
}

// ResourceResponse is what both the send and update endpoints answer with.
type ResourceResponse struct {
	ID string `json:"id"`
}

// activity renders the message as a Bot Framework activity. A bold markdown
// first line stands in for Title, the closest chat equivalent of the preview
// line a Teams channel post gets from its own title; the attachment is
// omitted entirely rather than left empty when there is no card.
func (m Message) activity() activity {
	var text strings.Builder
	if m.Title != "" {
		text.WriteString("**")
		text.WriteString(m.Title)
		text.WriteString("**\n")
	}
	text.WriteString(m.Text)

	act := activity{Type: "message", Text: text.String()}
	if len(m.Card) > 0 {
		act.Attachments = []activityAttachment{{
			ContentType: "application/vnd.microsoft.card.adaptive",
			Content:     m.Card,
		}}
	}
	return act
}

// APIError carries what the Bot Connector said about a failed call, rather
// than a flat formatted string, so a caller can tell a permanent 403
// (MessageWritesBlocked: the person uninstalled or blocked the bot) from a
// transient 502 without parsing prose.
type APIError struct {
	StatusCode int
	// Code is error.code from the response body, e.g. "MessageWritesBlocked".
	Code string
	// SubCode is error.innerHttpError.code, or a top-level subCode -- the
	// Bot Connector uses both shapes depending on where the failure occurred.
	SubCode string
	// RetryAfter is the Retry-After header, set only on a 429.
	RetryAfter string
	// Body is the raw response body, kept for logging when neither field
	// above parsed out of it.
	Body string
}

func (e *APIError) Error() string {
	switch {
	case e.Code != "" && e.SubCode != "":
		return fmt.Sprintf("bot connector call failed: %d %s (%s)", e.StatusCode, e.Code, e.SubCode)
	case e.Code != "":
		return fmt.Sprintf("bot connector call failed: %d %s", e.StatusCode, e.Code)
	case e.RetryAfter != "":
		return fmt.Sprintf("bot connector call failed: %d, retry after %s", e.StatusCode, e.RetryAfter)
	default:
		return fmt.Sprintf("bot connector call failed: %d: %s", e.StatusCode, e.Body)
	}
}

// newAPIError parses what the Bot Connector will tell us, if anything: a
// response that fails to parse still yields a usable error carrying the
// status and raw body.
func newAPIError(resp *http.Response, body []byte) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(body), RetryAfter: resp.Header.Get("Retry-After")}

	var parsed struct {
		Error *struct {
			Code           string `json:"code"`
			InnerHTTPError *struct {
				Code string `json:"code"`
			} `json:"innerHttpError"`
		} `json:"error"`
		SubCode string `json:"subCode"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Error != nil {
			apiErr.Code = parsed.Error.Code
			if parsed.Error.InnerHTTPError != nil {
				apiErr.SubCode = parsed.Error.InnerHTTPError.Code
			}
		}
		if parsed.SubCode != "" {
			apiErr.SubCode = parsed.SubCode
		}
	}
	return apiErr
}

// TokenURL is where this client asks for a token: the configured endpoint, or
// one derived from the tenant id and tenant type when none is set. Getting the
// multi-tenant case wrong 401s every send with nothing in the response
// explaining why, because a multi-tenant bot registration authenticates
// through a shared botframework.com tenant rather than its own.
func TokenURL(cfg config.BotConfig) string {
	if cfg.TokenURL != "" {
		return cfg.TokenURL
	}
	if cfg.TenantType == "multi" {
		return "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token"
	}
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", cfg.TenantID)
}

func scopeOf(cfg config.BotConfig) string {
	if cfg.Scope != "" {
		return cfg.Scope
	}
	return "https://api.botframework.com/.default"
}

// instrumentation wraps the transport this client calls the Bot Connector
// through. It lives here, not in internal/metrics, so bot depends on nothing
// but an interface -- and so a test can pass one that does nothing.
type instrumentation interface {
	ClientTransport(base http.RoundTripper) http.RoundTripper
}

func NewClient(cfg config.BotConfig, tel instrumentation) (*Client, error) {
	oauthCfg := clientcredentials.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		TokenURL:     TokenURL(cfg),
		Scopes:       []string{scopeOf(cfg)},
	}

	// The instrumented transport goes underneath oauth2's, not around it. A
	// token refresh happens inside oauth2's RoundTrip, so measuring from the
	// outside would charge Entra's latency to the Bot Connector once an hour
	// and would report a token endpoint that is down as a Connector failure.
	// Underneath, both are measured and the address tells them apart.
	timeout := time.Duration(cfg.TimeoutSec) * time.Second
	base := &http.Client{
		Transport: tel.ClientTransport(http.DefaultTransport),
		Timeout:   timeout,
	}

	// The token source shares this context, so the token request goes through
	// the same instrumented client -- and gains the timeout it never had.
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, base)

	httpClient := oauthCfg.Client(ctx)
	// oauthCfg.Client returns a fresh *http.Client with a zero timeout, so the
	// timeout is set again here rather than inherited from base.
	httpClient.Timeout = timeout

	return &Client{httpClient: httpClient}, nil
}

// SendMessage posts a new activity to a conversation and returns its activity
// id, or "" when the Bot Connector answered without one -- which means the
// activity was delivered but cannot be updated later, not that sending failed.
func (c *Client) SendMessage(ctx context.Context, ref ConversationReference, msg Message) (string, error) {
	endpoint := conversationEndpoint(ref) + "/activities"
	resBody, err := c.doRequest(ctx, http.MethodPost, endpoint, msg.activity())
	if err != nil {
		return "", err
	}

	// An empty body says the same thing as one carrying no id: delivered, not
	// updatable. Decoding it would fail, and a failure here would be retried
	// into a second copy of a message the person already has.
	if len(bytes.TrimSpace(resBody)) == 0 {
		return "", nil
	}

	var res ResourceResponse
	if err := json.Unmarshal(resBody, &res); err != nil {
		return "", fmt.Errorf("decode send response: %w", err)
	}
	return res.ID, nil
}

// UpdateMessage replaces the content of a previously sent activity.
func (c *Client) UpdateMessage(ctx context.Context, ref ConversationReference, activityID string, msg Message) error {
	endpoint := conversationEndpoint(ref) + "/activities/" + url.PathEscape(activityID)
	_, err := c.doRequest(ctx, http.MethodPut, endpoint, msg.activity())
	return err
}

// conversationEndpoint builds the .../v3/conversations/{id} prefix both calls
// share. ServiceURL is trimmed of a trailing slash first: real Teams service
// URLs end in one (e.g. https://smba.trafficmanager.net/teams/), and naive
// concatenation would give "//v3/...". The conversation id is percent-escaped
// because it routinely contains ":" and "@" (e.g. a meeting chat's
// "19:meeting_abc@thread.v2").
func conversationEndpoint(ref ConversationReference) string {
	return fmt.Sprintf("%s/v3/conversations/%s", strings.TrimSuffix(ref.ServiceURL, "/"), url.PathEscape(ref.ConversationID))
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body any) ([]byte, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bot connector request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	resBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, newAPIError(resp, resBody)
	}

	return resBody, nil
}
