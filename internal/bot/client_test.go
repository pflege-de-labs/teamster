package bot

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// uninstrumented is what a client gets when nobody is measuring: the transport
// it was handed, unchanged.
type uninstrumented struct{}

func (uninstrumented) ClientTransport(base http.RoundTripper) http.RoundTripper { return base }

// newTestClient starts a stub Bot Connector over TLS and returns a client
// pointed at it, plus a ConversationReference whose ServiceURL reaches the
// stub. TLS, not plain HTTP, because conversationEndpoint refuses anything
// but https -- the bearer token must never be sent to ServiceURL over an
// unsecured channel -- and srv.Client() trusts the stub's certificate, so
// production's scheme check is exercised rather than worked around. This
// bypasses the OAuth2 exchange NewClient would otherwise perform against
// Entra.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, ConversationReference) {
	t.Helper()

	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	client := &Client{httpClient: srv.Client()}
	ref := ConversationReference{ServiceURL: srv.URL, ConversationID: "convo-1"}
	return client, ref
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		timeoutSec int
		want       time.Duration
	}{
		// The regression this guards: clientcredentials.Config.Client returns a
		// fresh *http.Client with a zero timeout, so the timeout must be set
		// again on the client it returns rather than assumed to carry over from
		// base.
		{name: "a configured timeout", timeoutSec: 7, want: 7 * time.Second},
		{name: "no timeout configured means no timeout, not the zero value's default", timeoutSec: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, err := NewClient(config.BotConfig{
				TenantID:     "tenant",
				ClientID:     "client",
				ClientSecret: "secret",
				TimeoutSec:   tt.timeoutSec,
			}, uninstrumented{})
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			if client.httpClient.Timeout != tt.want {
				t.Errorf("timeout = %v, want %v", client.httpClient.Timeout, tt.want)
			}
		})
	}
}

// instrumentedTransport marks the transport tel.ClientTransport returned, so
// the test below can tell it apart from the bare http.DefaultTransport
// NewClient was handed -- the regression being guarded is that return value
// being silently discarded.
type instrumentedTransport struct{ http.RoundTripper }

type spyInstrumentation struct{ called *bool }

func (s spyInstrumentation) ClientTransport(base http.RoundTripper) http.RoundTripper {
	*s.called = true
	return instrumentedTransport{base}
}

// TestNewClientInstallsInstrumentedTransport guards against NewClient wiring
// http.DefaultTransport directly instead of tel.ClientTransport's return
// value: every other test still passes either way, so nothing else catches a
// bot call silently vanishing from metrics.
func TestNewClientInstallsInstrumentedTransport(t *testing.T) {
	t.Parallel()

	var called bool
	client, err := NewClient(config.BotConfig{TenantID: "tenant", ClientID: "client", ClientSecret: "secret"}, spyInstrumentation{&called})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if !called {
		t.Fatal("NewClient never asked the instrumentation for a transport")
	}

	oauthTransport, ok := client.httpClient.Transport.(*oauth2.Transport)
	if !ok {
		t.Fatalf("httpClient.Transport = %T, want *oauth2.Transport", client.httpClient.Transport)
	}
	if _, ok := oauthTransport.Base.(instrumentedTransport); !ok {
		t.Errorf("oauth2.Transport.Base = %T, want the transport tel.ClientTransport returned, not the bare one NewClient was handed", oauthTransport.Base)
	}
}

func TestTokenURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.BotConfig
		want string
	}{
		{
			name: "single tenant derives the tenant-specific endpoint",
			cfg:  config.BotConfig{TenantID: "tenant-1", TenantType: "single"},
			want: "https://login.microsoftonline.com/tenant-1/oauth2/v2.0/token",
		},
		{
			name: "empty tenant type behaves as single",
			cfg:  config.BotConfig{TenantID: "tenant-1"},
			want: "https://login.microsoftonline.com/tenant-1/oauth2/v2.0/token",
		},
		{
			name: "multi tenant uses the shared botframework.com endpoint",
			cfg:  config.BotConfig{TenantID: "tenant-1", TenantType: "multi"},
			want: "https://login.microsoftonline.com/botframework.com/oauth2/v2.0/token",
		},
		{
			name: "a configured endpoint overrides derivation even for multi tenant",
			cfg:  config.BotConfig{TenantID: "tenant-1", TenantType: "multi", TokenURL: "https://login.example/token"},
			want: "https://login.example/token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := TokenURL(tt.cfg); got != tt.want {
				t.Errorf("TokenURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestScopeOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.BotConfig
		want string
	}{
		{name: "no scope configured derives the Bot Connector default", cfg: config.BotConfig{}, want: "https://api.botframework.com/.default"},
		{name: "a configured scope overrides the default", cfg: config.BotConfig{Scope: "https://example/.default"}, want: "https://example/.default"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := scopeOf(tt.cfg); got != tt.want {
				t.Errorf("scopeOf() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSendMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serviceURLFunc func(base string) string
		conversationID string
		wantPathSuffix string
	}{
		{
			name:           "a service URL with a trailing slash",
			serviceURLFunc: func(base string) string { return base + "/" },
			conversationID: "convo-1",
			wantPathSuffix: "/v3/conversations/convo-1/activities",
		},
		{
			name:           "a service URL without a trailing slash",
			serviceURLFunc: func(base string) string { return base },
			conversationID: "convo-1",
			wantPathSuffix: "/v3/conversations/convo-1/activities",
		},
		{
			// A real regional service URL carries a path, e.g.
			// https://smba.trafficmanager.net/teams/ or .../emea/.
			name:           "a service URL with a path component",
			serviceURLFunc: func(base string) string { return base + "/teams/" },
			conversationID: "convo-1",
			wantPathSuffix: "/teams/v3/conversations/convo-1/activities",
		},
		{
			// ":" and "@" are legal in a path segment unescaped (RFC 3986
			// pchar), so PathEscape leaves them as they are; what matters is
			// that the id survives intact rather than being mangled or
			// truncated.
			name:           "a conversation id containing : and @",
			serviceURLFunc: func(base string) string { return base + "/" },
			conversationID: "19:meeting_abc@thread.v2",
			wantPathSuffix: "/v3/conversations/19:meeting_abc@thread.v2/activities",
		},
		{
			// "/" is the character that actually changes under PathEscape: left
			// unescaped it injects a path segment rather than surviving as part
			// of the id. Deleting url.PathEscape from conversationEndpoint must
			// fail this case specifically -- the ":"/"@" case above cannot,
			// because escaping is a no-op on both.
			name:           "a conversation id containing a slash",
			serviceURLFunc: func(base string) string { return base },
			conversationID: "convo/1",
			wantPathSuffix: "/v3/conversations/convo%2F1/activities",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.EscapedPath()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"activity-1"}`))
			}))
			t.Cleanup(srv.Close)

			client := &Client{httpClient: srv.Client()}
			ref := ConversationReference{ServiceURL: tt.serviceURLFunc(srv.URL), ConversationID: tt.conversationID}

			id, err := client.SendMessage(context.Background(), ref, Message{Title: "CPU spiking", Text: "worker is hot"})
			if err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			if id != "activity-1" {
				t.Errorf("SendMessage() = %q, want %q", id, "activity-1")
			}
			if gotMethod != http.MethodPost {
				t.Errorf("method = %q, want POST", gotMethod)
			}
			if gotPath != tt.wantPathSuffix {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPathSuffix)
			}
		})
	}
}

func TestSendMessageBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		msg             Message
		wantText        string
		wantAttachments bool
	}{
		{
			name:     "title and text combine into a bold markdown first line, separated by a blank line",
			msg:      Message{Title: "CPU spiking", Text: "worker is hot"},
			wantText: "**CPU spiking**\n\nworker is hot",
		},
		{
			name:            "a card is attached at the activity root",
			msg:             Message{Title: "Disk filling", Text: "92% used", Card: json.RawMessage(`{"type":"AdaptiveCard"}`)},
			wantText:        "**Disk filling**\n\n92% used",
			wantAttachments: true,
		},
		{
			name:     "no title is just the text",
			msg:      Message{Text: "body only"},
			wantText: "body only",
		},
		{
			// An alert title is plain text, not something the caller composed
			// as markdown: unescaped, "*", "_" and "`" would restyle the title
			// or run it into the body.
			name:     "markdown metacharacters in the title are escaped",
			msg:      Message{Title: "disk_usage_high on node_3", Text: "92% used"},
			wantText: "**disk\\_usage\\_high on node\\_3**\n\n92% used",
		},
		{
			// A newline in the title would otherwise break the bold first
			// line's markdown; it is replaced rather than escaped, since
			// backslash-newline is itself markdown for a hard break.
			name:     "a newline in the title does not break the markdown",
			msg:      Message{Title: "line one\nline two", Text: "body"},
			wantText: "**line one line two**\n\nbody",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"activity-1"}`))
			}))
			t.Cleanup(srv.Close)

			client := &Client{httpClient: srv.Client()}
			ref := ConversationReference{ServiceURL: srv.URL, ConversationID: "c1"}

			if _, err := client.SendMessage(context.Background(), ref, tt.msg); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}

			if gotBody["type"] != "message" {
				t.Errorf("type = %v, want %q", gotBody["type"], "message")
			}
			if gotBody["text"] != tt.wantText {
				t.Errorf("text = %q, want %q", gotBody["text"], tt.wantText)
			}
			_, hasAttachments := gotBody["attachments"]
			if hasAttachments != tt.wantAttachments {
				t.Errorf("attachments present = %v, want %v (body: %+v)", hasAttachments, tt.wantAttachments, gotBody)
			}
		})
	}
}

func TestUpdateMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serviceURLFunc func(base string) string
		conversationID string
		activityID     string
		wantPathSuffix string
	}{
		{
			name:           "the conversation id's : and @ survive unescaped, the activity id's space does not",
			serviceURLFunc: func(base string) string { return base + "/" },
			conversationID: "19:meeting_abc@thread.v2",
			activityID:     "activity 1",
			wantPathSuffix: "/v3/conversations/19:meeting_abc@thread.v2/activities/activity%201",
		},
		{
			// "/" is the character PathEscape actually changes; unescaped it
			// would inject an extra path segment into either id.
			name:           "a slash in either id is percent-escaped",
			serviceURLFunc: func(base string) string { return base },
			conversationID: "convo/1",
			activityID:     "activity/1",
			wantPathSuffix: "/v3/conversations/convo%2F1/activities/activity%2F1",
		},
		{
			name:           "a service URL with a path component",
			serviceURLFunc: func(base string) string { return base + "/emea/" },
			conversationID: "convo-1",
			activityID:     "activity-1",
			wantPathSuffix: "/emea/v3/conversations/convo-1/activities/activity-1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod = r.Method
				gotPath = r.URL.EscapedPath()
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"activity-1"}`))
			}))
			t.Cleanup(srv.Close)

			client := &Client{httpClient: srv.Client()}
			ref := ConversationReference{ServiceURL: tt.serviceURLFunc(srv.URL), ConversationID: tt.conversationID}

			if err := client.UpdateMessage(context.Background(), ref, tt.activityID, Message{Title: "summary", Text: "body"}); err != nil {
				t.Fatalf("UpdateMessage: %v", err)
			}
			if gotMethod != http.MethodPut {
				t.Errorf("method = %q, want PUT", gotMethod)
			}
			if gotPath != tt.wantPathSuffix {
				t.Errorf("path = %q, want %q", gotPath, tt.wantPathSuffix)
			}
		})
	}
}

// TestUpdateMessageBody guards the wire format UpdateMessage sends: the
// existing tests only ever asserted the method and the path, never what the
// PUT body actually contained.
func TestUpdateMessageBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		msg             Message
		wantText        string
		wantAttachments bool
	}{
		{
			name:     "title and text combine into a bold markdown first line",
			msg:      Message{Title: "Disk filling", Text: "92% used"},
			wantText: "**Disk filling**\n\n92% used",
		},
		{
			name:            "a card is attached at the activity root",
			msg:             Message{Title: "Disk filling", Text: "92% used", Card: json.RawMessage(`{"type":"AdaptiveCard"}`)},
			wantText:        "**Disk filling**\n\n92% used",
			wantAttachments: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			client, ref := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				raw, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(raw, &gotBody)
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"activity-1"}`))
			})

			if err := client.UpdateMessage(context.Background(), ref, "activity-1", tt.msg); err != nil {
				t.Fatalf("UpdateMessage: %v", err)
			}

			if gotBody["type"] != "message" {
				t.Errorf("type = %v, want %q", gotBody["type"], "message")
			}
			if gotBody["text"] != tt.wantText {
				t.Errorf("text = %q, want %q", gotBody["text"], tt.wantText)
			}
			_, hasAttachments := gotBody["attachments"]
			if hasAttachments != tt.wantAttachments {
				t.Errorf("attachments present = %v, want %v (body: %+v)", hasAttachments, tt.wantAttachments, gotBody)
			}
		})
	}
}

// A response with no id means the activity was sent but cannot be updated
// later -- not that sending failed -- so this must not error the way
// graph.PostMessage does on a missing id.
func TestSendMessageNoIDIsNotAnError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "an object without an id", body: `{}`},
		// Some channels answer a send with 200 and nothing at all. Decoding
		// that fails, and an error here would be retried into a second copy of
		// a message the person already has.
		{name: "an empty body", body: ""},
		{name: "whitespace only", body: "\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, ref := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			})

			id, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"})
			if err != nil {
				t.Fatalf("SendMessage: %v, want no error on a missing id", err)
			}
			if id != "" {
				t.Errorf("SendMessage() id = %q, want empty", id)
			}
		})
	}
}

// A 200 with a body that is not JSON at all -- the realistic case is a proxy
// in front of the Connector returning its own HTML error page with a 200 --
// must not be mistaken for a delivered message with no id.
func TestSendMessageMalformedSuccessBody(t *testing.T) {
	t.Parallel()

	client, ref := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body>upstream error</body></html>"))
	})

	_, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"})
	if err == nil {
		t.Fatal("SendMessage() = nil error, want a decode error")
	}
	if !strings.Contains(err.Error(), "decode send response") {
		t.Errorf("SendMessage() = %v, want an error containing %q", err, "decode send response")
	}
}

func TestSendMessageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantStatusCode int
		wantCode       string
		wantMessage    string
		wantInnerCode  string
		wantRetryAfter string
		wantBody       string
		wantErr        string
	}{
		{
			// The documented shape: error.{code,message}, and the nested,
			// actionable code at error.innerHttpError.body.error.code -- not
			// error.innerHttpError.code and not a top-level subCode, neither of
			// which the Connector emits.
			name: "403 with a Bot Connector error body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"MessageWritesBlocked","message":"the bot was blocked by the user","innerHttpError":{"statusCode":403,"body":{"error":{"code":"ConversationBlockedByUser","message":"user blocked the conversation"}}}}}`))
			},
			wantStatusCode: http.StatusForbidden,
			wantCode:       "MessageWritesBlocked",
			wantMessage:    "the bot was blocked by the user",
			wantInnerCode:  "ConversationBlockedByUser",
			wantBody:       `{"error":{"code":"MessageWritesBlocked","message":"the bot was blocked by the user","innerHttpError":{"statusCode":403,"body":{"error":{"code":"ConversationBlockedByUser","message":"user blocked the conversation"}}}}}`,
			wantErr:        "bot connector call failed: 403 MessageWritesBlocked (ConversationBlockedByUser): the bot was blocked by the user",
		},
		{
			name: "429 with Retry-After",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests","message":"rate limited"}}`))
			},
			wantStatusCode: http.StatusTooManyRequests,
			wantCode:       "TooManyRequests",
			wantMessage:    "rate limited",
			wantRetryAfter: "30",
			wantBody:       `{"error":{"code":"TooManyRequests","message":"rate limited"}}`,
			wantErr:        "bot connector call failed: 429 TooManyRequests: rate limited, retry after 30",
		},
		{
			name: "500 with no parseable body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantStatusCode: http.StatusInternalServerError,
			wantBody:       "internal error",
			wantErr:        "bot connector call failed: 500: internal error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, ref := newTestClient(t, tt.handler)

			_, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"})
			if err == nil {
				t.Fatalf("SendMessage() = nil error, want one")
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("SendMessage() error = %v (%T), want *APIError", err, err)
			}
			if apiErr.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.wantStatusCode)
			}
			if apiErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tt.wantCode)
			}
			if apiErr.Message != tt.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tt.wantMessage)
			}
			if apiErr.InnerCode != tt.wantInnerCode {
				t.Errorf("InnerCode = %q, want %q", apiErr.InnerCode, tt.wantInnerCode)
			}
			if apiErr.RetryAfter != tt.wantRetryAfter {
				t.Errorf("RetryAfter = %q, want %q", apiErr.RetryAfter, tt.wantRetryAfter)
			}
			if apiErr.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", apiErr.Body, tt.wantBody)
			}
			if got := apiErr.Error(); got != tt.wantErr {
				t.Errorf("Error() = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestUpdateMessageError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode string
		wantErr  string
	}{
		{
			name: "404 activity not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"code":"ActivityNotFound","message":"the activity no longer exists"}}`))
			},
			wantCode: "ActivityNotFound",
			wantErr:  "bot connector call failed: 404 ActivityNotFound: the activity no longer exists",
		},
		{
			name: "403 message writes blocked",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"MessageWritesBlocked","message":"the bot was blocked"}}`))
			},
			wantCode: "MessageWritesBlocked",
			wantErr:  "bot connector call failed: 403 MessageWritesBlocked: the bot was blocked",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client, ref := newTestClient(t, tt.handler)

			err := client.UpdateMessage(context.Background(), ref, "gone", Message{Text: "hi"})
			if err == nil {
				t.Fatalf("UpdateMessage() = nil error, want one")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("UpdateMessage() error = %v (%T), want *APIError", err, err)
			}
			if apiErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tt.wantCode)
			}
			if got := apiErr.Error(); got != tt.wantErr {
				t.Errorf("Error() = %q, want %q", got, tt.wantErr)
			}
		})
	}
}

// ServiceURL arrives on an inbound activity and is stored per recipient, so
// it is attacker-influenced, not configuration. Sending the bearer token to
// anything but https would hand it, in plaintext for http, to whatever host
// the activity named.
func TestConversationEndpointRejectsInsecureSchemes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		serviceURL string
		wantErr    bool
	}{
		{name: "https is accepted", serviceURL: "https://smba.trafficmanager.net/teams/", wantErr: false},
		{name: "http is refused", serviceURL: "http://smba.trafficmanager.net/teams/", wantErr: true},
		{name: "no scheme at all is refused", serviceURL: "smba.trafficmanager.net/teams/", wantErr: true},
		{name: "an unparseable URL is refused", serviceURL: "https://%zz", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := conversationEndpoint(ConversationReference{ServiceURL: tt.serviceURL, ConversationID: "c1"})
			if tt.wantErr && err == nil {
				t.Errorf("conversationEndpoint(%q) = nil error, want one", tt.serviceURL)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("conversationEndpoint(%q) = %v, want it accepted", tt.serviceURL, err)
			}
		})
	}
}

// Both callers must fail closed on a non-https ServiceURL rather than making
// the request anyway: the assertion that matters is that the stub server
// never sees a call, not just that an error came back.
func TestSendAndUpdateRefuseInsecureServiceURL(t *testing.T) {
	t.Parallel()

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	t.Cleanup(srv.Close)

	client := &Client{httpClient: srv.Client()}
	ref := ConversationReference{ServiceURL: srv.URL, ConversationID: "c1"} // srv.URL is http://

	if _, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"}); err == nil {
		t.Error("SendMessage() = nil error over http, want it refused")
	}
	if err := client.UpdateMessage(context.Background(), ref, "a1", Message{Text: "hi"}); err == nil {
		t.Error("UpdateMessage() = nil error over http, want it refused")
	}
	if calls != 0 {
		t.Errorf("stub server was called %d times, want 0 -- the request must never be sent", calls)
	}
}

func TestDoRequestTransportErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		call func(c *Client, ref ConversationReference) error
	}{
		{
			name: "SendMessage",
			call: func(c *Client, ref ConversationReference) error {
				_, err := c.SendMessage(context.Background(), ref, Message{Text: "hi"})
				return err
			},
		},
		{
			name: "UpdateMessage",
			call: func(c *Client, ref ConversationReference) error {
				return c.UpdateMessage(context.Background(), ref, "a1", Message{Text: "hi"})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &Client{httpClient: &http.Client{Timeout: time.Millisecond}}
			// https, not http: the point of this test is an unreachable port,
			// not the scheme check, and a non-https URL would be rejected
			// before any connection was attempted at all.
			ref := ConversationReference{ServiceURL: "https://127.0.0.1:1", ConversationID: "c1"}

			err := tt.call(client, ref)
			if err == nil || !strings.Contains(err.Error(), "bot connector request") {
				t.Errorf("%s() = %v, want an error containing %q", tt.name, err, "bot connector request")
			}
		})
	}
}
