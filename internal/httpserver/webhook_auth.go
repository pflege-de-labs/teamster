package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// configTokenName is what the log calls the token from webhook.token, which has
// no row and so no name of its own.
const configTokenName = "webhook.token"

// accessTokenPrefix marks an issued token, so a secret scanner can recognise
// one that leaked into a repository or a log.
const accessTokenPrefix = "tst_"

// touchInterval bounds how stale last_used_at may be. Writing it on every
// request would turn each alert into a database write.
const touchInterval = time.Hour

var (
	errNoCredential = errors.New("no credential: send Authorization: Bearer <token>")
	errUnknownToken = errors.New("token matches neither webhook.token nor an issued access token")
	// errTokenScope is a scoped token used beyond its scope or its creator's permissions.
	errTokenScope = errors.New("the token's scope, or its creator, does not allow this webhook")
)

// webhookCredential takes the token from Authorization: Bearer, or from the
// X-Teamster-Token header the webhooks accepted before ADR 0044.
func webhookCredential(r *http.Request) string {
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return r.Header.Get("X-Teamster-Token")
}

// webhookAuth returns the token the request presented. A store failure is
// returned as is, so it is not mistaken for a wrong token.
func (s *Server) webhookAuth(r *http.Request) (models.AccessToken, error) {
	provided := webhookCredential(r)
	// safeEquals("", "") holds, so an unset webhook.token must not be compared.
	if provided == "" {
		return models.AccessToken{}, errNoCredential
	}
	if s.cfg.Webhook.Token != "" && safeEquals(provided, s.cfg.Webhook.Token) {
		return models.AccessToken{Name: configTokenName}, nil
	}

	ctx := r.Context()
	token, err := s.store.GetAccessTokenByHash(ctx, hashToken(provided))
	if errors.Is(err, store.ErrNotFound) {
		return models.AccessToken{}, errUnknownToken
	}
	if err != nil {
		return models.AccessToken{}, err
	}

	if now := time.Now(); now.Sub(token.LastUsedAt) > touchInterval {
		// Best effort: a failed bookkeeping write must not drop the alert.
		if err := s.store.TouchAccessToken(ctx, token.ID, now); err != nil {
			logging.FromContext(ctx).Error("record use of access token", "token", token.Name, "err", err)
		}
	}
	return token, nil
}

// tokenCreator is the principal a scoped token answers to, as they are now.
// A creator who is gone or disabled takes their tokens with them; the local
// login has no registry row until it signs in, and stays admin.
func (s *Server) tokenCreator(r *http.Request, subject string) (authz.Principal, error) {
	user, err := s.store.GetUser(r.Context(), subject)
	switch {
	case err == nil && user.Disabled():
		return authz.Principal{}, errTokenScope
	case err == nil && user.Source == sourceLocal:
		return authz.Principal{Subject: subject, Roles: []authz.Role{authz.RoleAdmin}}, nil
	case err == nil:
		return authz.Principal{Subject: subject, Roles: authz.Decode(strings.Join(user.Roles, " ")), IdPGroups: user.IdPGroups}, nil
	case !errors.Is(err, store.ErrNotFound):
		return authz.Principal{}, err
	case localLoginConfigured(s.cfg) && subject == s.cfg.Admin.Username:
		return authz.Principal{Subject: subject, Roles: []authz.Role{authz.RoleAdmin}}, nil
	}
	return authz.Principal{}, errTokenScope
}

// A sender is who a webhook request speaks for: the token, and for a scoped
// one its creator as they are now. The deployment token and tokens from
// before scopes have no creator, so they may not name people (ADR 0082).
type sender struct {
	token   models.AccessToken
	creator *authz.Principal
}

// authorizeScoped asks Cedar about a scoped token: its own scope and its creator's use.
func (s *Server) authorizeScoped(r *http.Request, token models.AccessToken, source string) (authz.Principal, error) {
	snapshot, err := s.engine.Authorizer(r.Context())
	if err != nil {
		return authz.Principal{}, err
	}
	creator, err := s.tokenCreator(r, token.CreatedBy)
	if err != nil {
		return authz.Principal{}, err
	}
	if !snapshot.AllowToken(token.ID, creator, source) {
		return authz.Principal{}, errTokenScope
	}
	return creator, nil
}

// authorizeWebhook answers a refused request itself and reports whether the
// handler may go on, and for whom.
func (s *Server) authorizeWebhook(w http.ResponseWriter, r *http.Request, source string) (sender, bool) {
	token, err := s.webhookAuth(r)
	from := sender{token: token}
	if err == nil && token.Scoped() {
		var creator authz.Principal
		creator, err = s.authorizeScoped(r, token, source)
		from.creator = &creator
	}
	switch {
	case err == nil:
		return from, true
	case errors.Is(err, errNoCredential), errors.Is(err, errUnknownToken):
		// Counted because a refused token is otherwise a 401 nobody is
		// watching, and "the sender's secret is wrong" looks exactly like "the
		// sender stopped sending".
		s.metrics.WebhookReceived(r.Context(), source, "refused")
		logging.FromContext(r.Context()).Warn("webhook refused", "source", source, "reason", err)
		w.Header().Set("WWW-Authenticate", `Bearer realm="webhook"`)
		w.WriteHeader(http.StatusUnauthorized)
	case errors.Is(err, errTokenScope):
		s.metrics.WebhookReceived(r.Context(), source, "forbidden")
		logging.FromContext(r.Context()).Warn("webhook refused", "source", source, "token", token.Name, "creator", token.CreatedBy, "reason", err)
		writeJSONError(w, http.StatusForbidden, errTokenScope.Error())
	default:
		logging.FromContext(r.Context()).Error("webhook: look up access token", "source", source, "err", err)
		writeJSONError(w, http.StatusServiceUnavailable, "cannot check the token right now")
	}
	return sender{}, false
}

// newAccessToken is 256 bits behind a recognisable prefix.
func newAccessToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return accessTokenPrefix + hex.EncodeToString(raw), nil
}
