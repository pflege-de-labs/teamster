package httpserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/oauth2"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// storeBrokerToken seals and persists sessionID's Keycloak token right after
// an OIDC login, so a later "my Teams" request can ask Keycloak's broker
// endpoint for the Entra token it already stored -- Teamster never holds an
// Entra credential of its own for this. See ADR 0037.
//
// A Keycloak realm without offline access issues no refresh token, in which
// case there is nothing durable enough to store: "my Teams" would work only
// until the access token expires and then fail silently for the rest of the
// session. Refusing to store anything here is what turns that into a visible
// configuration problem the first time the feature is used, rather than a
// slow, unexplained failure later.
func (s *Server) storeBrokerToken(ctx context.Context, sessionID string, token *oauth2.Token) error {
	if token.RefreshToken == "" {
		return errors.New("keycloak issued no refresh token; grant offline_access for auth-broker-enabled")
	}

	access, err := s.sealTokenField(sessionID, token.AccessToken)
	if err != nil {
		return err
	}
	refresh, err := s.sealTokenField(sessionID, token.RefreshToken)
	if err != nil {
		return err
	}

	return s.store.CreateBrokerToken(ctx, models.BrokerToken{
		SessionID: sessionID, AccessToken: access, RefreshToken: refresh,
		ExpiresAt: token.Expiry, UpdatedAt: time.Now().UTC(),
	})
}

// sealTokenField and openTokenField are Seal/Open plus the base64 encoding
// that lets a TEXT column carry the ciphertext, matching this codebase's
// TEXT-only column convention. The session id is the AAD both directions, so
// a row cannot be decrypted as if it belonged to a different session.
func (s *Server) sealTokenField(sessionID, plaintext string) (string, error) {
	sealed, err := s.sealer.Seal([]byte(plaintext), []byte(sessionID))
	if err != nil {
		return "", fmt.Errorf("seal broker token: %w", err)
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (s *Server) openTokenField(sessionID, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("decode broker token: %w", err)
	}
	plaintext, err := s.sealer.Open(raw, []byte(sessionID))
	if err != nil {
		return "", fmt.Errorf("open broker token: %w", err)
	}
	return string(plaintext), nil
}

// brokerTokenSource loads and decrypts sessionID's stored Keycloak token, and
// wraps it in an oauth2.TokenSource that refreshes it against Keycloak's own
// token endpoint -- discovered the same way oauthConfig is for login, see
// oidc.go -- and persists any refreshed token back, re-encrypted, so the next
// call does not refresh again for nothing.
func (s *Server) brokerTokenSource(ctx context.Context, sessionID string) (oauth2.TokenSource, error) {
	stored, err := s.store.GetBrokerToken(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("get broker token: %w", err)
	}
	if stored.RefreshToken == "" {
		// Should not happen -- storeBrokerToken refuses to write a row without
		// one -- but a row is not proof of what wrote it, so this is checked
		// rather than assumed.
		return nil, errors.New("stored broker token carries no refresh token")
	}

	access, err := s.openTokenField(sessionID, stored.AccessToken)
	if err != nil {
		return nil, err
	}
	refresh, err := s.openTokenField(sessionID, stored.RefreshToken)
	if err != nil {
		return nil, err
	}

	provider, err := s.discover(ctx)
	if err != nil {
		return nil, err
	}

	cfg := oauth2.Config{
		ClientID:     s.cfg.Auth.OIDCClientID,
		ClientSecret: s.cfg.Auth.OIDCClientSecret,
		Endpoint:     provider.Endpoint(),
	}
	current := &oauth2.Token{AccessToken: access, RefreshToken: refresh, Expiry: stored.ExpiresAt}

	return &persistingTokenSource{
		ctx:       ctx,
		server:    s,
		sessionID: sessionID,
		base:      cfg.TokenSource(ctx, current),
		last:      current,
	}, nil
}

// persistingTokenSource wraps oauth2's own refreshing token source so that a
// refresh it performs is written back to the store, re-encrypted, rather than
// repeated on every subsequent call for the rest of the token's life.
type persistingTokenSource struct {
	ctx       context.Context
	server    *Server
	sessionID string
	base      oauth2.TokenSource
	last      *oauth2.Token
}

func (p *persistingTokenSource) Token() (*oauth2.Token, error) {
	tok, err := p.base.Token()
	if err != nil {
		if hardRefreshFailure(err) {
			// The refresh token itself is dead, not merely unreachable: retrying
			// it again next time would just fail the same way, so the row is
			// removed and the next attempt fails fast with a clear "log in
			// again" rather than a repeated, misleading timeout.
			if delErr := p.server.store.DeleteBrokerToken(p.ctx, p.sessionID); delErr != nil {
				logError("delete dead broker token", delErr)
			}
		}
		return nil, fmt.Errorf("refresh broker token: %w", err)
	}

	if tok.AccessToken != p.last.AccessToken || !tok.Expiry.Equal(p.last.Expiry) {
		p.last = tok
		if err := p.server.persistRefreshedToken(p.ctx, p.sessionID, tok); err != nil {
			logError("persist refreshed broker token", err)
		}
	}
	return tok, nil
}

func (s *Server) persistRefreshedToken(ctx context.Context, sessionID string, tok *oauth2.Token) error {
	access, err := s.sealTokenField(sessionID, tok.AccessToken)
	if err != nil {
		return err
	}
	// oauth2's own token source carries the previous refresh token over when
	// Keycloak's response omits one, so tok.RefreshToken is never empty here
	// merely because this exchange did not rotate it.
	refresh, err := s.sealTokenField(sessionID, tok.RefreshToken)
	if err != nil {
		return err
	}

	return s.store.UpdateBrokerToken(ctx, models.BrokerToken{
		SessionID: sessionID, AccessToken: access, RefreshToken: refresh,
		ExpiresAt: tok.Expiry, UpdatedAt: time.Now().UTC(),
	})
}

// hardRefreshFailure reports whether Keycloak firmly rejected the refresh
// token -- expired, revoked, or the session it belonged to logged out
// upstream -- as opposed to a transient failure to reach Keycloak at all. Only
// the former means retrying later cannot help, so only the former deletes the
// stored row.
func hardRefreshFailure(err error) bool {
	var retrieveErr *oauth2.RetrieveError
	if !errors.As(err, &retrieveErr) || retrieveErr.Response == nil {
		return false
	}
	switch retrieveErr.Response.StatusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

// entraTokenFor is the one place handleMyTeams and handleMyChannels both go
// through: it refreshes the session's Keycloak token if it has gone stale,
// then exchanges it at Keycloak's broker endpoint for the Entra token
// Keycloak stored when it federated this login.
func (s *Server) entraTokenFor(ctx context.Context, session models.Session) (string, error) {
	tokenSource, err := s.brokerTokenSource(ctx, session.ID)
	if err != nil {
		return "", err
	}
	tok, err := tokenSource.Token()
	if err != nil {
		return "", err
	}

	if _, err := s.discover(ctx); err != nil {
		return "", err
	}
	issuer := s.discoveredIssuer()
	if issuer == "" {
		return "", errors.New("discovery document carries no issuer")
	}

	return s.broker.EntraToken(ctx, issuer, s.cfg.Auth.Broker.IdPAlias, tok.AccessToken)
}

// brokerSession reports whether this request may use delegated Teams/Channels
// at all: the feature must be configured on, and the session must be the kind
// that has a Keycloak login behind it -- a local login was never federated
// through Keycloak and has no broker token to ask for.
func (s *Server) brokerSession(r *http.Request) (models.Session, bool) {
	if !s.cfg.Auth.Broker.Enabled {
		return models.Session{}, false
	}
	session, ok := s.currentSession(r)
	if !ok || session.Source != "oidc" {
		return models.Session{}, false
	}
	return session, true
}
