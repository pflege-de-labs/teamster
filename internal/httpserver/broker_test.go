package httpserver

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/cryptutil"
	"github.com/pflege-de-labs/teamster/internal/models"
)

const testEncryptionKey = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func testSealer(t *testing.T) *cryptutil.Sealer {
	t.Helper()

	key, err := base64.StdEncoding.DecodeString(testEncryptionKey)
	if err != nil {
		t.Fatalf("decode test key: %v", err)
	}
	sealer, err := cryptutil.NewSealer(key)
	if err != nil {
		t.Fatalf("NewSealer: %v", err)
	}
	return sealer
}

// seedBrokerToken seals plaintext and refresh under sessionID -- the same aad
// the server itself uses -- and writes it straight into the fake store, as if
// a previous OIDC login had stored it.
func seedBrokerToken(t *testing.T, st *fakeStore, sessionID, access, refresh string, expiresAt time.Time) {
	t.Helper()

	sealer := testSealer(t)
	sealedAccess, err := sealer.Seal([]byte(access), []byte(sessionID))
	if err != nil {
		t.Fatalf("seal access: %v", err)
	}
	sealedRefresh, err := sealer.Seal([]byte(refresh), []byte(sessionID))
	if err != nil {
		t.Fatalf("seal refresh: %v", err)
	}

	st.brokerTokens[sessionID] = models.BrokerToken{
		SessionID:    sessionID,
		AccessToken:  base64.StdEncoding.EncodeToString(sealedAccess),
		RefreshToken: base64.StdEncoding.EncodeToString(sealedRefresh),
		ExpiresAt:    expiresAt,
		UpdatedAt:    time.Now().UTC(),
	}
}

// openStoredAccessToken decrypts what is currently stored for sessionID, so a
// test can tell a refresh actually happened rather than trusting the response
// body alone.
func openStoredAccessToken(t *testing.T, st *fakeStore, sessionID string) string {
	t.Helper()

	sealer := testSealer(t)
	tok, ok := st.brokerTokens[sessionID]
	if !ok {
		t.Fatalf("no broker token stored for %s", sessionID)
	}
	raw, err := base64.StdEncoding.DecodeString(tok.AccessToken)
	if err != nil {
		t.Fatalf("decode stored access token: %v", err)
	}
	plaintext, err := sealer.Open(raw, []byte(sessionID))
	if err != nil {
		t.Fatalf("open stored access token: %v", err)
	}
	return string(plaintext)
}

// brokerConfig is a Config wired for the delegated Teams/Channels feature,
// pointed at the given Keycloak and Graph stand-ins.
func brokerConfig(idpURL, graphURL string) config.Config {
	return config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Auth: config.AuthConfig{
			OIDCDiscoveryURL: idpURL + "/.well-known/openid-configuration",
			OIDCClientID:     "teamster",
			OIDCRedirectURL:  "https://teamster.example/admin/auth/callback",
			Claim:            "realm_access.roles",
			Broker: config.BrokerConfig{
				Enabled: true, IdPAlias: "entra-broker", TokenEncryptionKey: testEncryptionKey,
			},
		},
		Graph: config.GraphConfig{BaseURL: graphURL},
	}
}

// oidcSession makes the seeded test session look like an OIDC login: the
// broker only ever considers a session with Source "oidc" for delegated
// Teams/Channels.
func oidcSession(st *fakeStore) {
	session := st.sessions[testSessionID]
	session.Source = "oidc"
	st.sessions[testSessionID] = session
}

func TestHandleMyTeamsRequiresBrokerAndOIDCSession(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		enabled bool
		source  string
	}{
		{name: "broker disabled", enabled: false, source: "oidc"},
		{name: "local session", enabled: true, source: "local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
			}
			if tt.enabled {
				cfg.Auth = config.AuthConfig{
					OIDCDiscoveryURL: "https://idp.example/.well-known/openid-configuration",
					Broker:           config.BrokerConfig{Enabled: true, IdPAlias: "alias", TokenEncryptionKey: testEncryptionKey},
				}
			}

			st := newFakeStore()
			session := st.sessions[testSessionID]
			session.Source = tt.source
			st.sessions[testSessionID] = session

			handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler

			for _, path := range []string{"/api/graph/my-teams", "/api/graph/my-teams/team-1/channels"} {
				rec := do(t, handler, http.MethodGet, path, "")
				if rec.Code != http.StatusConflict {
					t.Errorf("GET %s = %d, want 409 (%s)", path, rec.Code, rec.Body.String())
				}
			}
		})
	}
}

func TestHandleMyTeamsRejectsNonGet(t *testing.T) {
	t.Parallel()

	cfg := brokerConfig("https://idp.example", "https://graph.example")
	st := newFakeStore()
	oidcSession(st)
	handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler

	for _, path := range []string{"/api/graph/my-teams", "/api/graph/my-teams/team-1/channels"} {
		rec := do(t, handler, http.MethodPost, path, "")
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, rec.Code)
		}
	}
}

// The path parsing must reject the same shapes handleGraphChannels does,
// before ever reaching Keycloak or Graph -- this test configures neither.
func TestHandleMyChannelsPathParsing(t *testing.T) {
	t.Parallel()

	cfg := brokerConfig("https://idp.example", "https://graph.example")
	st := newFakeStore()
	oidcSession(st)
	handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{name: "a team literally named channels is not a team id", path: "/api/graph/my-teams/channels", wantStatus: http.StatusNotFound},
		{name: "nested path", path: "/api/graph/my-teams/a/b/channels", wantStatus: http.StatusNotFound},
		{name: "team id without the channels suffix", path: "/api/graph/my-teams/team-1", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := do(t, handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.wantStatus {
				t.Fatalf("GET %s = %d, want %d (%s)", tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// The full round trip: a stored, still-valid Keycloak token is decrypted, its
// access token is exchanged at Keycloak's broker endpoint for an Entra token,
// and that Entra token is what reaches Graph -- never this service's own
// app-only credential.
func TestHandleMyTeamsAndMyChannelsEndToEnd(t *testing.T) {
	t.Parallel()

	var brokerAuth string
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			// The discovery document embeds the issuer, which is this
			// server's own URL -- known only once it is listening, hence the
			// self-reference through the closure rather than a fixed string.
			_, _ = w.Write([]byte(discoveryDocument(idp.URL)))
		case "/broker/entra-broker/token":
			brokerAuth = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"entra-token-xyz","token_type":"bearer"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer idp.Close()

	var graphAuth, graphPath string
	graphSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		graphAuth, graphPath = r.Header.Get("Authorization"), r.URL.Path
		switch r.URL.Path {
		case "/me/joinedTeams":
			_, _ = w.Write([]byte(`{"value":[{"id":"t1","displayName":"Operations"}]}`))
		case "/teams/t1/channels":
			_, _ = w.Write([]byte(`{"value":[{"id":"c1","displayName":"General"}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer graphSrv.Close()

	cfg := brokerConfig(idp.URL, graphSrv.URL)
	st := newFakeStore()
	oidcSession(st)
	seedBrokerToken(t, st, testSessionID, "kc-access-1", "kc-refresh-1", time.Now().Add(time.Hour))

	handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodGet, "/api/graph/my-teams", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/my-teams = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if brokerAuth != "Bearer kc-access-1" {
		t.Errorf("broker endpoint Authorization = %q, want the stored Keycloak access token", brokerAuth)
	}
	if graphAuth != "Bearer entra-token-xyz" {
		t.Errorf("Graph Authorization = %q, want the Entra token from the broker endpoint", graphAuth)
	}
	if graphPath != "/me/joinedTeams" {
		t.Errorf("Graph path = %q, want /me/joinedTeams", graphPath)
	}

	var payload struct {
		Teams []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Teams) != 1 || payload.Teams[0].Name != "Operations" {
		t.Errorf("teams = %+v, want the one fake team", payload.Teams)
	}

	channelsRec := do(t, handler, http.MethodGet, "/api/graph/my-teams/t1/channels", "")
	if channelsRec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/my-teams/t1/channels = %d, want 200 (%s)", channelsRec.Code, channelsRec.Body.String())
	}
	if graphPath != "/teams/t1/channels" {
		t.Errorf("Graph path = %q, want /teams/t1/channels", graphPath)
	}
}

// A stored Keycloak token past its expiry is refreshed against Keycloak's own
// token endpoint before it is ever handed to the broker endpoint, and the
// refreshed token is persisted, re-encrypted, so a second request need not
// refresh again.
func TestHandleMyTeamsRefreshesAStaleToken(t *testing.T) {
	t.Parallel()

	var tokenRequests int
	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(discoveryDocument(idp.URL)))
		case "/protocol/openid-connect/token":
			tokenRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"kc-access-2","refresh_token":"kc-refresh-2","token_type":"Bearer","expires_in":3600}`))
		case "/broker/entra-broker/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"entra-token-xyz"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer idp.Close()

	graphSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"value":[]}`))
	}))
	defer graphSrv.Close()

	cfg := brokerConfig(idp.URL, graphSrv.URL)
	st := newFakeStore()
	oidcSession(st)
	// Already expired, so the token source must refresh before doing anything else.
	seedBrokerToken(t, st, testSessionID, "kc-access-1", "kc-refresh-1", time.Now().Add(-time.Hour))

	handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler
	rec := do(t, handler, http.MethodGet, "/api/graph/my-teams", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/graph/my-teams = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if tokenRequests != 1 {
		t.Fatalf("Keycloak token endpoint called %d times, want 1", tokenRequests)
	}

	if got := openStoredAccessToken(t, st, testSessionID); got != "kc-access-2" {
		t.Errorf("stored access token after refresh = %q, want the refreshed one persisted", got)
	}
}

// Keycloak firmly rejecting the refresh token -- expired or revoked -- is a
// dead credential, not a transient failure: the stored row must be removed so
// the next attempt fails fast rather than retrying it forever.
func TestHandleMyTeamsHardRefreshFailureDeletesTheStoredToken(t *testing.T) {
	t.Parallel()

	var idp *httptest.Server
	idp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(discoveryDocument(idp.URL)))
		case "/protocol/openid-connect/token":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"Token is not active"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer idp.Close()

	cfg := brokerConfig(idp.URL, "https://graph.example")
	st := newFakeStore()
	oidcSession(st)
	seedBrokerToken(t, st, testSessionID, "kc-access-1", "kc-refresh-1", time.Now().Add(-time.Hour))

	handler := mustServer(t, cfg, st, &fakeMessenger{}).Handler
	rec := do(t, handler, http.MethodGet, "/api/graph/my-teams", "")
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("GET /api/graph/my-teams = %d, want 502 (%s)", rec.Code, rec.Body.String())
	}

	if _, ok := st.brokerTokens[testSessionID]; ok {
		t.Error("the broker token survived a hard refresh failure, want it removed")
	}
	if _, ok := st.sessions[testSessionID]; !ok {
		t.Error("the session itself was deleted by a broker failure, want only the broker token touched")
	}
}

func TestHardRefreshFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "network error", err: errors.New("dial tcp: connection refused"), want: false},
		{
			name: "400 invalid_grant",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}, ErrorCode: "invalid_grant"},
			want: true,
		},
		{
			name: "401",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusUnauthorized}},
			want: true,
		},
		{
			name: "500, transient",
			err:  &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusInternalServerError}},
			want: false,
		},
		{
			name: "wrapped 400",
			err:  fmt.Errorf("refresh: %w", &oauth2.RetrieveError{Response: &http.Response{StatusCode: http.StatusBadRequest}}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := hardRefreshFailure(tt.err); got != tt.want {
				t.Errorf("hardRefreshFailure(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// storeBrokerToken is what handleAuthCallback calls right after startSession;
// it is exercised directly here rather than through a full signed-JWT login
// round trip, which this package has no fixture for building (bot_auth_test.go
// signs Bot Framework tokens, not OIDC id tokens).
func TestStoreBrokerTokenRefusesAMissingRefreshToken(t *testing.T) {
	t.Parallel()

	s := &Server{sealer: testSealer(t), store: newFakeStore()}
	err := s.storeBrokerToken(t.Context(), "sess-1", &oauth2.Token{AccessToken: "a"})
	if err == nil {
		t.Error("storeBrokerToken with no refresh token = nil error, want it refused")
	}
}

func TestStoreBrokerTokenPersistsSealedFields(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	s := &Server{sealer: testSealer(t), store: st}

	tok := &oauth2.Token{AccessToken: "kc-access", RefreshToken: "kc-refresh", Expiry: time.Now().Add(time.Hour)}
	if err := s.storeBrokerToken(t.Context(), "sess-1", tok); err != nil {
		t.Fatalf("storeBrokerToken: %v", err)
	}

	stored, ok := st.brokerTokens["sess-1"]
	if !ok {
		t.Fatal("no broker token was stored")
	}
	if stored.AccessToken == "kc-access" || stored.RefreshToken == "kc-refresh" {
		t.Error("the stored fields are plaintext, want them sealed")
	}
	if got := openStoredAccessToken(t, st, "sess-1"); got != "kc-access" {
		t.Errorf("stored access token decrypts to %q, want kc-access", got)
	}
}

// Sealing is keyed to the session id as AAD, so a row cannot be opened as if
// it belonged to a different session -- the same guarantee cryptutil's own
// tests check in isolation, verified here through the server's actual use of it.
func TestStoreBrokerTokenSealsWithTheSessionIDAsAAD(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	s := &Server{sealer: testSealer(t), store: st}

	tok := &oauth2.Token{AccessToken: "kc-access", RefreshToken: "kc-refresh", Expiry: time.Now().Add(time.Hour)}
	if err := s.storeBrokerToken(t.Context(), "sess-1", tok); err != nil {
		t.Fatalf("storeBrokerToken: %v", err)
	}

	stored := st.brokerTokens["sess-1"]
	raw, err := base64.StdEncoding.DecodeString(stored.AccessToken)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := testSealer(t).Open(raw, []byte("sess-2")); err == nil {
		t.Error("Open() under a different session id = nil error, want it refused")
	}
}
