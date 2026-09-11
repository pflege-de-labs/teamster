package httpserver

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

const (
	loginFlowTTL      = 10 * time.Minute
	maxDiscoveryBytes = 1 << 20
)

var discoveryClient = &http.Client{Timeout: 15 * time.Second}

// oidcProvider is discovered lazily so an unreachable identity provider delays
// a login rather than preventing the service from starting.
type oidcProvider struct {
	mu       sync.Mutex
	provider *oidc.Provider
}

func (s *Server) oidcEnabled() bool {
	return s.cfg.Auth.OIDCDiscoveryURL != ""
}

func (s *Server) discover(ctx context.Context) (*oidc.Provider, error) {
	s.oidc.mu.Lock()
	defer s.oidc.mu.Unlock()

	if s.oidc.provider != nil {
		return s.oidc.provider, nil
	}

	config, err := fetchProviderConfig(ctx, s.cfg.Auth.OIDCDiscoveryURL)
	if err != nil {
		return nil, err
	}

	// The issuer comes from the document rather than from configuration, and id
	// tokens are then verified against it.
	provider := config.NewProvider(ctx)
	s.oidc.provider = provider
	return provider, nil
}

// fetchProviderConfig reads the document the operator pointed at, rather than
// deriving its location from an issuer, because a provider is free to publish
// it anywhere.
func fetchProviderConfig(ctx context.Context, discoveryURL string) (*oidc.ProviderConfig, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, discoveryURL, nil)
	if err != nil {
		return nil, fmt.Errorf("discovery request: %w", err)
	}

	resp, err := discoveryClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", discoveryURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: %s", discoveryURL, resp.Status)
	}

	var config oidc.ProviderConfig
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxDiscoveryBytes)).Decode(&config); err != nil {
		return nil, fmt.Errorf("decode %s: %w", discoveryURL, err)
	}

	// Ordered, so a document missing several fields always names the same one.
	required := []struct {
		name  string
		value string
	}{
		{"issuer", config.IssuerURL},
		{"authorization_endpoint", config.AuthURL},
		{"token_endpoint", config.TokenURL},
		{"jwks_uri", config.JWKSURL},
	}
	for _, field := range required {
		if field.value == "" {
			return nil, fmt.Errorf("%s advertises no %s", discoveryURL, field.name)
		}
	}
	return &config, nil
}

func (s *Server) oauthConfig(provider *oidc.Provider) oauth2.Config {
	scopes := append([]string{oidc.ScopeOpenID}, s.cfg.Auth.OIDCScopes...)
	return oauth2.Config{
		ClientID:     s.cfg.Auth.OIDCClientID,
		ClientSecret: s.cfg.Auth.OIDCClientSecret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  s.cfg.Auth.OIDCRedirectURL,
		Scopes:       scopes,
	}
}

func (s *Server) handleAuthStart(w http.ResponseWriter, r *http.Request) {
	if !s.oidcEnabled() {
		http.Redirect(w, r, "/admin/login?error=no+identity+provider+configured", http.StatusFound)
		return
	}

	provider, err := s.discover(r.Context())
	if err != nil {
		logError("oidc discovery", err)
		http.Redirect(w, r, "/admin/login?error=identity+provider+unreachable", http.StatusFound)
		return
	}

	state, err := newToken()
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=could+not+start+login", http.StatusFound)
		return
	}
	nonce, err := newToken()
	if err != nil {
		http.Redirect(w, r, "/admin/login?error=could+not+start+login", http.StatusFound)
		return
	}
	verifier := oauth2.GenerateVerifier()

	if err := s.store.CreateLoginFlow(models.LoginFlow{
		State: state, Verifier: verifier, Nonce: nonce,
		ExpiresAt: time.Now().UTC().Add(loginFlowTTL),
	}); err != nil {
		logError("store login flow", err)
		http.Redirect(w, r, "/admin/login?error=could+not+start+login", http.StatusFound)
		return
	}

	config := s.oauthConfig(provider)
	http.Redirect(w, r, config.AuthCodeURL(state,
		oidc.Nonce(nonce),
		oauth2.S256ChallengeOption(verifier),
	), http.StatusFound)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidcEnabled() {
		http.NotFound(w, r)
		return
	}

	if authErr := r.URL.Query().Get("error"); authErr != "" {
		loginFailed(w, r, providerError(authErr, r.URL.Query().Get("error_description")))
		return
	}

	// The flow is redeemed here, so a replayed callback finds nothing.
	flow, err := s.store.TakeLoginFlow(r.URL.Query().Get("state"))
	if err != nil {
		loginFailed(w, r, "this login did not start here, or it expired")
		return
	}

	provider, err := s.discover(r.Context())
	if err != nil {
		logError("oidc discovery", err)
		loginFailed(w, r, "identity provider unreachable")
		return
	}

	config := s.oauthConfig(provider)
	token, err := config.Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		logError("oidc exchange", err)
		loginFailed(w, r, "the identity provider rejected the login")
		return
	}

	subject, name, roles, err := s.verifyIDToken(r.Context(), provider, token, flow.Nonce)
	if err != nil {
		logError("oidc verify", err)
		loginFailed(w, r, err.Error())
		return
	}

	if err := s.startSession(w, r, subject, name, "oidc", roles); err != nil {
		logError("start session", err)
		loginFailed(w, r, "could not start a session")
		return
	}

	http.Redirect(w, r, "/admin", http.StatusFound)
}

func (s *Server) verifyIDToken(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, nonce string) (string, string, []authz.Role, error) {
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return "", "", nil, errors.New("the identity provider returned no id token")
	}

	idToken, err := provider.Verifier(&oidc.Config{ClientID: s.cfg.Auth.OIDCClientID}).Verify(ctx, raw)
	if err != nil {
		return "", "", nil, fmt.Errorf("id token rejected: %w", err)
	}
	if idToken.Nonce != nonce {
		return "", "", nil, errors.New("id token nonce does not match this login")
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return "", "", nil, fmt.Errorf("read claims: %w", err)
	}

	// Whether the claim names a role decides what this user may do, not whether
	// they may sign in: a user with no role signs in and is told so, which is a
	// better answer than a login that fails for reasons they cannot see.
	values, _ := s.membership(ctx, provider, token, claims)

	name, _ := claims["preferred_username"].(string)
	if name == "" {
		name, _ = claims["name"].(string)
	}
	return idToken.Subject, name, s.rolesFor(values), nil
}

// rolesFor maps the claim onto roles by name — a provider role called "editor"
// is the editor role here, and one this build has never heard of is handed to
// the policies unchanged — falling back to the configured default.
func (s *Server) rolesFor(values []string) []authz.Role {
	return authz.RolesFor(values, authz.Role(s.cfg.Auth.DefaultRole))
}

// membership looks for the configured claim in the id token, then in userinfo,
// then in the access token. Keycloak's role mappers add roles to the access
// token by default and leave the id token without them, so an id-token-only
// lookup rejects a correctly configured realm.
func (s *Server) membership(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, claims map[string]any) ([]string, string) {
	if values := claimValues(claims, s.cfg.Auth.Claim); len(values) > 0 {
		return values, "the id token"
	}

	if info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token)); err != nil {
		logError("userinfo", err)
	} else {
		var infoClaims map[string]any
		if err := info.Claims(&infoClaims); err != nil {
			logError("userinfo claims", err)
		} else if values := claimValues(infoClaims, s.cfg.Auth.Claim); len(values) > 0 {
			return values, "userinfo"
		}
	}

	if values := claimValues(accessTokenClaims(token.AccessToken), s.cfg.Auth.Claim); len(values) > 0 {
		return values, "the access token"
	}
	return nil, ""
}

// accessTokenClaims reads the payload without verifying it. The token came
// straight from the token endpoint over TLS alongside an id token that was
// verified, and it is only ever read to look up membership.
func accessTokenClaims(accessToken string) map[string]any {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return nil
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil
	}

	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil
	}
	return claims
}

// providerError keeps the description the provider sent. The code alone reads
// as "access_denied" for anything from a declined consent to a broken upstream
// federation, and the description is what distinguishes them.
func providerError(code, description string) string {
	if description == "" {
		return code
	}
	return code + ": " + description
}

func loginFailed(w http.ResponseWriter, r *http.Request, message string) {
	http.Redirect(w, r, "/admin/login?error="+urlQueryEscape(message), http.StatusFound)
}
