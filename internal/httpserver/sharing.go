package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

var (
	errGrant       = errors.New("a grant needs a principal type of user, group, idp_group or role, a principal, and actions")
	errNoSuchGrant = errors.New("no such permission")
)

// recordActions are what can be granted on a record; create belongs to the collection.
var recordActions = []string{authz.ActionRead, authz.ActionUpdate, authz.ActionDelete, authz.ActionAttach, authz.ActionShare, authz.ActionOwn}

// grantRefusal checks what changing who holds actions on a record takes: share
// for ordinary actions, transfer for ownership either way, and no granting an
// action the caller does not hold.
func (s *Server) grantRefusal(r *http.Request, typ, id string, before, after []string) error {
	if !permissionedType(typ) {
		return userError{errGrant}
	}
	if !s.can(r, authz.ActionShare, typ, id) {
		return userError{refusal{action: authz.ActionShare, typ: typ}}
	}
	if slices.Contains(before, authz.ActionOwn) != slices.Contains(after, authz.ActionOwn) && !s.can(r, authz.ActionTransfer, typ, id) {
		return userError{refusal{action: authz.ActionTransfer, typ: typ}}
	}
	for _, action := range after {
		if !slices.Contains(before, action) && !s.can(r, action, typ, id) {
			return userError{refusal{action: action, typ: typ}}
		}
	}
	return nil
}

// share sets the actions one principal holds on one record; none revokes.
func (s *Server) share(r *http.Request, p models.Permission) (models.Permission, error) {
	p.PrincipalID = strings.TrimSpace(p.PrincipalID)
	if !p.PrincipalType.Valid() || p.PrincipalID == "" || p.ResourceID == "" || p.ResourceID == "*" {
		return models.Permission{}, userError{errGrant}
	}
	for _, action := range p.Actions {
		if !slices.Contains(recordActions, action) {
			return models.Permission{}, userError{errGrant}
		}
	}
	slices.Sort(p.Actions)
	p.Actions = slices.Compact(p.Actions)

	var saved models.Permission
	err := s.store.WithTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		existing, err := tx.ListPermissionsFor(ctx, p.ResourceType, p.ResourceID)
		if err != nil {
			return err
		}
		var before []string
		for _, e := range existing {
			if e.PrincipalType == p.PrincipalType && e.PrincipalID == p.PrincipalID {
				before = e.Actions
			}
		}
		if err := s.grantRefusal(r, p.ResourceType, p.ResourceID, before, p.Actions); err != nil {
			return err
		}
		p.CreatedBy = principalSubject(r)
		saved, err = tx.PutPermission(ctx, p)
		return err
	})
	return saved, err
}

// unshare revokes one row, which takes what granting it would.
func (s *Server) unshare(r *http.Request, id string) (models.Permission, error) {
	var existing models.Permission
	err := s.store.WithTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		var err error
		existing, err = tx.GetPermission(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			markChecked(r)
			return userError{errNoSuchGrant}
		}
		if err != nil {
			return err
		}
		if err := s.grantRefusal(r, existing.ResourceType, existing.ResourceID, existing.Actions, nil); err != nil {
			return err
		}
		return tx.DeletePermission(ctx, id)
	})
	return existing, err
}

// safeReturn keeps a form's landing page on this site's admin UI.
func safeReturn(target string) string {
	if strings.HasPrefix(target, "/admin") && !strings.HasPrefix(target, "//") && !strings.ContainsAny(target, "\\\r\n") {
		return target
	}
	return "/admin"
}

func (s *Server) handleShareForm(w http.ResponseWriter, r *http.Request) {
	target := safeReturn(r.FormValue("return"))
	if !formGuard(target, w, r) {
		return
	}
	_, err := s.share(r, models.Permission{
		PrincipalType: models.PrincipalType(r.PostFormValue("principal_type")),
		PrincipalID:   r.PostFormValue("principal_id"),
		ResourceType:  r.PostFormValue("resource_type"),
		ResourceID:    r.PostFormValue("resource_id"),
		Actions:       r.PostForm["actions"],
	})
	if err != nil {
		markChecked(r)
		redirectTo(target, w, r, "", visibleError(r.Context(), "share", err))
		return
	}
	redirectTo(target, w, r, "Permissions saved.", "")
}

func (s *Server) handleUnshareForm(w http.ResponseWriter, r *http.Request) {
	target := safeReturn(r.FormValue("return"))
	if !formGuard(target, w, r) {
		return
	}
	if _, err := s.unshare(r, r.PostFormValue("id")); err != nil {
		redirectTo(target, w, r, "", visibleError(r.Context(), "unshare", err))
		return
	}
	redirectTo(target, w, r, "Permission revoked.", "")
}

// handleSharingAPI is GET /api/sharing?type=&id=, POST /api/sharing and
// DELETE /api/sharing/{id}.
func (s *Server) handleSharingAPI(w http.ResponseWriter, r *http.Request) {
	if id, ok := strings.CutPrefix(r.URL.Path, "/api/sharing/"); ok {
		if r.Method != http.MethodDelete || id == "" {
			w.Header().Set("Allow", http.MethodDelete)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		revoked, err := s.unshare(r, id)
		if err != nil {
			writeSharingError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, revoked)
		return
	}

	switch r.Method {
	case http.MethodGet:
		typ, id := r.URL.Query().Get("type"), r.URL.Query().Get("id")
		if err := s.mayRecord(r, authz.ActionRead, typ, id); err != nil || !permissionedType(typ) || id == "" {
			markChecked(r)
			writeError(w, r, http.StatusForbidden, refusal{action: authz.ActionRead, typ: typ})
			return
		}
		rows, err := s.store.ListPermissionsFor(r.Context(), typ, id)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		if rows == nil {
			rows = []models.Permission{}
		}
		writeJSON(w, http.StatusOK, rows)
	case http.MethodPost:
		var p models.Permission
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			markChecked(r)
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		p.ID = ""
		saved, err := s.share(r, p)
		if err != nil {
			markChecked(r)
			writeSharingError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, saved)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func writeSharingError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, errNotAllowed):
		writeError(w, r, http.StatusForbidden, err)
	case errors.Is(err, errNoSuchGrant):
		writeError(w, r, http.StatusNotFound, err)
	case errors.Is(err, errGrant):
		writeError(w, r, http.StatusBadRequest, err)
	default:
		writeError(w, r, http.StatusInternalServerError, err)
	}
}

// sharingFor is the sharing panel for one record, or nil for whoever may not
// read it. Its suggestions are the users, groups and provider groups known here.
func (s *Server) sharingFor(r *http.Request, typ, id, returnTo string) (*views.Sharing, error) {
	if id == "" || !s.can(r, authz.ActionRead, typ, id) {
		return nil, nil
	}
	ctx := r.Context()
	rows, err := s.store.ListPermissionsFor(ctx, typ, id)
	if err != nil {
		return nil, err
	}
	users, err := s.store.ListUsers(ctx, "", usersPageSize)
	if err != nil {
		return nil, err
	}
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{}
	var idpGroups []string
	for _, u := range users {
		labels["user:"+u.Subject] = displayName(u)
		for _, name := range u.IdPGroups {
			if !slices.Contains(idpGroups, name) {
				idpGroups = append(idpGroups, name)
			}
		}
	}
	for _, g := range groups {
		labels["group:"+g.ID] = g.Name
	}
	slices.Sort(idpGroups)

	sharing := &views.Sharing{
		ResourceType: typ, ResourceID: id, Return: returnTo,
		CanShare:    s.can(r, authz.ActionShare, typ, id),
		CanTransfer: s.can(r, authz.ActionTransfer, typ, id),
		Actions:     recordActions,
		Users:       users, Groups: groups, IdPGroups: idpGroups,
	}
	for _, p := range rows {
		label := p.PrincipalID
		if name, ok := labels[string(p.PrincipalType)+":"+p.PrincipalID]; ok {
			label = name
		}
		sharing.Permissions = append(sharing.Permissions, views.SharedPermission{Permission: p, Label: label})
	}
	return sharing, nil
}
