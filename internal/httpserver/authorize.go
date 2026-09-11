package httpserver

import (
	"context"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

type contextKey string

const (
	roleKey    contextKey = "role"
	subjectKey contextKey = "subject"
)

// withPrincipal carries who is asking into the handlers, so authorization reads
// it from one place rather than each handler re-deriving it.
func withPrincipal(ctx context.Context, subject string, role authz.Role) context.Context {
	return context.WithValue(context.WithValue(ctx, subjectKey, subject), roleKey, role)
}

// principalSubject is the principal's identifier alone, for the callers that
// do not need the role beside it.
func principalSubject(r *http.Request) string {
	subject, _ := r.Context().Value(subjectKey).(string)
	return subject
}

func principalOf(r *http.Request) (string, authz.Role) {
	subject, _ := r.Context().Value(subjectKey).(string)
	role, _ := r.Context().Value(roleKey).(authz.Role)
	return subject, role
}

// roleOf reads the role a session was created with. A session written before
// roles existed carries none, and keeps the access it had rather than being
// silently demoted mid-shift; a role this build does not know is refused
// everything, because it is either tampering or a downgrade.
func roleOf(session models.Session) authz.Role {
	if session.Role == "" {
		return authz.RoleAdmin
	}
	role := authz.Role(session.Role)
	if !authz.Valid(role) {
		return authz.Role("unknown")
	}
	return role
}

// authorize is the one place a permission is enforced. The UI hides what a role
// may not do, but hiding is not enforcing: a viewer who posts the form anyway
// gets a 403 that says what was refused.
func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		subject, role := principalOf(r)
		action, resource := requestAuthorization(r)

		if s.authz.Allow(subject, role, action, resource) {
			next.ServeHTTP(w, r)
			return
		}

		refusal := "the " + string(role) + " role may not " + action + " a " + strings.ToLower(resource.Type)
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
