package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func authServer(t *testing.T, st *fakeStore, auth config.AuthConfig) http.Handler {
	t.Helper()

	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Auth:    auth,
	}
	return NewServer(cfg, st, &fakeMessenger{}).Handler
}

// Anonymous is the state that matters: the admin pages must not be reachable
// without a session, however the request is dressed up.
func TestAdminRequiresASession(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), config.AuthConfig{})

	tests := []struct {
		name   string
		path   string
		cookie string
		basic  bool
	}{
		{name: "no credentials at all", path: "/admin"},
		{name: "basic auth is no longer enough for the UI", path: "/admin", basic: true},
		{name: "an unknown session id", path: "/admin", cookie: "not-a-session"},
		{name: "the root page", path: "/"},
		{name: "a form endpoint", path: "/admin/templates"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			if tt.basic {
				req.SetBasicAuth("admin", "pass")
			}
			if tt.cookie != "" {
				req.AddCookie(&http.Cookie{Name: sessionCookie, Value: tt.cookie})
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("GET %s = %d, want a redirect to the login page", tt.path, rec.Code)
			}
			if got := rec.Header().Get("Location"); got != "/admin/login" {
				t.Errorf("Location = %q, want /admin/login", got)
			}
		})
	}
}

// Automation cannot complete an authorization code flow, so the JSON API keeps
// accepting basic auth.
func TestAPIStillAcceptsBasicAuth(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), config.AuthConfig{})

	req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	req.SetBasicAuth("admin", "pass")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/templates with basic auth = %d, want 200", rec.Code)
	}

	anonymous := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, anonymous)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous API request = %d, want 401", rec.Code)
	}
}

func TestLocalLoginCreatesASession(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	delete(st.sessions, testSessionID)
	handler := authServer(t, st, config.AuthConfig{})

	rec := postLogin(t, handler, url.Values{"username": {"admin"}, "password": {"pass"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("login = %d, want 303 (%s)", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/admin" {
		t.Errorf("Location = %q, want /admin", got)
	}

	cookie := sessionCookieFrom(t, rec)
	session, err := st.GetSession(cookie.Value)
	if err != nil {
		t.Fatalf("the login did not store a session: %v", err)
	}
	if session.Source != "local" || session.Name != "admin" {
		t.Errorf("session = %+v, want a local login by admin", session)
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("cookie = %+v, want HttpOnly and SameSite=Lax", cookie)
	}
}

func TestLocalLoginRefusesWrongCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		form url.Values
	}{
		{name: "wrong password", form: url.Values{"username": {"admin"}, "password": {"nope"}}},
		{name: "wrong username", form: url.Values{"username": {"root"}, "password": {"pass"}}},
		{name: "nothing at all", form: url.Values{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			delete(st.sessions, testSessionID)
			rec := postLogin(t, authServer(t, st, config.AuthConfig{}), tt.form)

			if rec.Code != http.StatusFound {
				t.Fatalf("login = %d, want a redirect back to the form", rec.Code)
			}
			if !strings.Contains(rec.Header().Get("Location"), "wrong+username+or+password") {
				t.Errorf("Location = %q, want the failure reported", rec.Header().Get("Location"))
			}
			if len(st.sessions) != 0 {
				t.Error("a failed login created a session")
			}
		})
	}
}

// With no local credentials configured, the form must not become a way in.
func TestLocalLoginRefusedWhenNoCredentialsConfigured(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	delete(st.sessions, testSessionID)
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Auth:    config.AuthConfig{},
	}
	handler := NewServer(cfg, st, &fakeMessenger{}).Handler

	rec := postLogin(t, handler, url.Values{"username": {""}, "password": {""}})
	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "wrong") {
		t.Fatalf("empty credentials = %d %q, want refusal", rec.Code, rec.Header().Get("Location"))
	}
	if len(st.sessions) != 0 {
		t.Error("an empty-credential login created a session")
	}
}

func TestLogoutEndsTheSession(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	handler := authServer(t, st, config.AuthConfig{})

	req := httptest.NewRequest(http.MethodPost, "/admin/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("logout = %d, want 303", rec.Code)
	}
	if _, ok := st.sessions[testSessionID]; ok {
		t.Error("the session survived logout")
	}
	if cookie := sessionCookieFrom(t, rec); cookie.MaxAge >= 0 {
		t.Errorf("cookie MaxAge = %d, want it cleared", cookie.MaxAge)
	}
}

func TestLoginPageOffersTheConfiguredWaysIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		auth       config.AuthConfig
		local      bool
		wantOIDC   bool
		wantForm   bool
		wantNoWays bool
	}{
		{name: "local only", local: true, wantForm: true},
		{name: "oidc only", auth: config.AuthConfig{OIDCIssuer: "https://idp.example"}, wantOIDC: true},
		{name: "both", auth: config.AuthConfig{OIDCIssuer: "https://idp.example"}, local: true, wantOIDC: true, wantForm: true},
		{name: "neither", wantNoWays: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Auth:    tt.auth,
			}
			if tt.local {
				cfg.Admin = config.AdminConfig{Username: "admin", Password: "pass"}
			}

			handler := NewServer(cfg, newFakeStore(), &fakeMessenger{}).Handler
			req := httptest.NewRequest(http.MethodGet, "/admin/login", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET /admin/login = %d, want 200", rec.Code)
			}

			body := rec.Body.String()
			if got := strings.Contains(body, "/admin/auth/start"); got != tt.wantOIDC {
				t.Errorf("offers OIDC = %v, want %v", got, tt.wantOIDC)
			}
			if got := strings.Contains(body, `name="password"`); got != tt.wantForm {
				t.Errorf("offers the password form = %v, want %v", got, tt.wantForm)
			}
			if tt.wantNoWays && !strings.Contains(body, "No way to sign in is configured") {
				t.Error("a deployment with no way in does not say so")
			}
		})
	}
}

func TestAuthStartWithoutAnIssuer(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), config.AuthConfig{})
	req := httptest.NewRequest(http.MethodGet, "/admin/auth/start", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "no+identity+provider") {
		t.Errorf("auth start = %d %q, want a redirect explaining there is no provider", rec.Code, rec.Header().Get("Location"))
	}
}

// A callback that this service did not start must not create a session, which
// is what the single-use flow row buys.
func TestAuthCallbackRejectsAnUnknownState(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	handler := authServer(t, st, config.AuthConfig{
		OIDCIssuer: "https://idp.example", OIDCClientID: "teamster",
		OIDCRedirectURL: "https://teamster.example/admin/auth/callback",
		Claim:           "realm_access.roles", Allowed: []string{"admin"},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/callback?state=forged&code=whatever", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("callback = %d, want a redirect back to the login page", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "did+not+start+here") {
		t.Errorf("Location = %q, want the forged state refused", rec.Header().Get("Location"))
	}
	if len(st.sessions) != 1 {
		t.Error("a forged callback changed the sessions")
	}
}

func TestAuthCallbackReportsProviderErrors(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), config.AuthConfig{
		OIDCIssuer: "https://idp.example", OIDCClientID: "teamster",
		OIDCRedirectURL: "https://teamster.example/admin/auth/callback",
		Claim:           "realm_access.roles", Allowed: []string{"admin"},
	})

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/callback?error=access_denied", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !strings.Contains(rec.Header().Get("Location"), "access_denied") {
		t.Errorf("Location = %q, want the provider's refusal shown", rec.Header().Get("Location"))
	}
}

func TestSessionCookieIsSecureBehindTLS(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	delete(st.sessions, testSessionID)
	handler := authServer(t, st, config.AuthConfig{})

	req := httptest.NewRequest(http.MethodPost, "/admin/login",
		strings.NewReader(url.Values{"username": {"admin"}, "password": {"pass"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if cookie := sessionCookieFrom(t, rec); !cookie.Secure {
		t.Error("the cookie is not Secure behind a TLS-terminating proxy")
	}
}

func TestExpiredSessionIsNotHonoured(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.sessions["stale"] = models.Session{ID: "stale", ExpiresAt: time.Now().Add(-time.Minute)}
	handler := authServer(t, st, config.AuthConfig{})

	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "stale"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Errorf("an expired session was honoured: %d", rec.Code)
	}
}

func postLogin(t *testing.T, handler http.Handler, form url.Values) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/admin/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func sessionCookieFrom(t *testing.T, rec *httptest.ResponseRecorder) *http.Cookie {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == sessionCookie {
			return cookie
		}
	}
	t.Fatal("no session cookie was set")
	return nil
}

// A username configured without a password would otherwise let anyone in by
// submitting that username and an empty password.
func TestEmptyConfiguredPasswordAdmitsNobody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		admin config.AdminConfig
		form  url.Values
	}{
		{
			name:  "empty password configured, empty password submitted",
			admin: config.AdminConfig{Username: "admin"},
			form:  url.Values{"username": {"admin"}, "password": {""}},
		},
		{
			name:  "empty username configured",
			admin: config.AdminConfig{Password: "pass"},
			form:  url.Values{"username": {""}, "password": {"pass"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			delete(st.sessions, testSessionID)
			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   tt.admin,
			}
			handler := NewServer(cfg, st, &fakeMessenger{}).Handler

			rec := postLogin(t, handler, tt.form)
			if rec.Code != http.StatusFound || !strings.Contains(rec.Header().Get("Location"), "wrong") {
				t.Errorf("login = %d %q, want refusal", rec.Code, rec.Header().Get("Location"))
			}
			if len(st.sessions) != 0 {
				t.Fatal("a half-configured local login created a session")
			}

			// The same must hold for the API, which shares those credentials.
			req := httptest.NewRequest(http.MethodGet, "/api/templates", nil)
			req.SetBasicAuth(tt.form.Get("username"), tt.form.Get("password"))
			apiRec := httptest.NewRecorder()
			handler.ServeHTTP(apiRec, req)
			if apiRec.Code != http.StatusUnauthorized {
				t.Errorf("API with half-configured credentials = %d, want 401", apiRec.Code)
			}

			// And the page must not offer a form that cannot work.
			page := httptest.NewRecorder()
			handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/admin/login", nil))
			if strings.Contains(page.Body.String(), `name="password"`) {
				t.Error("the login page offers a password form that cannot succeed")
			}
		})
	}
}
