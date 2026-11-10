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

// handleDestinationList serves GET /admin/destinations: the destinations this session may read.
func (s *Server) handleDestinationList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	if id := q.Get("edit"); id != "" { // links already in Teams chats
		markChecked(r)
		http.Redirect(w, r, destinationEditURL(id, q), http.StatusMovedPermanently)
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
	d, err := s.store.GetDestination(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		logError(ctx, "get destination", err)
		http.Error(w, "could not load the destination", http.StatusInternalServerError)
		return
	}

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
		ListHref: "/admin/destinations", ListLabel: i18n.T(ctx, "nav.destinations"), Title: d.Name,
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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.DestinationRecordPage(rec).Render(ctx, w); err != nil {
		logError(ctx, "render destination record", err)
	}
}
