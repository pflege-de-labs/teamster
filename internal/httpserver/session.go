package httpserver

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const sessionCookie = "teamster_session"

func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, subject, name, source string) error {
	id, err := newToken()
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	if err := s.store.CreateSession(models.Session{
		ID: id, Subject: subject, Name: name, Source: source,
		CreatedAt: now, ExpiresAt: now.Add(s.sessionTTL()),
	}); err != nil {
		return err
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    id,
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  now.Add(s.sessionTTL()),
	})
	return nil
}

func (s *Server) sessionTTL() time.Duration {
	if s.cfg.Auth.SessionTTL > 0 {
		return s.cfg.Auth.SessionTTL
	}
	return 12 * time.Hour
}

// isTLS decides whether the cookie may be marked Secure. A deployment behind a
// terminating proxy only knows from the forwarded header.
func isTLS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) currentSession(r *http.Request) (models.Session, bool) {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return models.Session{}, false
	}

	session, err := s.store.GetSession(cookie.Value)
	if err != nil {
		return models.Session{}, false
	}
	return session, true
}

func (s *Server) endSession(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil && cookie.Value != "" {
		if err := s.store.DeleteSession(cookie.Value); err != nil && !isNotFound(err) {
			logError("delete session", err)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isTLS(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func urlQueryEscape(value string) string {
	return url.QueryEscape(value)
}

func isNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), store.ErrNotFound.Error())
}

// requireSession protects the browser-facing admin pages. The JSON API keeps
// basic auth, because automation cannot complete an authorization code flow.
func (s *Server) requireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := s.currentSession(r); ok {
			next.ServeHTTP(w, r)
			return
		}
		http.Redirect(w, r, "/admin/login", http.StatusFound)
	})
}

// claimValues walks a dotted path such as realm_access.roles, because Keycloak
// nests role claims rather than exposing them at the top level.
func claimValues(claims map[string]any, path string) []string {
	var current any = claims
	for _, segment := range strings.Split(path, ".") {
		nested, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = nested[segment]
		if !ok {
			return nil
		}
	}

	switch value := current.(type) {
	case string:
		return []string{value}
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func allowedByClaim(claims map[string]any, path string, allowed []string) bool {
	values := claimValues(claims, path)
	for _, value := range values {
		for _, want := range allowed {
			if value == want {
				return true
			}
		}
	}
	return false
}
