package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

const issuedToken = "tst_issued"

func tokenStore() *fakeStore {
	st := newFakeStore()
	st.accessTokens["id-1"] = models.AccessToken{ID: "id-1", Name: "alertmanager", TokenHash: hashToken(issuedToken)}
	return st
}

func webhookRequest(headers map[string]string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	return req
}

func TestWebhookAuth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configToken string
		headers     map[string]string
		wantName    string
		wantErr     error
	}{
		{name: "config token as bearer", configToken: "secret", headers: map[string]string{"Authorization": "Bearer secret"}, wantName: configTokenName},
		{name: "scheme is case-insensitive", configToken: "secret", headers: map[string]string{"Authorization": "bearer secret"}, wantName: configTokenName},
		{name: "config token in the legacy header", configToken: "secret", headers: map[string]string{"X-Teamster-Token": "secret"}, wantName: configTokenName},
		{name: "issued token as bearer", configToken: "secret", headers: map[string]string{"Authorization": "Bearer " + issuedToken}, wantName: "alertmanager"},
		{name: "issued token in the legacy header", headers: map[string]string{"X-Teamster-Token": issuedToken}, wantName: "alertmanager"},
		{name: "bearer wins over the legacy header", configToken: "secret", headers: map[string]string{"Authorization": "Bearer " + issuedToken, "X-Teamster-Token": "wrong"}, wantName: "alertmanager"},
		{name: "wrong token", configToken: "secret", headers: map[string]string{"Authorization": "Bearer nope"}, wantErr: errUnknownToken},
		{name: "prefix of the token", configToken: "secret", headers: map[string]string{"X-Teamster-Token": "sec"}, wantErr: errUnknownToken},
		{name: "basic auth is not a bearer token", configToken: "secret", headers: map[string]string{"Authorization": "Basic c2VjcmV0"}, wantErr: errNoCredential},
		{name: "empty bearer", configToken: "secret", headers: map[string]string{"Authorization": "Bearer "}, wantErr: errNoCredential},
		{name: "no credential", configToken: "secret", wantErr: errNoCredential},
		// An unset webhook.token must not match an empty credential.
		{name: "no credential and no config token", wantErr: errNoCredential},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := &Server{cfg: config.Config{Webhook: config.WebhookConfig{Token: tt.configToken}}, store: tokenStore()}
			token, err := srv.webhookAuth(webhookRequest(tt.headers))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("webhookAuth() error = %v, want %v", err, tt.wantErr)
			}
			if token.Name != tt.wantName {
				t.Errorf("webhookAuth() = %q, want %q", token.Name, tt.wantName)
			}
		})
	}
}

func TestWebhookAuthRecordsUse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		lastUsed  time.Time
		wantTouch bool
	}{
		{name: "never used", wantTouch: true},
		{name: "used long ago", lastUsed: time.Now().Add(-2 * touchInterval), wantTouch: true},
		{name: "used moments ago", lastUsed: time.Now().Add(-time.Minute), wantTouch: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := tokenStore()
			token := st.accessTokens["id-1"]
			token.LastUsedAt = tt.lastUsed
			st.accessTokens["id-1"] = token

			srv := &Server{store: st}
			if _, err := srv.webhookAuth(webhookRequest(map[string]string{"Authorization": "Bearer " + issuedToken})); err != nil {
				t.Fatalf("webhookAuth: %v", err)
			}
			if touched := !st.accessTokens["id-1"].LastUsedAt.Equal(tt.lastUsed); touched != tt.wantTouch {
				t.Errorf("last_used_at touched = %v, want %v", touched, tt.wantTouch)
			}
		})
	}
}

func TestWebhookAuthOutcomes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		failOn     []string
		headers    map[string]string
		wantStatus int
	}{
		{name: "issued token is admitted", headers: map[string]string{"Authorization": "Bearer " + issuedToken}, wantStatus: http.StatusOK},
		{name: "a failed touch still admits", failOn: []string{"TouchAccessToken"}, headers: map[string]string{"Authorization": "Bearer " + issuedToken}, wantStatus: http.StatusOK},
		{name: "wrong token is refused", headers: map[string]string{"Authorization": "Bearer nope"}, wantStatus: http.StatusUnauthorized},
		{name: "missing token is refused", wantStatus: http.StatusUnauthorized},
		// An outage must not read as a wrong secret to the sender.
		{name: "store failure is unavailable", failOn: []string{"GetAccessTokenByHash"}, headers: map[string]string{"Authorization": "Bearer nope"}, wantStatus: http.StatusServiceUnavailable},
	}

	for _, tt := range tests {
		for _, path := range []string{"/webhook/alertmanager", "/webhook/universal"} {
			t.Run(tt.name+" "+path, func(t *testing.T) {
				t.Parallel()

				st := tokenStore().fail(tt.failOn...)
				st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"x"}`}
				st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel"}
				st.routes["route"] = models.Route{ID: "route", TemplateID: "tmpl", DestinationID: "dest", IsDefault: true}
				handler := newTestServer(t, st, &fakeMessenger{}).Handler

				req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"alerts":[]}`))
				for key, value := range tt.headers {
					req.Header.Set(key, value)
				}
				rec := httptest.NewRecorder()
				handler.ServeHTTP(rec, req)

				if rec.Code != tt.wantStatus {
					t.Fatalf("POST %s = %d, want %d (body %s)", path, rec.Code, tt.wantStatus, rec.Body.String())
				}
				if got := rec.Header().Get("WWW-Authenticate"); (tt.wantStatus == http.StatusUnauthorized) != (got != "") {
					t.Errorf("WWW-Authenticate = %q on a %d", got, rec.Code)
				}
			})
		}
	}
}

func TestNewAccessToken(t *testing.T) {
	t.Parallel()

	a, err := newAccessToken()
	if err != nil {
		t.Fatalf("newAccessToken: %v", err)
	}
	b, _ := newAccessToken()
	if !strings.HasPrefix(a, accessTokenPrefix) || len(a) != len(accessTokenPrefix)+64 || a == b {
		t.Errorf("newAccessToken() = %q, %q, want two distinct prefixed 256-bit tokens", a, b)
	}
}
