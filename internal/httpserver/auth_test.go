package httpserver

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/pflege-de/teamster/internal/config"
)

func TestBasicAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		header     string
		wantStatus int
	}{
		{name: "correct credentials", header: "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:pass")), wantStatus: http.StatusOK},
		{name: "wrong password", header: "Basic " + base64.StdEncoding.EncodeToString([]byte("admin:nope")), wantStatus: http.StatusUnauthorized},
		{name: "wrong user", header: "Basic " + base64.StdEncoding.EncodeToString([]byte("root:pass")), wantStatus: http.StatusUnauthorized},
		{name: "missing header", header: "", wantStatus: http.StatusUnauthorized},
		{name: "wrong scheme", header: "Bearer token", wantStatus: http.StatusUnauthorized},
		{name: "payload is not base64", header: "Basic not-base64!", wantStatus: http.StatusUnauthorized},
		{name: "payload has no colon", header: "Basic " + base64.StdEncoding.EncodeToString([]byte("adminpass")), wantStatus: http.StatusUnauthorized},
	}

	srv := &Server{cfg: config.Config{Admin: config.AdminConfig{Username: "admin", Password: "pass"}}}
	protected := srv.basicAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/admin", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}

			rec := httptest.NewRecorder()
			protected.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if tt.wantStatus == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("missing WWW-Authenticate header on a rejected request")
			}
		})
	}
}

func TestWebhookAuth(t *testing.T) {
	t.Parallel()

	srv := &Server{cfg: config.Config{Webhook: config.WebhookConfig{Token: "secret"}}}

	tests := []struct {
		name  string
		token string
		want  bool
	}{
		{name: "matching token", token: "secret", want: true},
		{name: "wrong token", token: "nope", want: false},
		{name: "prefix of the token", token: "sec", want: false},
		{name: "missing token", token: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodPost, "/webhook/universal", nil)
			if tt.token != "" {
				req.Header.Set("X-Teamster-Token", tt.token)
			}

			if got := srv.webhookAuth(req); got != tt.want {
				t.Errorf("webhookAuth() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSafeEquals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "equal", a: "abc", b: "abc", want: true},
		{name: "different content", a: "abc", b: "abd", want: false},
		{name: "different length", a: "abc", b: "abcd", want: false},
		{name: "both empty", a: "", b: "", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := safeEquals(tt.a, tt.b); got != tt.want {
				t.Errorf("safeEquals(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
