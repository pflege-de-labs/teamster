package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// uninstrumented is what a client gets when nobody is measuring: the transport
// it was handed, unchanged.
type uninstrumented struct{}

func (uninstrumented) ClientTransport(base http.RoundTripper) http.RoundTripper { return base }

// newTestClient starts a stub Bot Connector and returns a client pointed at
// it, plus a ConversationReference whose ServiceURL reaches the stub. This
// bypasses the OAuth2 exchange NewClient would otherwise perform against Entra.
func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, ConversationReference) {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	client := &Client{httpClient: srv.Client()}
	ref := ConversationReference{ServiceURL: srv.URL, ConversationID: "convo-1"}
	return client, ref
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.BotConfig{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		TimeoutSec:   7,
	}, uninstrumented{})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	// The regression this guards: clientcredentials.Config.Client returns a
	// fresh *http.Client with a zero timeout, so the timeout must be set again
	// on the client it returns rather than assumed to carry over from base.
	if client.httpClient.Timeout != 7*time.Second {
		t.Errorf("timeout = %v, want 7s", client.httpClient.Timeout)
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
			// ":" and "@" are legal in a path segment unescaped (RFC 3986
			// pchar), so PathEscape leaves them as they are; what matters is
			// that the id survives intact rather than being mangled or
			// truncated.
			name:           "a conversation id containing : and @",
			serviceURLFunc: func(base string) string { return base + "/" },
			conversationID: "19:meeting_abc@thread.v2",
			wantPathSuffix: "/v3/conversations/19:meeting_abc@thread.v2/activities",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			name:     "title and text combine into a bold markdown first line",
			msg:      Message{Title: "CPU spiking", Text: "worker is hot"},
			wantText: "**CPU spiking**\nworker is hot",
		},
		{
			name:            "a card is attached at the activity root",
			msg:             Message{Title: "Disk filling", Text: "92% used", Card: json.RawMessage(`{"type":"AdaptiveCard"}`)},
			wantText:        "**Disk filling**\n92% used",
			wantAttachments: true,
		},
		{
			name:     "no title is just the text",
			msg:      Message{Text: "body only"},
			wantText: "body only",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"activity-1"}`))
	}))
	t.Cleanup(srv.Close)

	client := &Client{httpClient: srv.Client()}
	ref := ConversationReference{ServiceURL: srv.URL + "/", ConversationID: "19:meeting_abc@thread.v2"}

	if err := client.UpdateMessage(context.Background(), ref, "activity 1", Message{Title: "summary", Text: "body"}); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	// The conversation id's ":" and "@" are legal unescaped in a path segment
	// and survive as-is; the activity id's space is not, and is escaped.
	want := "/v3/conversations/19:meeting_abc@thread.v2/activities/activity%201"
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
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

func TestSendMessageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		wantStatusCode int
		wantCode       string
		wantSubCode    string
		wantRetryAfter string
	}{
		{
			name: "403 with a Bot Connector error body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":{"code":"MessageWritesBlocked","message":"the bot was blocked","innerHttpError":{"code":"Forbidden"}}}`))
			},
			wantStatusCode: http.StatusForbidden,
			wantCode:       "MessageWritesBlocked",
			wantSubCode:    "Forbidden",
		},
		{
			name: "429 with Retry-After",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "30")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"error":{"code":"TooManyRequests"}}`))
			},
			wantStatusCode: http.StatusTooManyRequests,
			wantCode:       "TooManyRequests",
			wantRetryAfter: "30",
		},
		{
			name: "500 with no parseable body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte("internal error"))
			},
			wantStatusCode: http.StatusInternalServerError,
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

			apiErr, ok := err.(*APIError)
			if !ok {
				t.Fatalf("SendMessage() error = %v (%T), want *APIError", err, err)
			}
			if apiErr.StatusCode != tt.wantStatusCode {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tt.wantStatusCode)
			}
			if apiErr.Code != tt.wantCode {
				t.Errorf("Code = %q, want %q", apiErr.Code, tt.wantCode)
			}
			if apiErr.SubCode != tt.wantSubCode {
				t.Errorf("SubCode = %q, want %q", apiErr.SubCode, tt.wantSubCode)
			}
			if apiErr.RetryAfter != tt.wantRetryAfter {
				t.Errorf("RetryAfter = %q, want %q", apiErr.RetryAfter, tt.wantRetryAfter)
			}
			if apiErr.Error() == "" {
				t.Error("Error() = empty string")
			}
		})
	}
}

func TestUpdateMessageError(t *testing.T) {
	t.Parallel()

	client, ref := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"ActivityNotFound"}}`))
	})

	err := client.UpdateMessage(context.Background(), ref, "gone", Message{Text: "hi"})
	if err == nil {
		t.Fatalf("UpdateMessage() = nil error, want one")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("UpdateMessage() error = %v (%T), want *APIError", err, err)
	}
	if apiErr.Code != "ActivityNotFound" {
		t.Errorf("Code = %q, want %q", apiErr.Code, "ActivityNotFound")
	}
}

func TestDoRequestTransportErrors(t *testing.T) {
	t.Parallel()

	client := &Client{httpClient: &http.Client{Timeout: time.Millisecond}}
	ref := ConversationReference{ServiceURL: "http://127.0.0.1:1", ConversationID: "c1"}

	if _, err := client.SendMessage(context.Background(), ref, Message{Text: "hi"}); err == nil || !strings.Contains(err.Error(), "bot connector request") {
		t.Errorf("SendMessage() = %v, want an error containing %q", err, "bot connector request")
	}
}
