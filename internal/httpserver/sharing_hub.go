package httpserver

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// principalFromForm reads whom a grant names: the picker's "type:id", the
// free-text fallback for someone not signed in yet, or the older type and id fields.
func principalFromForm(r *http.Request) (models.PrincipalType, string) {
	if picked := r.PostFormValue("principal"); strings.Contains(picked, ":") {
		kind, id, _ := strings.Cut(picked, ":")
		return models.PrincipalType(kind), id
	}
	if other := strings.TrimSpace(r.PostFormValue("other_id")); other != "" {
		return models.PrincipalType(r.PostFormValue("other_type")), other
	}
	return models.PrincipalType(r.PostFormValue("principal_type")), r.PostFormValue("principal_id")
}

// recordSections maps a record type to the admin UI section that edits it.
var recordSections = map[string]string{
	typeTemplate: "templates", typeDestination: "destinations", typeRoute: "routes", typeWebhookEndpoint: "webhooks",
}

func recordHref(typ, id string) string {
	if typ == typeGroup {
		return "/admin/groups?id=" + url.QueryEscape(id)
	}
	return "/admin?edit=" + recordSections[typ] + "&id=" + url.QueryEscape(id)
}

// recordIndex names every record of the permissioned types, and says which the
// viewer may share. It lists collections the viewer may not read too: the
// names are only used where a grant or a share right already reaches the record.
func (s *Server) recordIndex(ctx context.Context, r *http.Request) ([]views.HubClass, error) {
	var classes []views.HubClass
	add := func(typ string, records []views.HubRecord) {
		for i := range records {
			records[i].Href = recordHref(typ, records[i].ID)
			records[i].CanShare = s.can(r, authz.ActionShare, typ, records[i].ID)
		}
		classes = append(classes, views.HubClass{Type: typ, Records: records})
	}

	templates, err := s.store.ListTemplates(ctx)
	if err != nil {
		return nil, err
	}
	var records []views.HubRecord
	for _, t := range templates {
		records = append(records, views.HubRecord{ID: t.ID, Name: t.Name})
	}
	add(typeTemplate, records)

	destinations, err := s.store.ListDestinations(ctx)
	if err != nil {
		return nil, err
	}
	records = nil
	for _, d := range destinations {
		records = append(records, views.HubRecord{ID: d.ID, Name: d.Name})
	}
	add(typeDestination, records)

	routes, err := s.store.ListRoutes(ctx)
	if err != nil {
		return nil, err
	}
	records = nil
	for _, rt := range routes {
		records = append(records, views.HubRecord{ID: rt.ID, Name: rt.Name})
	}
	add(typeRoute, records)

	endpoints, err := s.store.ListWebhookEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	records = nil
	for _, e := range endpoints {
		records = append(records, views.HubRecord{ID: e.ID, Name: e.TeamSlug + "/" + e.ChannelSlug})
	}
	add(typeWebhookEndpoint, records)

	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	records = nil
	for _, g := range groups {
		records = append(records, views.HubRecord{ID: g.ID, Name: g.Name})
	}
	add(typeGroup, records)
	return classes, nil
}

// shareable keeps the records the viewer may share, and drops empty classes.
func shareable(index []views.HubClass) []views.HubClass {
	var out []views.HubClass
	for _, class := range index {
		kept := views.HubClass{Type: class.Type}
		for _, rec := range class.Records {
			if rec.CanShare {
				kept.Records = append(kept.Records, rec)
			}
		}
		if len(kept.Records) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// hubFor is the picker for /admin/access: the classes and records the viewer
// may share, and the sharing panel of the one record picked.
func (s *Server) hubFor(r *http.Request, index []views.HubClass) (*views.Hub, error) {
	hub := &views.Hub{Classes: shareable(index), Class: r.URL.Query().Get("class")}
	for _, class := range hub.Classes {
		if class.Type == hub.Class {
			hub.Records = class.Records
		}
	}
	if hub.Records == nil {
		hub.Class = ""
	}
	users, err := s.store.ListUsers(r.Context(), "", usersPageSize)
	if err != nil {
		return nil, err
	}
	groups, err := s.store.ListGroups(r.Context())
	if err != nil {
		return nil, err
	}
	hub.Principals = principalOptionsFor(users, groups)
	hub.Actions = slices.DeleteFunc(slices.Clone(recordActions), func(a string) bool { return a == authz.ActionOwn })

	if id := r.URL.Query().Get("id"); id != "" && hub.Class != "" && slices.ContainsFunc(hub.Records, func(rec views.HubRecord) bool { return rec.ID == id }) {
		returnTo := "/admin/access?class=" + url.QueryEscape(hub.Class) + "&id=" + url.QueryEscape(id)
		if hub.Selected, err = s.sharingFor(r, hub.Class, id, returnTo); err != nil {
			return nil, err
		}
		hub.SelectedID = id
	}
	return hub, nil
}

// handleShareBatchForm gives one principal the same actions on several records.
// A local group must exist; a user or provider group that has not signed in yet
// is accepted with a warning, because someone may be granted before their first sign-in.
func (s *Server) handleShareBatchForm(w http.ResponseWriter, r *http.Request) {
	target := safeReturn(r.FormValue("return"))
	if !formGuard(target, w, r) {
		return
	}
	kind, id := principalFromForm(r)
	id = strings.TrimSpace(id)

	var warn string
	if kind.Valid() && id != "" {
		switch kind {
		case models.PrincipalGroup:
			if _, err := s.store.GetGroup(r.Context(), id); err != nil {
				markChecked(r)
				redirectTo(target, w, r, "", i18n.T(r.Context(), "sharing.no_such_group"))
				return
			}
		case models.PrincipalUser:
			if _, err := s.store.GetUser(r.Context(), id); err != nil {
				warn = i18n.T(r.Context(), "sharing.not_seen")
			}
		}
	}
	err := s.shareMany(r, models.Permission{
		PrincipalType: kind, PrincipalID: id, ResourceType: r.PostFormValue("resource_type"), Actions: r.PostForm["actions"],
	}, r.PostForm["ids"])
	if err != nil {
		redirectTo(target, w, r, "", visibleError(r.Context(), "share", err))
		return
	}
	notice := i18n.T(r.Context(), "sharing.saved")
	if warn != "" {
		notice += " " + warn
	}
	redirectTo(target, w, r, notice, "")
}
