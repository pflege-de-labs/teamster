package httpserver

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func discoveryDocument(issuer string) string {
	return fmt.Sprintf(`{
		"issuer": %q,
		"authorization_endpoint": "%s/protocol/openid-connect/auth",
		"token_endpoint": "%s/protocol/openid-connect/token",
		"jwks_uri": "%s/protocol/openid-connect/certs",
		"end_session_endpoint": "%s/protocol/openid-connect/logout",
		"code_challenge_methods_supported": ["plain", "S256"]
	}`, issuer, issuer, issuer, issuer, issuer)
}

// The document is fetched from where the operator pointed, and the issuer is
// taken from it rather than derived from the URL.
func TestFetchProviderConfigReadsTheDocument(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(discoveryDocument("https://login.example/auth/realms/internal")))
	}))
	defer srv.Close()

	config, err := fetchProviderConfig(context.Background(), srv.URL+"/auth/realms/internal/.well-known/openid-configuration")
	if err != nil {
		t.Fatalf("fetchProviderConfig: %v", err)
	}

	if gotPath != "/auth/realms/internal/.well-known/openid-configuration" {
		t.Errorf("fetched %q, want the configured path verbatim", gotPath)
	}
	if config.IssuerURL != "https://login.example/auth/realms/internal" {
		t.Errorf("IssuerURL = %q, want the issuer from the document", config.IssuerURL)
	}
	if !strings.HasSuffix(config.AuthURL, "/protocol/openid-connect/auth") {
		t.Errorf("AuthURL = %q, want the advertised authorization endpoint", config.AuthURL)
	}
}

func TestFetchProviderConfigRejectsUnusableDocuments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string
	}{
		{
			name:    "not found",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantErr: "404",
		},
		{
			name:    "not json",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>login page</html>")) },
			wantErr: "decode",
		},
		{
			name: "no issuer",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"token_endpoint":"https://x/t"}`))
			},
			wantErr: "advertises no issuer",
		},
		{
			name: "no jwks",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"issuer":"https://x","authorization_endpoint":"https://x/a","token_endpoint":"https://x/t"}`))
			},
			wantErr: "advertises no jwks_uri",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			_, err := fetchProviderConfig(context.Background(), srv.URL+"/.well-known/openid-configuration")
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("fetchProviderConfig() = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}

func TestFetchProviderConfigReportsAnUnreachableProvider(t *testing.T) {
	t.Parallel()

	_, err := fetchProviderConfig(context.Background(), "http://127.0.0.1:1/.well-known/openid-configuration")
	if err == nil || !strings.Contains(err.Error(), "fetch") {
		t.Errorf("fetchProviderConfig() = %v, want an unreachable provider reported", err)
	}
}

// An unreachable provider must send the operator back to the login page, where
// the local credentials still work, rather than failing the request outright.
func TestAuthStartSurvivesAnUnreachableProvider(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), authConfigFor("http://127.0.0.1:1/.well-known/openid-configuration"))

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/start", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("auth start = %d, want a redirect", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Location"), "unreachable") {
		t.Errorf("Location = %q, want the failure explained", rec.Header().Get("Location"))
	}
}

// The flow is stored before the redirect, so the callback can prove it started here.
func TestAuthStartStoresTheFlowAndRedirectsWithPKCE(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(discoveryDocument("https://login.example/auth/realms/internal")))
	}))
	defer srv.Close()

	st := newFakeStore()
	handler := authServer(t, st, authConfigFor(srv.URL+"/.well-known/openid-configuration"))

	req := httptest.NewRequest(http.MethodGet, "/admin/auth/start", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("auth start = %d, want a redirect to the provider (%s)", rec.Code, rec.Header().Get("Location"))
	}

	location := rec.Header().Get("Location")
	for _, want := range []string{
		"https://login.example/auth/realms/internal/protocol/openid-connect/auth",
		"client_id=teamster", "code_challenge_method=S256", "code_challenge=", "state=", "nonce=",
	} {
		if !strings.Contains(location, want) {
			t.Errorf("Location %q is missing %q", location, want)
		}
	}

	if len(st.loginFlows) != 1 {
		t.Fatalf("stored %d login flows, want 1", len(st.loginFlows))
	}
	for _, flow := range st.loginFlows {
		if flow.Verifier == "" || flow.Nonce == "" {
			t.Errorf("flow = %+v, want a verifier and a nonce stored", flow)
		}
		if !strings.Contains(location, "state="+flow.State) {
			t.Error("the redirect carries a different state than the one stored")
		}
	}
}

// The provider's own description is the only thing that distinguishes a
// declined consent from a broken upstream federation.
func TestAuthCallbackShowsTheProviderDescription(t *testing.T) {
	t.Parallel()

	handler := authServer(t, newFakeStore(), authConfigFor("https://idp.example/.well-known/openid-configuration"))

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "code and description",
			query: "?error=access_denied&error_description=Unexpected+error+when+authenticating+with+identity+provider",
			want:  []string{"access_denied", "Unexpected error when authenticating with identity provider"},
		},
		{
			name:  "code alone",
			query: "?error=temporarily_unavailable",
			want:  []string{"temporarily_unavailable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/admin/auth/callback"+tt.query, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Fatalf("callback = %d, want a redirect to the login page", rec.Code)
			}

			location, err := url.Parse(rec.Header().Get("Location"))
			if err != nil {
				t.Fatalf("parse Location: %v", err)
			}
			shown := location.Query().Get("error")
			for _, want := range tt.want {
				if !strings.Contains(shown, want) {
					t.Errorf("login page would show %q, want it to contain %q", shown, want)
				}
			}
		})
	}
}
