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
	// Text is plain text or Bot Framework's Markdown subset -- unlike
	// graph.Message.Text, which is HTML. Feeding it HTML shows up as literal
	// "<p><b>" in the chat rather than being rendered.
	Text string
	Card json.RawMessage
}

// markdownMetachars are the ASCII punctuation characters CommonMark gives
// meaning to; backslash-escaping each one turns it back into a literal
// character rather than italics, a link, a header or a rule.
const markdownMetachars = "\\`*_{}[]()#+-.!|~<>"

// escapeMarkdown neutralizes Title before it is embedded in markdown. Title
// is plain text handed to us by a template or an alert label, not something
// the caller composed as markdown on purpose, so a stray "*", "[" or
// backtick in it must not restyle or break the message. A newline is
// replaced rather than escaped because backslash-newline is itself markdown
// for a hard line break, which would not fix the problem.
func escapeMarkdown(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if strings.ContainsRune(markdownMetachars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
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
// line a Teams channel post gets from its own title; Title is escaped because
// it is plain text being embedded in markdown, while Text is left alone
// because the caller composed it as markdown on purpose. The title and body
// are separated by a blank line rather than a single newline because some
// Teams clients collapse a lone newline, which would run the bold title into
// the body; the attachment is omitted entirely rather than left empty when
// there is no card.
func (m Message) activity() activity {
	var text strings.Builder
	if m.Title != "" {
		text.WriteString("**")
		text.WriteString(escapeMarkdown(m.Title))
		text.WriteString("**\n\n")
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
	// Message is error.message, the Bot Connector's human-readable cause.
	Message string
	// InnerStatusCode is error.innerHttpError.statusCode: the status of the
	// downstream call the Connector was proxying when it failed.
	InnerStatusCode int
	// InnerCode is error.innerHttpError.body.error.code -- documented as an
	// arbitrary Object, but in practice another {error:{code,message}}
	// wrapper one level deeper, carrying the actionable code (e.g.
	// "ConversationBlockedByUser") that Code alone does not.
	InnerCode string
	// RetryAfter is the Retry-After header, set only on a 429.
	RetryAfter string
	// Body is the raw response body, kept for logging when nothing above
	// parsed out of it.
	Body string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("bot connector call failed: %d", e.StatusCode)
	switch {
	case e.Code != "" && e.InnerCode != "":
		msg += fmt.Sprintf(" %s (%s)", e.Code, e.InnerCode)
	case e.Code != "":
		msg += " " + e.Code
	default:
		// Nothing parsed out of the body; keep it for whoever reads the log.
		msg += ": " + e.Body
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	if e.RetryAfter != "" {
		msg += ", retry after " + e.RetryAfter
	}
	return msg
}

// newAPIError parses what the Bot Connector will tell us, if anything: a
// response that fails to parse still yields a usable error carrying the
// status and raw body. The shape follows the documented Error object --
// {code, message, innerHttpError: {statusCode, body}} -- rather than the
// {innerHttpError: {code}} / top-level subCode shape the Connector does not
// actually emit; the nested code callers want (e.g. "MessageWritesBlocked" vs
// "ConversationBlockedByUser") lives at error.innerHttpError.body.error.code.
func newAPIError(resp *http.Response, body []byte) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Body: string(body), RetryAfter: resp.Header.Get("Retry-After")}

	var parsed struct {
		Error *struct {
			Code           string `json:"code"`
			Message        string `json:"message"`
			InnerHTTPError *struct {
				StatusCode int             `json:"statusCode"`
				Body       json.RawMessage `json:"body"`
			} `json:"innerHttpError"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Error == nil {
		return apiErr
	}
	apiErr.Code = parsed.Error.Code
	apiErr.Message = parsed.Error.Message

	inner := parsed.Error.InnerHTTPError
	if inner == nil {
		return apiErr
	}
	apiErr.InnerStatusCode = inner.StatusCode

	var nested struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(inner.Body, &nested); err == nil && nested.Error != nil {
		apiErr.InnerCode = nested.Error.Code
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
	endpoint, err := conversationEndpoint(ref)
	if err != nil {
		return "", err
	}
	resBody, err := c.doRequest(ctx, http.MethodPost, endpoint+"/activities", msg.activity())
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
	endpoint, err := conversationEndpoint(ref)
	if err != nil {
		return err
	}
	_, err = c.doRequest(ctx, http.MethodPut, endpoint+"/activities/"+url.PathEscape(activityID), msg.activity())
	return err
}

// conversationEndpoint builds the .../v3/conversations/{id} prefix both calls
// share. ServiceURL is trimmed of a trailing slash first: real Teams service
// URLs end in one (e.g. https://smba.trafficmanager.net/teams/), and naive
// concatenation would give "//v3/...". The conversation id is percent-escaped
// because it routinely contains ":" and "@" (e.g. a meeting chat's
// "19:meeting_abc@thread.v2").
//
// ServiceURL is not configuration: it arrives on an inbound activity and is
// stored per recipient, so it is attacker-influenced data. Both callers carry
// the bot's bearer token in an Authorization header, and Microsoft's own auth
// guidance is explicit that the token must never go out over an unsecured
// channel, so anything other than https is refused here rather than sent.
func conversationEndpoint(ref ConversationReference) (string, error) {
	trimmed := strings.TrimSuffix(ref.ServiceURL, "/")
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse conversation service url: %w", err)
	}
	if u.Scheme != "https" {
		return "", fmt.Errorf("conversation service url %q must use https, not send the bot token over %q", ref.ServiceURL, u.Scheme)
	}
	return fmt.Sprintf("%s/v3/conversations/%s", trimmed, url.PathEscape(ref.ConversationID)), nil
}

// maxResponseBody caps how much of a Bot Connector response this client will
// read. The response comes from ServiceURL -- attacker-influenced data, see
// conversationEndpoint -- so reading it without a limit lets that host exhaust
// memory with an arbitrarily large body.
const maxResponseBody = 1 << 20 // 1 MiB

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

	resBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, newAPIError(resp, resBody)
	}

	return resBody, nil
}
