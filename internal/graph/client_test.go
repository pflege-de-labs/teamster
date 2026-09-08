package graph

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// newTestClient points a client at a stub Graph API, bypassing the OAuth2
// exchange that NewClient would otherwise perform against Entra.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Client{baseURL: srv.URL, httpClient: srv.Client()}
}

func TestNewClient(t *testing.T) {
	t.Parallel()

	client, err := NewClient(config.GraphConfig{
		TenantID:     "tenant",
		ClientID:     "client",
		ClientSecret: "secret",
		BaseURL:      "https://graph.example/v1.0",
		TimeoutSec:   7,
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.baseURL != "https://graph.example/v1.0" {
		t.Errorf("baseURL = %q, want the configured URL", client.baseURL)
	}
	if client.httpClient.Timeout != 7*time.Second {
		t.Errorf("timeout = %v, want 7s", client.httpClient.Timeout)
	}
}

func TestPostMessage(t *testing.T) {
	t.Parallel()

	var (
		gotMethod string
		gotPath   string
		gotBody   MessageRequest
	)

	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"message-1"}`))
	})

	id, err := client.PostMessage("team-1", "channel-1", json.RawMessage(`{"type":"AdaptiveCard"}`), "CPU <spiking>")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if id != "message-1" {
		t.Errorf("PostMessage() = %q, want %q", id, "message-1")
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if want := "/teams/team-1/channels/channel-1/messages"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if want := "<p>CPU &lt;spiking&gt;</p>"; gotBody.Body.Content != want {
		t.Errorf("summary = %q, want it HTML-escaped as %q", gotBody.Body.Content, want)
	}
	if len(gotBody.Attachments) != 1 || gotBody.Attachments[0].ContentType != "application/vnd.microsoft.card.adaptive" {
		t.Errorf("attachments = %+v, want a single adaptive card", gotBody.Attachments)
	}
}

func TestPostMessageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name: "server rejects the request",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"forbidden"}`))
			},
			wantErr: "failed",
		},
		{
			name: "response is not JSON",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: "decode post response",
		},
		{
			name: "response has no id",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{}`))
			},
			wantErr: "graph response missing id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, tt.handler)

			_, err := client.PostMessage("team", "channel", json.RawMessage(`{}`), "summary")
			if err == nil {
				t.Fatalf("PostMessage() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("PostMessage() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestUpdateMessage(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.UpdateMessage("team-1", "channel-1", "message-1", json.RawMessage(`{}`), "summary"); err != nil {
		t.Fatalf("UpdateMessage: %v", err)
	}
	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if want := "/teams/team-1/channels/channel-1/messages/message-1"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestUpdateMessageError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("message gone"))
	})

	err := client.UpdateMessage("team", "channel", "missing", json.RawMessage(`{}`), "summary")
	if err == nil || !strings.Contains(err.Error(), "message gone") {
		t.Errorf("UpdateMessage() = %v, want the Graph error body to be surfaced", err)
	}
}

func TestDoRequestErrors(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, func(http.ResponseWriter, *http.Request) {})

	tests := []struct {
		name    string
		method  string
		url     string
		body    any
		wantErr string
	}{
		{
			name:    "unencodable body",
			method:  http.MethodPost,
			url:     client.baseURL,
			body:    make(chan int),
			wantErr: "encode request",
		},
		{
			name:    "invalid method",
			method:  "bad method",
			url:     client.baseURL,
			wantErr: "new request",
		},
		{
			name:    "unreachable host",
			method:  http.MethodPost,
			url:     "http://127.0.0.1:1/teams",
			wantErr: "graph request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.doRequest(tt.method, tt.url, tt.body)
			if err == nil {
				t.Fatalf("doRequest() = nil error, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("doRequest() = %v, want an error containing %q", err, tt.wantErr)
			}
		})
	}
}
