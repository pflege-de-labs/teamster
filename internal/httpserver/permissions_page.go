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

// knownRoles are the roles a scope can be set for: the ones this build defines
// plus any a grant already names, so a deployment that invented
// "payments-editors" finds it in the list rather than having to remember how it
// spelled it.
//
// Admin is not among them. The admin policy permits every action on every
// resource, so a grant naming that role would be stored, shown, and then
// ignored by the authorizer — a control that does nothing is worse than no
// control.
func (s *Server) knownRoles() []string {
	roles := map[string]bool{
		string(authz.RoleEditor): true,
		string(authz.RoleViewer): true,
	}

	if grants, err := s.store.ListGrants(); err == nil {
		for _, grant := range grants {
			roles[grant.Role] = true
		}
	}
	delete(roles, string(authz.RoleAdmin))

	out := make([]string, 0, len(roles))
	for role := range roles {
		out = append(out, role)
	}
	sort.Strings(out)
	return out
}
