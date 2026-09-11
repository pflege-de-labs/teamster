package httpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

type contextKey string

const (
	roleKey    contextKey = "role"
	subjectKey contextKey = "subject"
	nameKey    contextKey = "name"
)

// isPageRequest says whether a browser is asking for something to look at, as
// opposed to a script asking for JSON or a form being posted.
func isPageRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/")
}

// withPrincipal carries who is asking into the handlers, so authorization reads
// it from one place rather than each handler re-deriving it.
func withPrincipal(ctx context.Context, subject, name string, roles []authz.Role) context.Context {
	ctx = context.WithValue(ctx, subjectKey, subject)
	ctx = context.WithValue(ctx, nameKey, name)
	return context.WithValue(ctx, roleKey, roles)
}

// principalSubject is the principal's identifier alone, for the callers that
// do not need the roles beside it.
func principalSubject(r *http.Request) string {
	subject, _ := r.Context().Value(subjectKey).(string)
	return subject
}

func principalOf(r *http.Request) (string, []authz.Role) {
	subject, _ := r.Context().Value(subjectKey).(string)
	roles, _ := r.Context().Value(roleKey).([]authz.Role)
	return subject, roles
}

// rolesOf reads the roles a session was created with. A session written before
// roles existed carries none at all, and keeps the access it had rather than
// being silently demoted mid-shift.
func rolesOf(session models.Session) []authz.Role {
	if session.Roles == "" {
		return []authz.Role{authz.RoleAdmin}
	}
	return authz.Decode(session.Roles)
}

// authorize is the one place a permission is enforced. The UI hides what a role
// may not do, but hiding is not enforcing: a viewer who posts the form anyway
// gets a 403 that says what was refused.
func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, roles := principalOf(r)
		action, resource := requestAuthorization(r)

		if s.authz.Allow(subject, roles, action, resource) {
			next.ServeHTTP(w, r)
			return
		}

		// A user the provider named no Teamster role for is not looking at a
		// permissions problem they can read out of a 403 body, so they get a
		// page that says what to ask for. Roles a deployment defined itself do
		// not count here: if its own policies refuse, the refusal is the answer.
		if !authz.HasBuiltin(roles) && isPageRequest(r) {
			name, _ := r.Context().Value(nameKey).(string)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			if err := views.NoAccess(name).Render(r.Context(), w); err != nil {
				logError("render no-access page", err)
			}
			return
		}

		refusal := "the " + authz.Encode(roles) + " role may not " + action + " a " + strings.ToLower(resource.Type)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeJSONError(w, http.StatusForbidden, refusal)
			return
		}
		http.Error(w, refusal, http.StatusForbidden)
	})
}

// requestAuthorization turns a request into the question to ask about it. Paths
// map to a resource type and methods to an action, with the two endpoints that
// answer a question by POST — preview and match — counted as reads, because
// neither changes anything.
func requestAuthorization(r *http.Request) (string, authz.Resource) {
	path := r.URL.Path

	resource := authz.Resource{Type: "Page"}
	switch {
	// Deciding who may deliver where is the admin's, not the editor's, so it
	// is a different action rather than another thing an editor may edit.
	case strings.HasPrefix(path, "/api/grants"), strings.HasPrefix(path, "/admin/grants"):
		return authz.ActionAdminister, authz.Resource{Type: "Grant"}
	case strings.HasPrefix(path, "/api/templates"), strings.HasPrefix(path, "/admin/templates"):
		resource.Type = "Template"
	case strings.HasPrefix(path, "/api/destinations"), strings.HasPrefix(path, "/admin/destinations"):
		resource.Type = "Destination"
	case strings.HasPrefix(path, "/api/routes"), strings.HasPrefix(path, "/admin/routes"):
		resource.Type = "Route"
	case strings.HasPrefix(path, "/api/graph/"):
		resource.Type = "Directory"
	case strings.HasPrefix(path, "/api/routing/"):
		resource.Type = "Routing"
	}

	switch {
	case r.Method == http.MethodGet, r.Method == http.MethodHead:
		return authz.ActionView, resource
	// Rendering a template against a sample alert, and asking which route an
	// alert would take, are reads that need a body to ask.
	case path == "/api/templates/preview", path == "/api/routing/match":
		return authz.ActionView, resource
	default:
		return authz.ActionEdit, resource
	}
}
