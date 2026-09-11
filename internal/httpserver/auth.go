package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"

	"github.com/pflege-de-labs/teamster/internal/config"
)

// apiAuth accepts either of the two ways in: a session, which is how the admin
// UI's own fetches arrive, or the local credentials, which is how a script
// arrives. Before roles existed only the credentials were accepted, which meant
// a browser signed in through the provider could not load the pickers or the
// routing graph at all.
func (s *Server) apiAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := s.currentSession(r)
		if !ok {
			s.basicAuth(next).ServeHTTP(w, r)
			return
		}

		// A cookie travels with a cross-site request, so a state-changing call
		// authenticated by one has to prove its origin. Basic auth does not
		// need this: those credentials are sent deliberately.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && !sameOrigin(r) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), session.Subject, session.Name, roleOf(session))))
	})
}

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := parseBasicAuth(r.Header.Get("Authorization"))
		// Both comparisons always run, so a wrong user costs what a wrong
		// password costs and neither can be told apart by timing.
		userOK, passwordOK := safeEquals(user, s.cfg.Admin.Username), safeEquals(pass, s.cfg.Admin.Password)
		if !ok || !localLoginConfigured(s.cfg) || !userOK || !passwordOK {
			w.Header().Set("WWW-Authenticate", "Basic realm=\"admin\"")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// The API credentials are the local ones, which administer: scripts
		// predate roles and there is nowhere to put a role for them.
		next.ServeHTTP(w, r.WithContext(withPrincipal(r.Context(), user, user, authz.RoleAdmin)))
	})
}

func (s *Server) webhookAuth(r *http.Request) bool {
	provided := r.Header.Get("X-Teamster-Token")
	return safeEquals(provided, s.cfg.Webhook.Token)
}

func parseBasicAuth(header string) (string, string, bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", false
	}
	payload, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return "", "", false
	}
	parts := strings.SplitN(string(payload), ":", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// Comparing digests rather than the strings keeps the comparison constant time
// regardless of length, which a bare ConstantTimeCompare cannot do because it
// returns early when the lengths differ.
func safeEquals(a, b string) bool {
	sumA, sumB := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))
	return subtle.ConstantTimeCompare(sumA[:], sumB[:]) == 1
}

// A configured username with an empty password would otherwise admit that
// username to anyone who submits an empty password, so both are required
// before the local login counts as configured at all.
func localLoginConfigured(cfg config.Config) bool {
	return cfg.Admin.Username != "" && cfg.Admin.Password != ""
}
