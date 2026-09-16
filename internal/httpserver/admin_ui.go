package httpserver

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// handleAdminPage renders the admin UI. Notices arrive as query parameters
// because a form post answers with a redirect, which carries no body.
func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	_, roles := principalOf(r)
	page := views.Page{
		Notice:         r.URL.Query().Get("notice"),
		Error:          r.URL.Query().Get("error"),
		PreviewSamples: previewSamples(),
		Snippets:       cards.Snippets(),
		Starter:        cards.Starter,
		Viewer:         s.viewerFor(r),
		CanEdit:        s.authz.Allow(principalSubject(r), roles, authz.ActionEdit, authz.Resource{Type: "Template"}),
		CanManage:      s.authz.Allow(principalSubject(r), roles, authz.ActionAdminister, authz.Resource{Type: "Grant"}),
	}

	var err error
	if page.Templates, err = s.store.ListTemplates(ctx); err != nil {
		page.Error = err.Error()
	}
	if page.Destinations, err = s.store.ListDestinations(ctx); err != nil {
		page.Error = err.Error()
	}
	// A destination in a channel this session may not see is not theirs to read.
	if page.Destinations, err = s.visibleDestinations(r, page.Destinations); err != nil {
		page.Error = err.Error()
	}
	if page.Routes, err = s.store.ListRoutes(ctx); err != nil {
		page.Error = err.Error()
	}

	if selected := r.URL.Query().Get("edit"); selected != "" {
		s.loadForEditing(ctx, &page, selected, r.URL.Query().Get("id"))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Admin(page).Render(ctx, w); err != nil {
		log.Printf("render admin page: %v", err)
	}
}

// loadForEditing fills in the record a form should start from. A record that
// has gone missing is reported on the page, which stays useful, rather than
// turning the whole request into a 404.
func (s *Server) loadForEditing(ctx context.Context, page *views.Page, section, id string) {
	if id == "" {
		return
	}

	var err error
	switch section {
	case "templates":
		var template models.Template
		if template, err = s.store.GetTemplate(ctx, id); err == nil {
			page.EditTemplate = &template
		}
	case "destinations":
		var destination models.Destination
		if destination, err = s.store.GetDestination(ctx, id); err == nil {
			page.EditDestination = &destination
		}
	case "routes":
		var route models.Route
		if route, err = s.store.GetRoute(ctx, id); err == nil {
			page.EditRoute = &route
		}
	default:
		return
	}

	if err != nil {
		page.Error = err.Error()
	}
}

// formPost guards the state-changing form endpoints that redirect back to
// /admin. Basic auth credentials ride along on any cross-site form post, so
// the request has to prove it came from this origin; there is no session to
// hang a CSRF token on.
func (s *Server) formPost(handler func(*http.Request) (string, error)) http.HandlerFunc {
	return s.formPostTo("/admin", handler)
}

// formPostTo is formPost for the endpoints that land somewhere other than
// /admin -- the notifications page's own unlink control, most immediately.
func (s *Server) formPostTo(target string, handler func(*http.Request) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !sameOrigin(r) {
			http.Error(w, "cross-origin form post rejected", http.StatusForbidden)
			return
		}
		if err := r.ParseForm(); err != nil {
			redirectTo(target, w, r, "", "invalid form submission")
			return
		}

		notice, err := handler(r)
		if err != nil {
			redirectTo(target, w, r, "", err.Error())
			return
		}
		redirectTo(target, w, r, notice, "")
	}
}

// sameOrigin accepts a request that the browser reports as same-origin, or
// whose Origin matches the host it was sent to. A request with neither header
// is not a browser form post and is left to the auth layer.
func sameOrigin(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "cross-site", "same-site":
		return false
	}

	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return parsed.Host == r.Host
}

func redirectTo(target string, w http.ResponseWriter, r *http.Request, notice, message string) {
	dest := &url.URL{Path: target}
	query := dest.Query()
	if notice != "" {
		query.Set("notice", notice)
	}
	if message != "" {
		query.Set("error", message)
	}
	dest.RawQuery = query.Encode()

	http.Redirect(w, r, dest.String(), http.StatusSeeOther)
}

func (s *Server) saveTemplate(r *http.Request) (string, error) {
	ctx := r.Context()
	template := models.Template{
		ID:    r.PostFormValue("id"),
		Name:  r.PostFormValue("name"),
		Title: r.PostFormValue("title"),
		Text:  r.PostFormValue("message_text"),
		Body:  r.PostFormValue("body"),
	}
	if err := templates.Validate(template); err != nil {
		return "", err
	}
	if template.ID == "" {
		if _, err := s.store.CreateTemplate(ctx, template); err != nil {
			return "", err
		}
		return "Template created.", nil
	}
	if _, err := s.store.UpdateTemplate(ctx, template); err != nil {
		return "", err
	}
	return "Template updated.", nil
}

func (s *Server) saveDestination(r *http.Request) (string, error) {
	ctx := r.Context()
	destination := models.Destination{
		ID:        r.PostFormValue("id"),
		Name:      r.PostFormValue("name"),
		TeamID:    r.PostFormValue("team_id"),
		ChannelID: r.PostFormValue("channel_id"),
	}
	// Typing a channel id the picker would not have offered reaches here, which
	// is why the check is on the write rather than on the list.
	allowed, err := s.mayDeliverTo(r, destination.TeamID, destination.ChannelID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errDeliveryRefused
	}

	if destination.ID == "" {
		if _, err := s.store.CreateDestination(ctx, destination); err != nil {
			return "", err
		}
		return "Destination created.", nil
	}
	if _, err := s.store.UpdateDestination(ctx, destination); err != nil {
		return "", err
	}
	return "Destination updated.", nil
}

func (s *Server) saveRoute(r *http.Request) (string, error) {
	ctx := r.Context()
	selector, err := parseSelector(r.PostFormValue("label_selector"))
	if err != nil {
		return "", err
	}

	priority := 0
	if raw := strings.TrimSpace(r.PostFormValue("priority")); raw != "" {
		if priority, err = strconv.Atoi(raw); err != nil {
			return "", err
		}
	}

	route := models.Route{
		ID:            r.PostFormValue("id"),
		Name:          r.PostFormValue("name"),
		ParentID:      r.PostFormValue("parent_id"),
		LabelSelector: selector,
		DestinationID: r.PostFormValue("destination_id"),
		TemplateID:    r.PostFormValue("template_id"),
		IsDefault:     r.PostFormValue("is_default") == "true",
		Greedy:        r.PostFormValue("greedy") == "true",
		Priority:      priority,
	}
	// A route is how an alert reaches a channel, so pointing one at a
	// destination outside the grants is the same escape as creating it there.
	allowed, err := s.mayDeliverToDestination(r, route.DestinationID)
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errDeliveryRefused
	}
	created := route.ID == ""
	if _, err := s.saveRouteChecked(ctx, route); err != nil {
		return "", err
	}
	if created {
		return "Route created.", nil
	}
	return "Route updated.", nil
}

func parseSelector(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}, nil
	}
	var selector map[string]string
	if err := json.Unmarshal([]byte(raw), &selector); err != nil {
		return nil, err
	}
	return selector, nil
}

func (s *Server) deleteTemplate(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.store.DeleteTemplate(ctx, r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Template deleted.", nil
}

func (s *Server) deleteDestination(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.store.DeleteDestination(ctx, r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Destination deleted.", nil
}

// Both write paths validate the same way, because the tree rules are routing's
// and neither the form nor the API may be the only place they hold.
// invalidRoute marks a rejection the caller can fix, so the handlers can still
// tell a bad request from a broken database now that both come back from the
// same call.
type invalidRoute struct{ err error }

func (e invalidRoute) Error() string { return e.err.Error() }
func (e invalidRoute) Unwrap() error { return e.err }

// saveRouteChecked validates the route against the tree and writes it in one
// transaction. Doing the two separately let two admins each validate against a
// tree the other was about to change, and the losing edit could orphan a child
// -- which the router treats as a root, so it starts matching alerts its parent
// used to filter out.
func (s *Server) saveRouteChecked(ctx context.Context, route models.Route) (models.Route, error) {
	var saved models.Route
	err := s.store.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		existing, err := tx.ListRoutes(ctx)
		if err != nil {
			return err
		}
		if err := routing.ValidateRoute(route, existing); err != nil {
			return invalidRoute{err}
		}
		if route.ID == "" {
			saved, err = tx.CreateRoute(ctx, route)
			return err
		}
		saved, err = tx.UpdateRoute(ctx, route)
		return err
	})
	return saved, err
}

// deleteRouteChecked is the same bargain for the other direction: a route with
// children may not be deleted, and the check has to see the tree the delete
// applies to.
func (s *Server) deleteRouteChecked(ctx context.Context, id string) error {
	return s.store.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		existing, err := tx.ListRoutes(ctx)
		if err != nil {
			return err
		}
		if err := routing.ValidateDelete(id, existing); err != nil {
			return invalidRoute{err}
		}
		return tx.DeleteRoute(ctx, id)
	})
}

func (s *Server) deleteRoute(r *http.Request) (string, error) {
	ctx := r.Context()

	if err := s.deleteRouteChecked(ctx, r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Route deleted.", nil
}
