package httpserver

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/config"
)

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
		next.ServeHTTP(w, r)
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
