package httpserver

import (
	"errors"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// handleWebhookList serves GET /admin/webhooks: the endpoints this session may read.
func (s *Server) handleWebhookList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if id := q.Get("edit"); id != "" { // links already in Teams chats
		markChecked(r)
		http.Redirect(w, r, recordEditURL("webhooks", id, q), http.StatusMovedPermanently)
		return
	}
	list := views.WebhookList{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		CanCreate: s.allow(r, authz.ActionCreate, authz.Resource{Type: typeWebhookEndpoint}),
	}

	all, err := s.store.ListWebhookEndpoints(ctx)
	if err == nil {
		all, err = s.listEndpoints(r, all)
	}
	if err != nil {
		list.Error = failureText(ctx, "list webhooks", err)
	}
	choices, err := s.webhookChoices(r)
	if err != nil {
		list.Error = failureText(ctx, "list webhooks", err)
	}
	for _, e := range all {
		row := views.WebhookRow{Endpoint: e, DestinationName: destinationLink(choices.Destinations, e.DestinationID).Label}
		if e.TemplateID != "" {
			row.TemplateName = templateLink(choices.Templates, e.TemplateID).Label
		}
		list.Rows = append(list.Rows, row)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.WebhookListPage(list).Render(ctx, w); err != nil {
		logError(ctx, "render webhook list", err)
	}
}

// webhookChoices are the destinations and templates this session may read, which the form offers.
func (s *Server) webhookChoices(r *http.Request) (views.WebhookChoices, error) {
	ctx := r.Context()
	var choices views.WebhookChoices
	destinations, err := s.store.ListDestinations(ctx)
	if err == nil {
		choices.Destinations, err = s.listDestinations(r, destinations)
	}
	if err != nil {
		return choices, err
	}
	templates, err := s.store.ListTemplates(ctx)
	choices.Templates = readable(s, r, typeTemplate, templates, templateID)
	return choices, err
}

// destinationLink names destination id and links it when it is among the readable ones; otherwise the id stays.
func destinationLink(readable []models.Destination, id string) views.UsedBy {
	for _, d := range readable {
		if d.ID == id {
			return views.UsedBy{Href: recordEditPath("destinations", id), Label: d.Name}
		}
	}
	return views.UsedBy{Label: id}
}

// templateLink is destinationLink for templates.
func templateLink(readable []models.Template, id string) views.UsedBy {
	for _, t := range readable {
		if t.ID == id {
			return views.UsedBy{Href: recordEditPath("templates", id), Label: t.Name}
		}
	}
	return views.UsedBy{Label: id}
}

// handleWebhookRecord serves GET /admin/webhooks/{id}; a record the session may not read is a 404.
func (s *Server) handleWebhookRecord(w http.ResponseWriter, r *http.Request) {
	e, ok := s.readableEndpoint(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	s.renderWebhookRecord(w, r, http.StatusOK, s.webhookRecord(r, e))
}

func (s *Server) renderWebhookRecord(w http.ResponseWriter, r *http.Request, status int, rec views.WebhookRecord) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.WebhookRecordPage(rec).Render(r.Context(), w); err != nil {
		logError(r.Context(), "render webhook record", err)
	}
}

// readableEndpoint loads an endpoint the session may read and see the channel of; otherwise it has answered 404 or 500.
func (s *Server) readableEndpoint(w http.ResponseWriter, r *http.Request, id string) (models.WebhookEndpoint, bool) {
	if !s.can(r, authz.ActionRead, typeWebhookEndpoint, id) {
		http.NotFound(w, r)
		return models.WebhookEndpoint{}, false
	}
	e, err := s.store.GetWebhookEndpoint(r.Context(), id)
	if err == nil {
		var visible []models.WebhookEndpoint
		if visible, err = s.listEndpoints(r, []models.WebhookEndpoint{e}); err == nil && len(visible) == 0 {
			err = store.ErrNotFound // a channel-scoped role's list hides it, so the page must too
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return e, false
	}
	if err != nil {
		logError(r.Context(), "get webhook", err)
		http.Error(w, "could not load the webhook", http.StatusInternalServerError)
		return e, false
	}
	return e, true
}

// webhookRecord builds the record page of e for the session: what it points at, its route and sharing.
func (s *Server) webhookRecord(r *http.Request, e models.WebhookEndpoint) views.WebhookRecord {
	ctx := r.Context()
	id := e.ID
	q := r.URL.Query()
	rec := views.WebhookRecord{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		Endpoint: e, Form: e,
		CanUpdate: s.can(r, authz.ActionUpdate, typeWebhookEndpoint, id),
		CanDelete: s.can(r, authz.ActionDelete, typeWebhookEndpoint, id),
	}
	_, edit := q["edit"]
	rec.Head = views.RecordHead{
		Href: recordEditPath("webhooks", id), ListHref: "/admin/webhooks", ListLabel: i18n.T(ctx, "nav.webhooks"),
		Title: "/teamsv2/" + e.TeamSlug + "/" + e.ChannelSlug, Editing: edit && rec.CanUpdate,
	}

	choices, err := s.webhookChoices(r)
	rec.Choices = choices
	rec.Destination = destinationLink(choices.Destinations, e.DestinationID)
	if e.TemplateID != "" {
		rec.Template = templateLink(choices.Templates, e.TemplateID)
	}
	var routes []models.Route
	if err == nil {
		routes, err = s.store.ListRoutes(ctx)
	}
	for _, route := range readable(s, r, typeRoute, routes, routeID) {
		if route.WebhookEndpointID == id {
			rec.RouteHref = recordEditPath("routes", route.ID)
		}
	}
	if err == nil {
		rec.Sharing, err = s.sharingFor(r, typeWebhookEndpoint, id, recordEditPath("webhooks", id))
	}
	if err != nil {
		rec.Error = failureText(ctx, "load webhook page", err)
	}
	return rec
}

// handleWebhookNew serves GET /admin/webhooks/new; the middleware lets a viewer read, so create is checked here.
func (s *Server) handleWebhookNew(w http.ResponseWriter, r *http.Request) {
	if err := s.mayRecord(r, authz.ActionCreate, typeWebhookEndpoint, ""); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	p := views.WebhookNew{Viewer: s.viewerFor(r), Error: r.URL.Query().Get("error")}
	var err error
	if p.Choices, err = s.webhookChoices(r); err != nil {
		p.Error = failureText(r.Context(), "load webhook form", err)
	}
	s.renderWebhookNew(w, r, http.StatusOK, p)
}

func (s *Server) renderWebhookNew(w http.ResponseWriter, r *http.Request, status int, p views.WebhookNew) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.WebhookNewPage(p).Render(r.Context(), w); err != nil {
		logError(r.Context(), "render new webhook", err)
	}
}

// handleWebhookSave serves POST /admin/webhooks. An update redirects to its record; a create answers with
// the record and its URL, because a redirect would carry the token into the browser history and the access
// log; a refused save re-renders the form with what was typed.
func (s *Server) handleWebhookSave(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/webhooks", w, r) {
		return
	}
	ctx := r.Context()
	// An id the session cannot read answers like a missing one, before anything is written or rendered.
	if id := r.PostFormValue("id"); id != "" {
		if _, ok := s.readableEndpoint(w, r, id); !ok {
			return
		}
	}
	saved, token, err := s.storeWebhook(r)
	switch {
	case err == nil && token == "":
		redirectTo(recordEditPath("webhooks", saved.ID), w, r, "Webhook updated.", "")
		return
	case err == nil:
		s.revealWebhookURL(w, r, saved, token, "Webhook created.")
		return
	}

	form := postedWebhook(r)
	message := visibleError(ctx, "save webhook", err)
	// Errors written for the reader are theirs to fix; the rest is ours.
	status := http.StatusUnprocessableEntity
	if !userFacing(err) {
		status = http.StatusInternalServerError
	}
	if form.ID == "" {
		p := views.WebhookNew{Viewer: s.viewerFor(r), Error: message, Form: form}
		p.Choices, _ = s.webhookChoices(r) // the error on show already says the save failed
		s.renderWebhookNew(w, r, status, p)
		return
	}
	stored, ok := s.readableEndpoint(w, r, form.ID)
	if !ok {
		return
	}
	rec := s.webhookRecord(r, stored)
	rec.Form, rec.Error, rec.Notice = form, message, ""
	rec.Head.Editing = rec.CanUpdate
	s.renderWebhookRecord(w, r, status, rec)
}

// revealWebhookURL answers with the endpoint's record and the one sight of its token there will ever be.
func (s *Server) revealWebhookURL(w http.ResponseWriter, r *http.Request, endpoint models.WebhookEndpoint, token, notice string) {
	rec := s.webhookRecord(r, endpoint)
	rec.NewURL = s.webhookURL(r, endpoint, token)
	rec.Notice = notice
	rec.Head.Editing = false
	rec.Head.Replace = true
	// Back and forward must not bring the token back from the cache.
	w.Header().Set("Cache-Control", "no-store")
	s.renderWebhookRecord(w, r, http.StatusOK, rec)
}
