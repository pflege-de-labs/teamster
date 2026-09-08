package httpserver

import (
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
)

func (s *Server) basicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := parseBasicAuth(r.Header.Get("Authorization"))
		if !ok || !safeEquals(user, s.cfg.Admin.Username) || !safeEquals(pass, s.cfg.Admin.Password) {
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

func safeEquals(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
