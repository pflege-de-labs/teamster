package httpserver

import (
	"net/http"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
)

// principalSearchLimit is how many users one search adds to a picker.
const principalSearchLimit = 20

// principalHit is one user a picker may add, shaped like the options it renders.
type principalHit struct {
	Value  string `json:"value"`
	Label  string `json:"label"`
	Search string `json:"search"`
	Title  string `json:"title"`
}

// handlePrincipalSearch finds users for a picker whose page listed only the
// first usersPageSize of them (ADR 0111).
func (s *Server) handlePrincipalSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	hits := []principalHit{}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, hits)
		return
	}
	users, err := s.store.ListUsers(r.Context(), q, principalSearchLimit)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	for _, u := range users {
		hits = append(hits, principalHit{
			Value: "user:" + u.Subject, Label: displayName(u),
			Search: strings.Join(strings.Fields(displayName(u)+" "+u.Email+" "+u.Subject), " "), Title: u.Subject,
		})
	}
	writeJSON(w, http.StatusOK, hits)
}

// mayPick is whoever a picker is shown to: editors and admins, and anyone a
// record was shared with, who may be allowed to share it on.
func (s *Server) mayPick(r *http.Request) bool {
	_, roles := principalOf(r)
	if slices.ContainsFunc(roles, func(role authz.Role) bool { return role == authz.RoleAdmin || role == authz.RoleEditor }) {
		return true
	}
	return s.policies(r).HasGrants(principalFor(r))
}
