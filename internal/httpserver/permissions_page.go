package httpserver

import (
	"net/http"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
)

// handlePermissionsPage draws the tree an admin ticks Teams and channels in.
// It is a page of its own rather than a panel on /admin: the tree is as long as
// the tenant is, and it is the only thing on it that an editor may not see at
// all.
func (s *Server) handlePermissionsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	page := views.Permissions{Viewer: s.viewerFor(r), Roles: s.knownRoles()}
	if err := views.PermissionsPage(page).Render(r.Context(), w); err != nil {
		logError("render permissions page", err)
	}
}

// knownRoles are the three this build defines plus any a grant already names,
// so a deployment that invented "payments-editors" finds it in the list rather
// than having to remember how it spelled it.
func (s *Server) knownRoles() []string {
	roles := map[string]bool{
		string(authz.RoleAdmin):  true,
		string(authz.RoleEditor): true,
		string(authz.RoleViewer): true,
	}

	if grants, err := s.store.ListGrants(); err == nil {
		for _, grant := range grants {
			roles[grant.Role] = true
		}
	}

	out := make([]string, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}
