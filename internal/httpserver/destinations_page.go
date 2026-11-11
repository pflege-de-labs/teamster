package httpserver

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// handleDestinationList serves GET /admin/destinations: the destinations this session may read.
func (s *Server) handleDestinationList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if id := q.Get("edit"); id != "" { // links already in Teams chats
		markChecked(r)
		http.Redirect(w, r, recordEditURL("destinations", id, q), http.StatusMovedPermanently)
		return
	}
	list := views.DestinationList{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		CanCreate: s.allow(r, authz.ActionCreate, authz.Resource{Type: typeDestination}),
	}

	all, err := s.store.ListDestinations(ctx)
	if err == nil {
		all, err = s.listDestinations(r, all)
	}
	if err != nil {
		list.Error = failureText(ctx, "list destinations", err)
	}
	teams := make([]string, len(all))
	for i, d := range all {
		teams[i] = d.TeamID
	}
	states := s.installStates(ctx, teams)
	for _, d := range all {
		list.Rows = append(list.Rows, views.DestinationRow{Destination: d, InstallState: states[d.TeamID]})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.DestinationListPage(list).Render(ctx, w); err != nil {
		logError(ctx, "render destination list", err)
	}
}

// handleDestinationRecord serves GET /admin/destinations/{id}; a record the session may not read is a 404.
func (s *Server) handleDestinationRecord(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")
	if !s.can(r, authz.ActionRead, typeDestination, id) {
		http.NotFound(w, r)
		return
	}
	d, ok := s.readableDestination(w, r, id)
	if !ok {
		return
	}

	rec := s.destinationRecord(r, d)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.DestinationRecordPage(rec).Render(ctx, w); err != nil {
		logError(ctx, "render destination record", err)
	}
}

// readableDestination loads a destination the session's scope lets it see; otherwise it has answered 404 or 500.
func (s *Server) readableDestination(w http.ResponseWriter, r *http.Request, id string) (models.Destination, bool) {
	ctx := r.Context()
	d, err := s.store.GetDestination(ctx, id)
	if err == nil {
		var visible []models.Destination
		if visible, err = s.listDestinations(r, []models.Destination{d}); err == nil && len(visible) == 0 {
			err = store.ErrNotFound // a channel-scoped role's list hides it, so the page must too
		}
	}
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return d, false
	}
	if err != nil {
		logError(ctx, "get destination", err)
		http.Error(w, "could not load the destination", http.StatusInternalServerError)
		return d, false
	}
	return d, true
}

// destinationRecord builds the record page of d for the session: access, usages and sharing.
func (s *Server) destinationRecord(r *http.Request, d models.Destination) views.DestinationRecord {
	ctx := r.Context()
	id := d.ID
	q := r.URL.Query()
	_, brokerAvailable := s.brokerSession(r)
	rec := views.DestinationRecord{
		Viewer: s.viewerFor(r), Notice: q.Get("notice"), Error: q.Get("error"),
		Destination: d, Form: d, BrokerAvailable: brokerAvailable,
		InstallState:   s.installStates(ctx, []string{d.TeamID})[d.TeamID],
		CanUpdate:      s.can(r, authz.ActionUpdate, typeDestination, id),
		CanDelete:      s.can(r, authz.ActionDelete, typeDestination, id),
		CanMakeDefault: !d.IsDefault && s.allow(r, authz.ActionAdminister, authz.Resource{Type: typeDestination}),
	}
	_, edit := q["edit"]
	rec.Head = views.RecordHead{
		Href: "/admin/destinations/" + url.PathEscape(id), ListHref: "/admin/destinations", ListLabel: i18n.T(ctx, "nav.destinations"), Title: d.Name,
		Editing: edit && rec.CanUpdate,
	}

	routes, err := s.store.ListRoutes(ctx)
	if err == nil {
		for _, route := range readable(s, r, typeRoute, routes, routeID) {
			if route.DestinationID == id {
				rec.Routes = append(rec.Routes, views.UsedBy{Href: recordEditPath("routes", route.ID), Label: views.RouteLabel(ctx, route)})
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
		if e.DestinationID == id {
			rec.Webhooks = append(rec.Webhooks, views.UsedBy{Href: recordEditPath("webhooks", e.ID), Label: e.TeamSlug + "/" + e.ChannelSlug})
		}
	}
	if err == nil {
		rec.Sharing, err = s.sharingFor(r, typeDestination, id, recordEditPath("destinations", id))
	}
	if err != nil {
		rec.Error = failureText(ctx, "load destination page", err)
	}
	return rec
}

// handleDestinationNew serves GET /admin/destinations/new; the middleware lets a viewer read, so create is checked here.
func (s *Server) handleDestinationNew(w http.ResponseWriter, r *http.Request) {
	if err := s.mayRecord(r, authz.ActionCreate, typeDestination, ""); err != nil {
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	}
	_, brokerAvailable := s.brokerSession(r)
	s.renderDestinationNew(w, r, http.StatusOK, views.DestinationNew{
		Viewer: s.viewerFor(r), Error: r.URL.Query().Get("error"), BrokerAvailable: brokerAvailable,
	})
}

func (s *Server) renderDestinationNew(w http.ResponseWriter, r *http.Request, status int, p views.DestinationNew) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.DestinationNewPage(p).Render(r.Context(), w); err != nil {
		logError(r.Context(), "render new destination", err)
	}
}

// handleDestinationSave serves POST /admin/destinations: a saved record redirects to its page, a refused one
// re-renders the form with what was typed.
func (s *Server) handleDestinationSave(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/destinations", w, r) {
		return
	}
	ctx := r.Context()
	// An id the session cannot read answers like a missing one, before anything is written or rendered.
	if id := r.PostFormValue("id"); id != "" {
		if !s.can(r, authz.ActionRead, typeDestination, id) {
			http.NotFound(w, r)
			return
		}
		// Otherwise moving an out-of-scope record into scope would pass the delivery check.
		if _, ok := s.readableDestination(w, r, id); !ok {
			return
		}
	}
	saved, notice, err := s.storeDestination(r)
	if err == nil {
		redirectTo("/admin/destinations/"+url.PathEscape(saved.ID), w, r, notice, "")
		return
	}

	form := postedDestination(r)
	message := visibleError(ctx, "save destination", err)
	// Errors written for the reader are theirs to fix; the rest is ours.
	status := http.StatusUnprocessableEntity
	if !userFacing(err) {
		status = http.StatusInternalServerError
	}

	if form.ID == "" {
		_, brokerAvailable := s.brokerSession(r)
		s.renderDestinationNew(w, r, status, views.DestinationNew{
			Viewer: s.viewerFor(r), Error: message, Form: form, BrokerAvailable: brokerAvailable,
		})
		return
	}
	stored, ok := s.readableDestination(w, r, form.ID)
	if !ok {
		return
	}
	rec := s.destinationRecord(r, stored)
	rec.Form, rec.Error, rec.Notice = form, message, ""
	rec.Head.Editing = rec.CanUpdate
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := views.DestinationRecordPage(rec).Render(ctx, w); err != nil {
		logError(ctx, "render destination record", err)
	}
}

// destinationTarget is where a destination post lands: the record it named, or
// the list once the record is gone.
func destinationTarget(toListOnSuccess bool) func(*http.Request, bool) string {
	return func(r *http.Request, failed bool) string {
		id := r.PostFormValue("id")
		if id == "" || (toListOnSuccess && !failed) {
			return "/admin/destinations"
		}
		return "/admin/destinations/" + url.PathEscape(id)
	}
}
