package httpserver

import (
	"errors"
	"net/http"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

// handleTemplateList serves GET /admin/templates: the templates this session may read, and the source defaults.
func (s *Server) handleTemplateList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if id := q.Get("edit"); id != "" { // links already in Teams chats
		markChecked(r)
		http.Redirect(w, r, recordEditURL("templates", id, q), http.StatusMovedPermanently)
		return
	}
	list := views.TemplateList{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		CanCreate: s.allow(r, authz.ActionCreate, authz.Resource{Type: typeTemplate}),
	}

	all, err := s.store.ListTemplates(ctx)
	if err != nil {
		list.Error = failureText(ctx, "list templates", err)
	}
	all = readable(s, r, typeTemplate, all, templateID)
	defaults, err := s.store.SourceDefaultTemplates(ctx)
	if err != nil {
		list.Error = failureText(ctx, "list templates", err)
	}
	for _, t := range all {
		list.Rows = append(list.Rows, views.TemplateRow{Template: t, DefaultFor: views.DefaultFor(ctx, defaults, t.ID)})
	}
	list.SourceDefaults = views.SourceDefaults{
		Templates: all, Defaults: defaults,
		CanEdit: s.allow(r, authz.ActionEdit, authz.Resource{Type: typeTemplate}),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TemplateListPage(list).Render(ctx, w); err != nil {
		logError(ctx, "render template list", err)
	}
}

// handleTemplateRecord serves GET /admin/templates/{id}; a record the session may not read is a 404.
func (s *Server) handleTemplateRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	t, ok := s.readableTemplate(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.TemplateRecordPage(s.templateRecord(r, t)).Render(ctx, w); err != nil {
		logError(ctx, "render template record", err)
	}
}

// readableTemplate loads a template the session may read; otherwise it has answered 404 or 500.
func (s *Server) readableTemplate(w http.ResponseWriter, r *http.Request, id string) (models.Template, bool) {
	if !s.can(r, authz.ActionRead, typeTemplate, id) {
		http.NotFound(w, r)
		return models.Template{}, false
	}
	t, err := s.store.GetTemplate(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return t, false
	}
	if err != nil {
		logError(r.Context(), "get template", err)
		http.Error(w, "could not load the template", http.StatusInternalServerError)
		return t, false
	}
	return t, true
}

// templateEditor is what the template form offers beside its fields.
func templateEditor() views.TemplateEditor {
	return views.TemplateEditor{
		Presets:        append(cards.Presets(), cards.Variants()...),
		Snippets:       cards.Snippets(),
		Starter:        cards.Starter,
		Vocabulary:     templates.EditorVocabulary(),
		PreviewSamples: previewSamples(),
	}
}

// templateRecord builds the record page of t for the session: access, usages and sharing.
func (s *Server) templateRecord(r *http.Request, t models.Template) views.TemplateRecord {
	ctx := r.Context()
	id := t.ID
	q := r.URL.Query()
	rec := views.TemplateRecord{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		Template: t, Form: t, Editor: templateEditor(),
		CanUpdate: s.can(r, authz.ActionUpdate, typeTemplate, id),
		CanDelete: s.can(r, authz.ActionDelete, typeTemplate, id),
		// requestAuthorization's check on /api/templates/preview.
		CanPreview: s.allow(r, authz.ActionView, authz.Resource{Type: typeTemplate}),
	}
	_, edit := q["edit"]
	rec.Head = views.RecordHead{
		Href: recordEditPath("templates", id), ListHref: "/admin/templates", ListLabel: i18n.T(ctx, "nav.templates"), Title: t.Name,
		Editing: edit && rec.CanUpdate,
	}

	routes, err := s.store.ListRoutes(ctx)
	if err == nil {
		for _, route := range readable(s, r, typeRoute, routes, routeID) {
			if route.TemplateID == id {
				rec.UsedBy = append(rec.UsedBy, views.UsedBy{Href: recordEditPath("routes", route.ID), Label: views.RouteLabel(ctx, route)})
			}
		}
	}
	var endpoints []models.WebhookEndpoint
	if err == nil {
		if endpoints, err = s.store.ListWebhookEndpoints(ctx); err == nil {
			endpoints, err = s.listEndpoints(r, endpoints)
		}
	}
	for _, e := range endpoints {
		if e.TemplateID == id {
			rec.UsedBy = append(rec.UsedBy, views.UsedBy{Href: recordEditPath("webhooks", e.ID), Label: e.TeamSlug + "/" + e.ChannelSlug})
		}
	}
	var defaults map[string]string
	if err == nil {
		defaults, err = s.store.SourceDefaultTemplates(ctx)
	}
	if rec.DefaultFor = views.DefaultFor(ctx, defaults, id); rec.DefaultFor != "" {
		rec.UsedBy = append(rec.UsedBy, views.UsedBy{Href: "/admin/templates#source-defaults", Label: i18n.T(ctx, "templates.default_for", rec.DefaultFor)})
	}
	var global string
	if err == nil {
		global, err = s.store.GetGlobalDefaultTemplate(ctx)
	}
	if err == nil && global == id {
		rec.UsedBy = append(rec.UsedBy, views.UsedBy{Href: "/admin/routes", Label: i18n.T(ctx, "routes.global_default")})
	}
	if err == nil {
		rec.Sharing, err = s.sharingFor(r, typeTemplate, id, recordEditPath("templates", id))
	}
	if err != nil {
		rec.Error = failureText(ctx, "load template page", err)
	}
	return rec
}

// handleTemplateNew serves GET /admin/templates/new; the middleware lets a viewer read, so create is checked here.
func (s *Server) handleTemplateNew(w http.ResponseWriter, r *http.Request) {
	if err := s.mayRecord(r, authz.ActionCreate, typeTemplate, ""); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	s.renderTemplateNew(w, r, http.StatusOK, views.TemplateNew{
		Viewer: s.viewerFor(r), Error: r.URL.Query().Get("error"), Editor: templateEditor(),
	})
}

func (s *Server) renderTemplateNew(w http.ResponseWriter, r *http.Request, status int, p views.TemplateNew) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.TemplateNewPage(p).Render(r.Context(), w); err != nil {
		logError(r.Context(), "render new template", err)
	}
}

// postedTemplate is the template as the form submitted it.
func postedTemplate(r *http.Request) models.Template {
	return models.Template{
		ID:    r.PostFormValue("id"),
		Name:  r.PostFormValue("name"),
		Title: r.PostFormValue("title"),
		Text:  r.PostFormValue("message_text"),
		Body:  r.PostFormValue("body"),
		// No box ticked is any source (ADR 0053).
		Sources: r.PostForm["sources"],
	}
}

// storeTemplate creates or updates the posted template and says which it did.
func (s *Server) storeTemplate(r *http.Request) (models.Template, string, error) {
	template := postedTemplate(r)
	if err := s.mayRecord(r, writeAction(template.ID), typeTemplate, template.ID); err != nil {
		return template, "", err
	}
	if err := templates.Validate(template); err != nil {
		return template, "", userError{err}
	}
	if template.ID == "" {
		created, err := s.createTemplate(r, template)
		return created, "Template created.", err
	}
	_, err := s.store.UpdateTemplate(r.Context(), template)
	return template, "Template updated.", err
}

// handleTemplateSave serves POST /admin/templates: a saved record redirects to its page, a refused one
// re-renders the form with what was typed.
func (s *Server) handleTemplateSave(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/templates", w, r) {
		return
	}
	ctx := r.Context()
	// An id the session cannot read answers like a missing one, before anything is written or rendered.
	if id := r.PostFormValue("id"); id != "" {
		if _, ok := s.readableTemplate(w, r, id); !ok {
			return
		}
	}
	saved, notice, err := s.storeTemplate(r)
	if err == nil {
		redirectTo(recordEditPath("templates", saved.ID), w, r, notice, "")
		return
	}

	form := postedTemplate(r)
	message := visibleError(ctx, "save template", err)
	// Errors written for the reader are theirs to fix; the rest is ours.
	status := http.StatusUnprocessableEntity
	if !userFacing(err) {
		status = http.StatusInternalServerError
	}

	if form.ID == "" {
		s.renderTemplateNew(w, r, status, views.TemplateNew{
			Viewer: s.viewerFor(r), Error: message, Form: form, Editor: templateEditor(),
		})
		return
	}
	stored, ok := s.readableTemplate(w, r, form.ID)
	if !ok {
		return
	}
	rec := s.templateRecord(r, stored)
	rec.Form, rec.Error, rec.Notice = form, message, ""
	rec.Head.Editing = rec.CanUpdate
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.TemplateRecordPage(rec).Render(ctx, w); err != nil {
		logError(ctx, "render template record", err)
	}
}
