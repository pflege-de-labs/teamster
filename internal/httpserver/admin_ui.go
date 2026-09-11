package httpserver

import (
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
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// handleAdminPage renders the admin UI. Notices arrive as query parameters
// because a form post answers with a redirect, which carries no body.
func (s *Server) handleAdminPage(w http.ResponseWriter, r *http.Request) {
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
	if page.Templates, err = s.store.ListTemplates(); err != nil {
		page.Error = err.Error()
	}
	if page.Destinations, err = s.store.ListDestinations(); err != nil {
		page.Error = err.Error()
	}
	// A destination in a channel this session may not see is not theirs to read.
	if page.Destinations, err = s.visibleDestinations(r, page.Destinations); err != nil {
		page.Error = err.Error()
	}
	if page.Routes, err = s.store.ListRoutes(); err != nil {
		page.Error = err.Error()
	}

	if selected := r.URL.Query().Get("edit"); selected != "" {
		s.loadForEditing(&page, selected, r.URL.Query().Get("id"))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Admin(page).Render(r.Context(), w); err != nil {
		log.Printf("render admin page: %v", err)
	}
}

// loadForEditing fills in the record a form should start from. A record that
// has gone missing is reported on the page, which stays useful, rather than
// turning the whole request into a 404.
func (s *Server) loadForEditing(page *views.Page, section, id string) {
	if id == "" {
		return
	}

	var err error
	switch section {
	case "templates":
		var template models.Template
		if template, err = s.store.GetTemplate(id); err == nil {
			page.EditTemplate = &template
		}
	case "destinations":
		var destination models.Destination
		if destination, err = s.store.GetDestination(id); err == nil {
			page.EditDestination = &destination
		}
	case "routes":
		var route models.Route
		if route, err = s.store.GetRoute(id); err == nil {
			page.EditRoute = &route
		}
	default:
		return
	}

	if err != nil {
		page.Error = err.Error()
	}
}

// formPost guards the state-changing form endpoints. Basic auth credentials
// ride along on any cross-site form post, so the request has to prove it came
// from this origin; there is no session to hang a CSRF token on.
func (s *Server) formPost(handler func(*http.Request) (string, error)) http.HandlerFunc {
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
			redirectToAdmin(w, r, "", "invalid form submission")
			return
		}

		notice, err := handler(r)
		if err != nil {
			redirectToAdmin(w, r, "", err.Error())
			return
		}
		redirectToAdmin(w, r, notice, "")
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

func redirectToAdmin(w http.ResponseWriter, r *http.Request, notice, message string) {
	target := &url.URL{Path: "/admin"}
	query := target.Query()
	if notice != "" {
		query.Set("notice", notice)
	}
	if message != "" {
		query.Set("error", message)
	}
	target.RawQuery = query.Encode()

	http.Redirect(w, r, target.String(), http.StatusSeeOther)
}

func (s *Server) saveTemplate(r *http.Request) (string, error) {
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
		if _, err := s.store.CreateTemplate(template); err != nil {
			return "", err
		}
		return "Template created.", nil
	}
	if _, err := s.store.UpdateTemplate(template); err != nil {
		return "", err
	}
	return "Template updated.", nil
}

func (s *Server) saveDestination(r *http.Request) (string, error) {
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
		if _, err := s.store.CreateDestination(destination); err != nil {
			return "", err
		}
		return "Destination created.", nil
	}
	if _, err := s.store.UpdateDestination(destination); err != nil {
		return "", err
	}
	return "Destination updated.", nil
}

func (s *Server) saveRoute(r *http.Request) (string, error) {
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
	if err := s.validateRoute(route); err != nil {
		return "", err
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
	if route.ID == "" {
		if _, err := s.store.CreateRoute(route); err != nil {
			return "", err
		}
		return "Route created.", nil
	}
	if _, err := s.store.UpdateRoute(route); err != nil {
		return "", err
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
	if err := s.store.DeleteTemplate(r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Template deleted.", nil
}

func (s *Server) deleteDestination(r *http.Request) (string, error) {
	if err := s.store.DeleteDestination(r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Destination deleted.", nil
}

// Both write paths validate the same way, because the tree rules are routing's
// and neither the form nor the API may be the only place they hold.
func (s *Server) validateRoute(route models.Route) error {
	existing, err := s.store.ListRoutes()
	if err != nil {
		return err
	}
	return routing.ValidateRoute(route, existing)
}

func (s *Server) validateRouteDelete(id string) error {
	existing, err := s.store.ListRoutes()
	if err != nil {
		return err
	}
	return routing.ValidateDelete(id, existing)
}

func (s *Server) deleteRoute(r *http.Request) (string, error) {
	if err := s.validateRouteDelete(r.PostFormValue("id")); err != nil {
		return "", err
	}
	if err := s.store.DeleteRoute(r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Route deleted.", nil
}
