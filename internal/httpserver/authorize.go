package httpserver

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

type contextKey string

const (
	roleKey     contextKey = "role"
	subjectKey  contextKey = "subject"
	nameKey     contextKey = "name"
	idpGroupKey contextKey = "idp-groups"
)

// isPageRequest says whether a browser is asking for something to look at, as
// opposed to a script asking for JSON or a form being posted.
func isPageRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/")
}

// withPrincipal carries who is asking into the handlers, so authorization reads
// it from one place rather than each handler re-deriving it. via is how they
// proved it, which the audit trail records.
func withPrincipal(ctx context.Context, subject, name, via string, roles []authz.Role, idpGroups []string) context.Context {
	ctx = context.WithValue(ctx, subjectKey, subject)
	ctx = context.WithValue(ctx, nameKey, name)
	ctx = context.WithValue(ctx, idpGroupKey, idpGroups)
	ctx = audit.WithActor(ctx, models.Actor{Subject: subject, Name: name, Via: via})
	return context.WithValue(ctx, roleKey, roles)
}

// viewerOf is who the page is being rendered for. Name comes from the provider
// and may be empty; the roles are what the header shows beside it.
func viewerOf(r *http.Request) views.Viewer {
	name, _ := r.Context().Value(nameKey).(string)
	_, roles := principalOf(r)

	named := make([]string, 0, len(roles))
	for _, role := range roles {
		named = append(named, string(role))
	}
	return views.Viewer{Name: name, Roles: named}
}

// viewerFor is viewerOf plus what the nav needs, which only a handler holding
// the authorizer can answer.
func (s *Server) viewerFor(r *http.Request) views.Viewer {
	viewer := viewerOf(r)
	viewer.CanManage = s.allow(r, authz.ActionAdminister, authz.Resource{Type: "Grant"})
	// Without the database trail there is nothing to list.
	viewer.CanAudit = s.cfg.Audit.Database && s.allow(r, authz.ActionAdminister, authz.Resource{Type: "Audit"})
	viewer.CanTokens = s.manageTokens(r) || len(s.usableWebhooks(r)) > 0
	viewer.CanComplete = s.cfg.Samples.Enabled && s.mayComplete(r)
	viewer.NotificationsEnabled = botConfigured(s.cfg.Bot)
	viewer.PeopleEnabled = viewer.NotificationsEnabled && s.cfg.Bot.GlobalInstall
	return viewer
}

// principalSubject is the principal's identifier alone, for the callers that
// do not need the roles beside it.
func principalSubject(r *http.Request) string {
	subject, _ := r.Context().Value(subjectKey).(string)
	return subject
}

// principalFor is who the request acts for, as authorization sees them.
func principalFor(r *http.Request) authz.Principal {
	subject, roles := principalOf(r)
	groups, _ := r.Context().Value(idpGroupKey).([]string)
	return authz.Principal{Subject: subject, Roles: roles, IdPGroups: groups}
}

// allow and allowScoped ask the request's snapshot about the request's principal.
func (s *Server) allow(r *http.Request, action string, resource authz.Resource) bool {
	return s.policies(r).AllowFor(principalFor(r), action, resource)
}

func (s *Server) allowScoped(r *http.Request, action string, resource authz.Resource, scope authz.Scope) bool {
	return s.policies(r).AllowScopedFor(principalFor(r), action, resource, scope)
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

type policiesKey struct{}

// authzSource is the store as the authorization engine reads it (ADR 0073).
type authzSource struct{ store store.Store }

func (a authzSource) Generation(ctx context.Context) (int64, error) {
	return a.store.AuthzGeneration(ctx)
}

func (a authzSource) Load(ctx context.Context) (authz.Model, error) {
	members, err := a.store.ListAllGroupMembers(ctx)
	if err != nil {
		return authz.Model{}, err
	}
	permissions, err := a.store.ListPermissions(ctx)
	if err != nil {
		return authz.Model{}, err
	}
	model := authz.Model{
		Members: make([]authz.Membership, 0, len(members)),
		Grants:  make([]authz.Grant, 0, len(permissions)),
	}
	for _, m := range members {
		model.Members = append(model.Members, authz.Membership{Group: m.GroupID, Kind: string(m.Type), ID: m.ID})
	}
	for _, p := range permissions {
		model.Grants = append(model.Grants, grantOf(p))
	}
	tokens, err := a.store.ListAccessTokens(ctx)
	if err != nil {
		return authz.Model{}, err
	}
	for _, t := range tokens {
		if t.Scoped() {
			model.Tokens = append(model.Tokens, authz.TokenScope{ID: t.ID, Webhooks: t.Scope, Messages: t.Messages})
		}
	}
	return model, nil
}

func grantOf(p models.Permission) authz.Grant {
	return authz.Grant{
		ID: p.ID, PrincipalType: string(p.PrincipalType), PrincipalID: p.PrincipalID,
		ResourceType: p.ResourceType, ResourceID: p.ResourceID, Actions: p.Actions,
	}
}

// policies is the snapshot authorize resolved for this request, so every check
// in one request answers from the same generation. Outside authorize it is the
// last one built.
func (s *Server) policies(r *http.Request) *authz.Authorizer {
	if snapshot, ok := r.Context().Value(policiesKey{}).(*authz.Authorizer); ok {
		return snapshot
	}
	return s.engine.Base()
}

// authorize is the one place a permission is enforced. The UI hides what a role
// may not do, but hiding is not enforcing: a viewer who posts the form anyway
// gets a 403 that says what was refused.
func (s *Server) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fails closed: without the current generation nothing is decided.
		snapshot, err := s.engine.Authorizer(r.Context())
		if err != nil {
			logError(r.Context(), "authorization snapshot", err)
			http.Error(w, "authorization is unavailable, try again", http.StatusServiceUnavailable)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), policiesKey{}, snapshot))

		_, roles := principalOf(r)
		action, resource := requestAuthorization(r)

		// Samples complete both editors, so editing routes admits as well.
		if s.allow(r, action, resource) || (r.URL.Path == "/api/samples" && s.mayComplete(r)) {
			next.ServeHTTP(w, r)
			return
		}

		// Someone a record was shared with gets as far as its handler, which
		// decides about that record (ADR 0075).
		if recordPath(r) && snapshot.HasGrants(principalFor(r)) {
			s.serveDeferred(next, w, r)
			return
		}

		// A user the provider named no Teamster role for is not looking at a
		// permissions problem they can read out of a 403 body, so they get a
		// page that says what to ask for. Roles a deployment defined itself do
		// not count here: if its own policies refuse, the refusal is the answer.
		if !authz.HasBuiltin(roles) && isPageRequest(r) && !snapshot.HasGrants(principalFor(r)) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			if err := views.NoAccess(viewerOf(r)).Render(r.Context(), w); err != nil {
				logError(r.Context(), "render no-access page", err)
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

// recordPaths are the endpoints whose handlers check the record they touch, so
// the middleware may leave the decision to them. Derived views -- the routing
// graph, previews, samples, exports -- are not among them.
var recordPaths = []string{
	"/admin",
	"/admin/templates", "/admin/templates/delete",
	"/admin/destinations", "/admin/destinations/delete",
	"/admin/routes", "/admin/routes/delete",
	"/admin/webhooks", "/admin/webhooks/rotate", "/admin/webhooks/delete",
	"/admin/groups", "/admin/groups/save", "/admin/groups/delete", "/admin/groups/members/add", "/admin/groups/members/remove",
	"/admin/sharing/grant", "/admin/sharing/revoke",
	"/admin/tokens", "/admin/tokens/new", "/admin/tokens/delete", "/api/tokens",
	"/api/templates", "/api/destinations", "/api/routes", "/api/webhooks", "/api/groups", "/api/sharing",
}

var recordPrefixes = []string{"/api/templates/", "/api/destinations/", "/api/routes/", "/api/webhooks/", "/api/groups/", "/api/sharing/", "/api/tokens/"}

func recordPath(r *http.Request) bool {
	path := r.URL.Path
	switch {
	case path == "/api/templates/preview", path == "/api/templates/source-defaults", path == "/api/routes/global-default",
		isDefaultDestinationPath(path):
		return false
	case slices.Contains(recordPaths, path):
		return true
	}
	for _, prefix := range recordPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// serveDeferred runs a handler that must check its record itself, and reports
// one that answered without asking: that is a hole, not a refusal.
func (s *Server) serveDeferred(next http.Handler, w http.ResponseWriter, r *http.Request) {
	checked := &atomic.Bool{}
	rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
	next.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), checkedKey{}, checked)))
	if rec.status < http.StatusBadRequest && !checked.Load() {
		s.unchecked.Add(1)
		logError(r.Context(), "record check", errors.New("a deferred request was answered without a record check: "+r.Method+" "+r.URL.Path))
	}
}

// isDefaultDestinationPath is the form and the API endpoint that switch the
// global default destination.
func isDefaultDestinationPath(path string) bool {
	return path == "/admin/destinations/default" ||
		(strings.HasPrefix(path, "/api/destinations/") && strings.HasSuffix(path, "/default"))
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
	case strings.HasPrefix(path, "/api/grants"),
		strings.HasPrefix(path, "/admin/grants"),
		strings.HasPrefix(path, "/admin/permissions"):
		return authz.ActionAdminister, authz.Resource{Type: "Grant"}
	// An export is the whole configuration in one file and an import rewrites
	// it, including who may deliver where. Both are the admin's.
	case strings.HasPrefix(path, "/api/config/"):
		return authz.ActionAdminister, transferResource()
	// Tokens are self-service (ADR 0077): the handlers decide whose a caller
	// sees and which webhooks a new one may name.
	case path == "/api/tokens", strings.HasPrefix(path, "/api/tokens/"),
		path == "/admin/tokens", strings.HasPrefix(path, "/admin/tokens/"):
		resource.Type = "AccessToken"
	// Who has signed in, and switching them off, is the admin's (ADR 0072).
	case path == "/admin/users", strings.HasPrefix(path, "/admin/users/"),
		path == "/api/users", strings.HasPrefix(path, "/api/users/"):
		return authz.ActionAdminister, authz.Resource{Type: "User"}
	// Who may send to the webhooks is the webhook admins' (ADR 0076); the
	// overview of everyone's access is the admins'.
	case path == "/admin/access/webhooks", path == "/api/access/webhooks":
		return authz.ActionAdminister, authz.WebhookResource(authz.WebhooksAll)
	case path == "/admin/access", strings.HasPrefix(path, "/admin/access/"), strings.HasPrefix(path, "/api/access/"):
		return authz.ActionAdminister, authz.Resource{Type: "Access"}
	// The trail names everyone who changed anything, and what it held before.
	case path == "/admin/audit", path == "/api/audit":
		return authz.ActionAdminister, authz.Resource{Type: "Audit"}
	// Installing the app for the whole tenant, and the directory behind it,
	// are the admin's, reads included: the page lists people (ADR 0059).
	case path == "/admin/people", strings.HasPrefix(path, "/admin/people/"),
		strings.HasPrefix(path, "/api/people/"):
		return authz.ActionAdminister, authz.Resource{Type: "DirectoryInstall"}
	// The global default decides where every unclaimed message lands, which is
	// broader than any one destination an editor may change (ADR 0038).
	case isDefaultDestinationPath(path) && r.Method != http.MethodGet && r.Method != http.MethodHead:
		return authz.ActionAdminister, authz.Resource{Type: "Destination"}
	// A read, but one only an editor needs: label values say what runs where,
	// and a viewer has nothing to complete. authorize also admits a route editor.
	case path == "/api/samples":
		return authz.ActionEdit, authz.Resource{Type: "Template"}
	// Minting a link code binds the caller's own subject, not anyone else's
	// configuration, so it is the on-call viewer's action rather than an edit
	// the default case below would otherwise refuse them.
	case path == "/api/recipients/link":
		return authz.ActionLink, authz.Resource{Type: "Recipient"}
	// The self-service page shares the mapping for its two POSTs -- minting
	// and unlinking (and cancelling a mint) bind or touch only the caller's
	// own subject too, so neither is a bigger ask than the API endpoint
	// above. Exact-match or a slash-anchored prefix, never a bare
	// HasPrefix("/admin/notifications"): that would also loosen a future
	// "/admin/notification-rules" or similar path to this mapping by
	// accident, the same way an unanchored prefix would confuse
	// "/admin/notifications" with itself plus anything typed after it.
	// Reading the page is a plain read, not a bigger ask than any other page
	// a viewer may look at, so a GET is deliberately left to fall through to
	// the method-based mapping below rather than being forced to ActionLink
	// here -- a deployment that defines its own read-only role and permits
	// only ActionView would otherwise see the nav link and get a 403.
	case path == "/admin/notifications" || strings.HasPrefix(path, "/admin/notifications/"):
		resource.Type = "Recipient"
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			return authz.ActionLink, resource
		}
	// Everything else under /recipients -- the admin page, listing and
	// unlinking -- is an ordinary view/edit on a Recipient, which the existing
	// admin/editor/viewer policies already cover: no new Cedar action.
	case strings.HasPrefix(path, "/api/recipients"), strings.HasPrefix(path, "/admin/recipients"):
		resource.Type = "Recipient"
	case path == "/api/sharing", strings.HasPrefix(path, "/api/sharing/"), strings.HasPrefix(path, "/admin/sharing/"):
		resource.Type = "Permission"
	case path == "/api/groups", strings.HasPrefix(path, "/api/groups/"),
		path == "/admin/groups", strings.HasPrefix(path, "/admin/groups/"):
		resource.Type = "Group"
	case strings.HasPrefix(path, "/api/templates"), strings.HasPrefix(path, "/admin/templates"):
		resource.Type = "Template"
	case strings.HasPrefix(path, "/api/destinations"), strings.HasPrefix(path, "/admin/destinations"):
		resource.Type = "Destination"
	case strings.HasPrefix(path, "/api/routes"), strings.HasPrefix(path, "/admin/routes"):
		resource.Type = "Route"
	case strings.HasPrefix(path, "/api/webhooks"), strings.HasPrefix(path, "/admin/webhooks"):
		resource.Type = "WebhookEndpoint"
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
