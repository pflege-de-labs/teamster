package httpserver

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	page := s.adminPage(r, r.URL.Query().Get("notice"), r.URL.Query().Get("error"))
	page.Tab = cmp.Or(r.URL.Query().Get("tab"), r.URL.Query().Get("edit"))

	if selected := r.URL.Query().Get("edit"); selected != "" {
		s.loadForEditing(r, &page, selected, r.URL.Query().Get("id"))
	}

	s.renderAdmin(w, r, page)
}

// adminPage collects everything the admin page draws. It is separate from the
// handler because the one post whose answer cannot be a redirect -- generating
// a webhook token -- has to render the same page itself.
func (s *Server) adminPage(r *http.Request, notice, errText string) views.Page {
	ctx := r.Context()
	_, brokerAvailable := s.brokerSession(r)
	page := views.Page{
		Notice:          notice,
		Error:           errText,
		PreviewSamples:  previewSamples(),
		Snippets:        cards.Snippets(),
		Starter:         cards.Starter,
		Presets:         cards.Presets(),
		Vocabulary:      templates.EditorVocabulary(),
		Viewer:          s.viewerFor(r),
		CanEdit:         s.allow(r, authz.ActionEdit, authz.Resource{Type: "Template"}),
		CanManage:       s.allow(r, authz.ActionAdminister, authz.Resource{Type: "Grant"}),
		BrokerAvailable: brokerAvailable,
	}

	var err error
	if page.Templates, err = s.store.ListTemplates(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	page.Templates = readable(s, r, typeTemplate, page.Templates, templateID)
	if page.Destinations, err = s.store.ListDestinations(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	// A destination in a channel this session may not see is not theirs to read.
	if page.Recipients, err = s.store.ListRecipients(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	page.Recipients = s.targetableRecipients(r, page.Recipients)
	// Only with the bot there is anyone to deliver to (ADR 0062).
	page.CanAddress = botConfigured(s.cfg.Bot) && s.mayAddress(r)
	if page.Destinations, err = s.listDestinations(r, page.Destinations); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	if page.Routes, err = s.store.ListRoutes(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	page.Routes = readable(s, r, typeRoute, page.Routes, routeID)
	if fallback, err := s.store.GetDefaultDestination(ctx); err == nil {
		page.GlobalDefault = &fallback
	} else if !errors.Is(err, store.ErrNotFound) {
		page.Error = failureText(ctx, "load admin page", err)
	}
	if page.GlobalDefaultTemplateID, err = s.store.GetGlobalDefaultTemplate(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	if page.SourceDefaults, err = s.store.SourceDefaultTemplates(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	if page.WebhookEndpoints, err = s.store.ListWebhookEndpoints(ctx); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	if page.WebhookEndpoints, err = s.listEndpoints(r, page.WebhookEndpoints); err != nil {
		page.Error = failureText(ctx, "load admin page", err)
	}
	s.fillAccess(r, &page)
	teams := make([]string, len(page.Destinations))
	for i, d := range page.Destinations {
		teams[i] = d.TeamID
	}
	page.InstallStates = s.installStates(ctx, teams)

	return page
}

// fillAccess answers, per listed record, what the page may offer for it.
func (s *Server) fillAccess(r *http.Request, page *views.Page) {
	page.CanCreate = map[string]bool{}
	for _, typ := range []string{typeTemplate, typeDestination, typeRoute, typeWebhookEndpoint} {
		page.CanCreate[typ] = s.allow(r, authz.ActionCreate, authz.Resource{Type: typ})
	}
	page.Access = map[string]views.RecordAccess{}
	add := func(typ, id string) {
		flags := s.recordAccess(r, typ, id)
		page.Access[views.AccessKey(typ, id)] = views.RecordAccess{Update: flags.Update, Delete: flags.Delete}
	}
	for _, t := range page.Templates {
		add(typeTemplate, t.ID)
	}
	for _, d := range page.Destinations {
		add(typeDestination, d.ID)
	}
	for _, rt := range page.Routes {
		add(typeRoute, rt.ID)
	}
	for _, e := range page.WebhookEndpoints {
		add(typeWebhookEndpoint, e.ID)
	}
}

func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, page views.Page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.Admin(page).Render(r.Context(), w); err != nil {
		logError(r.Context(), "render admin page", err)
	}
}

// loadForEditing fills in the record a form should start from. A record that
// has gone missing is reported on the page, which stays useful, rather than
// turning the whole request into a 404.
func (s *Server) loadForEditing(r *http.Request, page *views.Page, section, id string) {
	ctx := r.Context()
	if id == "" {
		return
	}
	typ, ok := map[string]string{
		"templates": typeTemplate, "destinations": typeDestination, "routes": typeRoute, "webhooks": typeWebhookEndpoint,
	}[section]
	if !ok {
		return
	}
	if err := s.mayRecord(r, authz.ActionRead, typ, id); err != nil {
		page.Error = visibleError(ctx, "load record for editing", err)
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
			page.Recipients = s.keepEditedRecipient(ctx, page.Recipients, route.RecipientID)
		}
	case "webhooks":
		var endpoint models.WebhookEndpoint
		if endpoint, err = s.store.GetWebhookEndpoint(ctx, id); err == nil {
			page.EditWebhookEndpoint = &endpoint
		}
	default:
		return
	}

	if err != nil {
		page.Error = visibleError(ctx, "load record for editing", err)
		return
	}
	if page.Sharing, err = s.sharingFor(r, typ, id, "/admin?edit="+section+"&id="+url.QueryEscape(id)); err != nil {
		page.Error = failureText(ctx, "load sharing", err)
	}
}

// formGuard runs the checks a form post has to pass before anything is written,
// and answers the request itself when one fails. It is separate from formPostTo
// because not every form post can answer with a redirect. It takes the target
// so that a rejection lands where the form it came from would have.
func formGuard(target string, w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	if !sameOrigin(r) {
		http.Error(w, "cross-origin form post rejected", http.StatusForbidden)
		return false
	}
	if err := r.ParseForm(); err != nil {
		redirectTo(target, w, r, "", "invalid form submission")
		return false
	}
	return true
}

// formPost guards the state-changing form endpoints that redirect back to
// the /admin tab they were posted from. Basic auth credentials ride along on any cross-site form post, so
// the request has to prove it came from this origin; there is no session to
// hang a CSRF token on.
func (s *Server) formPost(tab string, handler func(*http.Request) (string, error)) http.HandlerFunc {
	return s.formPostTo(adminTabPath(tab), handler)
}

// adminTabPath is /admin opened on one of its tabs.
func adminTabPath(tab string) string {
	return "/admin?tab=" + url.QueryEscape(tab)
}

// formPostTo is formPost for the endpoints that land somewhere other than
// /admin -- the notifications page's own unlink control, most immediately.
func (s *Server) formPostTo(target string, handler func(*http.Request) (string, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !formGuard(target, w, r) {
			return
		}

		notice, err := handler(r)
		if err != nil {
			redirectTo(target, w, r, "", visibleError(r.Context(), "form post", err))
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
	// A target may carry a query of its own, such as the tab to land on.
	dest, err := url.Parse(target)
	if err != nil {
		dest = &url.URL{Path: "/admin"}
	}
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

// writeAction is create for a form without an id and update for one with.
func writeAction(id string) string {
	if id == "" {
		return authz.ActionCreate
	}
	return authz.ActionUpdate
}

func (s *Server) saveTemplate(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.mayRecord(r, writeAction(r.PostFormValue("id")), typeTemplate, r.PostFormValue("id")); err != nil {
		return "", err
	}
	template := models.Template{
		ID:    r.PostFormValue("id"),
		Name:  r.PostFormValue("name"),
		Title: r.PostFormValue("title"),
		Text:  r.PostFormValue("message_text"),
		Body:  r.PostFormValue("body"),
		// No box ticked is any source (ADR 0053).
		Sources: r.PostForm["sources"],
	}
	if err := templates.Validate(template); err != nil {
		return "", userError{err}
	}
	if template.ID == "" {
		if _, err := s.createTemplate(r, template); err != nil {
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
	if err := s.mayRecord(r, writeAction(r.PostFormValue("id")), typeDestination, r.PostFormValue("id")); err != nil {
		return "", err
	}
	destination := models.Destination{
		ID:        r.PostFormValue("id"),
		Name:      r.PostFormValue("name"),
		TeamID:    r.PostFormValue("team_id"),
		ChannelID: r.PostFormValue("channel_id"),
	}
	// Typing a channel id the picker would not have offered reaches here, which
	// is why the check is on the write rather than on the list.
	allowed, err := s.mayDeliverTo(r, destination.TeamID, destination.ChannelID)
	if destination.ID != "" {
		allowed, err = s.mayRepointDestination(r, destination)
	}
	if err != nil {
		return "", err
	}
	if !allowed {
		return "", errDeliveryRefused
	}

	if destination.ID == "" {
		if _, err := s.createDestination(r, destination); err != nil {
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
	if err := s.mayRecord(r, writeAction(r.PostFormValue("id")), typeRoute, r.PostFormValue("id")); err != nil {
		return "", err
	}
	selector, err := parseSelector(r.PostFormValue("label_selector"))
	if err != nil {
		return "", userError{err}
	}

	priority := 0
	if raw := strings.TrimSpace(r.PostFormValue("priority")); raw != "" {
		if priority, err = strconv.Atoi(raw); err != nil {
			return "", userError{err}
		}
	}

	destinationID, recipientID := r.PostFormValue("destination_id"), r.PostFormValue("recipient_id")
	var addressed bool
	if _, ok := r.PostForm["target"]; ok {
		if destinationID, recipientID, addressed, err = parseRouteTarget(r.PostFormValue("target")); err != nil {
			return "", err
		}
	}

	route := models.Route{
		ID:            r.PostFormValue("id"),
		Name:          r.PostFormValue("name"),
		ParentID:      r.PostFormValue("parent_id"),
		LabelSelector: selector,
		DestinationID: destinationID,
		RecipientID:   recipientID,
		Addressed:     addressed,
		TemplateID:    r.PostFormValue("template_id"),
		IsDefault:     r.PostFormValue("is_default") == "true",
		Greedy:        r.PostFormValue("greedy") == "true",
		Priority:      priority,
	}
	// A route is how an alert reaches a channel or a chat, so its targets are
	// checked the same way creating them would be.
	if err := s.routeWriteRefusal(r, route); err != nil {
		return "", err
	}
	created := route.ID == ""
	if _, err := s.saveRouteChecked(r, route); err != nil {
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
	id := r.PostFormValue("id")
	if err := s.mayRecord(r, authz.ActionDelete, typeTemplate, id); err != nil {
		return "", err
	}
	if err := s.deleteOwned(r.Context(), typeTemplate, id, func(ctx context.Context, tx store.Store) error {
		return tx.DeleteTemplate(ctx, id)
	}); err != nil {
		return "", err
	}
	return "Template deleted.", nil
}

func (s *Server) setDefaultDestination(r *http.Request) (string, error) {
	if err := s.store.SetDefaultDestination(r.Context(), r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Global default destination changed.", nil
}

func (s *Server) deleteDestination(r *http.Request) (string, error) {
	id := r.PostFormValue("id")
	if err := s.mayRecord(r, authz.ActionDelete, typeDestination, id); err != nil {
		return "", err
	}
	if err := s.deleteOwned(r.Context(), typeDestination, id, func(ctx context.Context, tx store.Store) error {
		return tx.DeleteDestination(ctx, id)
	}); err != nil {
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
func (s *Server) saveRouteChecked(r *http.Request, route models.Route) (models.Route, error) {
	var saved models.Route
	err := s.store.WithSerializableTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		existing, err := tx.ListRoutes(ctx)
		if err != nil {
			return err
		}
		if err := routing.ValidateRoute(route, existing); err != nil {
			return invalidRoute{err}
		}
		if err := routeTemplateHandlesSource(ctx, tx, route, existing); err != nil {
			return err
		}
		if route.ID == "" {
			if saved, err = tx.CreateRoute(ctx, route); err != nil {
				return err
			}
			return grantOwner(ctx, tx, r, typeRoute, saved.ID)
		}
		saved, err = tx.UpdateRoute(ctx, route)
		return err
	})
	return saved, err
}

// routeTemplateHandlesSource refuses a route whose selector pins one webhook
// but whose template is written for others (ADR 0053). A template that no
// longer exists is left for delivery to report, as it always was.
func routeTemplateHandlesSource(ctx context.Context, tx store.Store, route models.Route, existing []models.Route) error {
	source := routing.PinnedSource(route, existing)
	if source == "" || route.TemplateID == "" {
		return nil
	}
	template, err := tx.GetTemplate(ctx, route.TemplateID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if !template.Handles(source) {
		return invalidRoute{fmt.Errorf("template %q does not handle %s alerts, which is all this route receives", template.Name, source)}
	}
	return nil
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
		if err := tx.DeleteRoute(ctx, id); err != nil {
			return err
		}
		return tx.DeletePermissionsFor(ctx, typeRoute, id)
	})
}

func (s *Server) deleteRoute(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.mayRecord(r, authz.ActionDelete, typeRoute, r.PostFormValue("id")); err != nil {
		return "", err
	}
	if err := s.routeDeleteRefusal(r, r.PostFormValue("id")); err != nil {
		return "", err
	}
	if err := s.deleteRouteChecked(ctx, r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Route deleted.", nil
}
