package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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
)

// webhookCredential takes the token from Authorization: Bearer, or from the
// X-Teamster-Token header the webhooks accepted before ADR 0044.
func webhookCredential(r *http.Request) string {
	if scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " "); ok && strings.EqualFold(scheme, "Bearer") {
		return strings.TrimSpace(token)
	}
	return r.Header.Get("X-Teamster-Token")
}

// webhookAuth returns the name of the token the request presented. A store
// failure is returned as is, so it is not mistaken for a wrong token.
func (s *Server) webhookAuth(r *http.Request) (string, error) {
	provided := webhookCredential(r)
	// safeEquals("", "") holds, so an unset webhook.token must not be compared.
	if provided == "" {
		return "", errNoCredential
	}
	if s.cfg.Webhook.Token != "" && safeEquals(provided, s.cfg.Webhook.Token) {
		return configTokenName, nil
	}

	ctx := r.Context()
	token, err := s.store.GetAccessTokenByHash(ctx, hashToken(provided))
	if errors.Is(err, store.ErrNotFound) {
		return "", errUnknownToken
	}
	if err != nil {
		return "", err
	}

	if now := time.Now(); now.Sub(token.LastUsedAt) > touchInterval {
		// Best effort: a failed bookkeeping write must not drop the alert.
		if err := s.store.TouchAccessToken(ctx, token.ID, now); err != nil {
			log.Printf("record use of access token %q: %v", token.Name, err)
		}
	}
	return token.Name, nil
}

// authorizeWebhook answers a refused request itself and reports whether the
// handler may go on.
func (s *Server) authorizeWebhook(w http.ResponseWriter, r *http.Request, source string) bool {
	_, err := s.webhookAuth(r)
	switch {
	case err == nil:
		return true
	case errors.Is(err, errNoCredential), errors.Is(err, errUnknownToken):
		// Counted because a refused token is otherwise a 401 nobody is
		// watching, and "the sender's secret is wrong" looks exactly like "the
		// sender stopped sending".
		s.metrics.WebhookReceived(r.Context(), source, "refused")
		log.Printf("webhook %s refused: %v", source, err)
		w.Header().Set("WWW-Authenticate", `Bearer realm="webhook"`)
		w.WriteHeader(http.StatusUnauthorized)
	default:
		log.Printf("webhook %s: look up access token: %v", source, err)
		writeJSONError(w, http.StatusServiceUnavailable, "cannot check the token right now")
	}
	return false
}

// newAccessToken is 256 bits behind a recognisable prefix.
func newAccessToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return accessTokenPrefix + hex.EncodeToString(raw), nil
}
