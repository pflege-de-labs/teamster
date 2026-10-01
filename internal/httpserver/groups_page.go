package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

const maxGroupNameLength = 100

var (
	errGroupName      = errors.New("a group needs a name of at most 100 characters")
	errGroupNameTaken = errors.New("a group with that name already exists")
	errMember         = errors.New("a member needs a type of user, group or idp_group and an id")
	errNoSuchGroup    = errors.New("no such group, or no such member")
)

// groupError maps the store's refusals onto what the caller can fix.
func groupError(err error) error {
	switch {
	case errors.Is(err, store.ErrConflict):
		return userError{errGroupNameTaken}
	case errors.Is(err, store.ErrGroupCycle):
		return userError{err}
	case errors.Is(err, store.ErrNotFound):
		return userError{errNoSuchGroup}
	}
	return err
}

func (s *Server) saveGroup(r *http.Request, g models.Group) (models.Group, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" || utf8.RuneCountInString(g.Name) > maxGroupNameLength {
		return models.Group{}, userError{errGroupName}
	}
	if g.ID == "" {
		g.CreatedBy = principalSubject(r)
		created, err := s.store.CreateGroup(r.Context(), g)
		return created, groupError(err)
	}
	updated, err := s.store.UpdateGroup(r.Context(), g)
	return updated, groupError(err)
}

func memberFrom(groupID, kind, id string, by string) (models.GroupMember, error) {
	m := models.GroupMember{GroupID: groupID, Type: models.MemberType(kind), ID: strings.TrimSpace(id), AddedBy: by}
	if !m.Type.Valid() || m.ID == "" || groupID == "" {
		return models.GroupMember{}, userError{errMember}
	}
	return m, nil
}

func (s *Server) handleGroupsPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	page := views.Groups{
		Viewer:  s.viewerFor(r),
		CanEdit: s.allow(r, authz.ActionEdit, authz.Resource{Type: "Group"}),
		Notice:  query.Get("notice"),
		Error:   query.Get("error"),
	}
	groups, err := s.store.ListGroups(ctx)
	if err != nil && page.Error == "" {
		page.Error = failureText(ctx, "list groups", err)
	}
	page.Groups = groups

	if id := query.Get("id"); id != "" {
		detail, err := s.groupDetail(r, id, groups)
		switch {
		case isNotFound(err):
			page.Error = "No such group."
		case err != nil && page.Error == "":
			page.Error = failureText(ctx, "load group", err)
		case err == nil:
			page.Selected = &detail
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.GroupsPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render groups page", err)
	}
}

// groupDetail labels each member and gathers the picker's suggestions.
func (s *Server) groupDetail(r *http.Request, id string, groups []models.Group) (views.GroupDetail, error) {
	ctx := r.Context()
	group, err := s.store.GetGroup(ctx, id)
	if err != nil {
		return views.GroupDetail{}, err
	}
	members, err := s.store.ListGroupMembers(ctx, id)
	if err != nil {
		return views.GroupDetail{}, err
	}
	users, err := s.store.ListUsers(ctx, "", usersPageSize)
	if err != nil {
		return views.GroupDetail{}, err
	}

	names := map[string]string{}
	for _, g := range groups {
		names["group:"+g.ID] = g.Name
	}
	var idpGroups []string
	for _, u := range users {
		names["user:"+u.Subject] = displayName(u)
		for _, name := range u.IdPGroups {
			if !slices.Contains(idpGroups, name) {
				idpGroups = append(idpGroups, name)
			}
		}
	}
	slices.Sort(idpGroups)

	detail := views.GroupDetail{Group: group, Users: users, IdPGroups: idpGroups}
	for _, g := range groups {
		if g.ID != id {
			detail.Others = append(detail.Others, g)
		}
	}
	for _, m := range members {
		label := m.ID
		if name, ok := names[string(m.Type)+":"+m.ID]; ok {
			label = name
		}
		detail.Members = append(detail.Members, views.GroupMemberView{Member: m, Label: label})
	}
	return detail, nil
}

func displayName(u models.User) string {
	if u.Name != "" {
		return u.Name
	}
	return u.Subject
}

// Form posts land back on the group they changed.

func (s *Server) handleGroupSave(w http.ResponseWriter, r *http.Request) {
	if !formGuard("/admin/groups", w, r) {
		return
	}
	saved, err := s.saveGroup(r, models.Group{
		ID: r.PostFormValue("id"), Name: r.PostFormValue("name"), Description: strings.TrimSpace(r.PostFormValue("description")),
	})
	target := "/admin/groups"
	if id := r.PostFormValue("id"); id != "" {
		target += "?id=" + urlQueryEscape(id)
	}
	if err != nil {
		redirectTo(target, w, r, "", visibleError(r.Context(), "save group", err))
		return
	}
	redirectTo("/admin/groups?id="+urlQueryEscape(saved.ID), w, r, "Group saved.", "")
}

func (s *Server) deleteGroupForm(r *http.Request) (string, error) {
	if err := s.store.DeleteGroup(r.Context(), r.PostFormValue("id")); err != nil {
		return "", groupError(err)
	}
	return "Group deleted.", nil
}

func (s *Server) handleGroupMemberForm(add bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		target := "/admin/groups?id=" + urlQueryEscape(r.FormValue("group_id"))
		if !formGuard(target, w, r) {
			return
		}
		m, err := memberFrom(r.PostFormValue("group_id"), r.PostFormValue("type"), r.PostFormValue("member"), principalSubject(r))
		if err == nil {
			if add {
				err = s.store.AddGroupMember(r.Context(), m)
			} else {
				err = s.store.RemoveGroupMember(r.Context(), m)
			}
			err = groupError(err)
		}
		if err != nil {
			redirectTo(target, w, r, "", visibleError(r.Context(), "change group members", err))
			return
		}
		notice := "Member removed."
		if add {
			notice = "Member added."
		}
		redirectTo(target, w, r, notice, "")
	}
}

// The JSON API: /api/groups, /api/groups/{id} and /api/groups/{id}/members.

type groupResponse struct {
	models.Group
	Members []models.GroupMember `json:"members"`
}

func (s *Server) handleGroupsAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		groups, err := s.store.ListGroups(r.Context())
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		if groups == nil {
			groups = []models.Group{}
		}
		writeJSON(w, http.StatusOK, groups)
	case http.MethodPost:
		var g models.Group
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		g.ID = ""
		created, err := s.saveGroup(r, g)
		writeGroupResult(w, r, http.StatusCreated, created, err)
	default:
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGroupByID(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/groups/")
	id, sub, _ := strings.Cut(rest, "/")
	if id == "" || (sub != "" && sub != "members") {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if sub == "members" {
		s.handleGroupMembersAPI(w, r, id)
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		group, err := s.store.GetGroup(ctx, id)
		if err != nil {
			writeGroupResult(w, r, http.StatusOK, group, groupError(err))
			return
		}
		members, err := s.store.ListGroupMembers(ctx, id)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, err)
			return
		}
		if members == nil {
			members = []models.GroupMember{}
		}
		writeJSON(w, http.StatusOK, groupResponse{Group: group, Members: members})
	case http.MethodPut:
		var g models.Group
		if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		g.ID = id
		updated, err := s.saveGroup(r, g)
		writeGroupResult(w, r, http.StatusOK, updated, err)
	case http.MethodDelete:
		if err := groupError(s.store.DeleteGroup(ctx, id)); err != nil {
			writeGroupResult(w, r, http.StatusOK, models.Group{}, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGroupMembersAPI(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "POST, DELETE")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	m, err := memberFrom(id, body.Type, body.ID, principalSubject(r))
	if err == nil {
		if r.Method == http.MethodPost {
			err = s.store.AddGroupMember(r.Context(), m)
		} else {
			err = s.store.RemoveGroupMember(r.Context(), m)
		}
		err = groupError(err)
	}
	if err != nil {
		writeGroupResult(w, r, http.StatusOK, models.Group{}, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func writeGroupResult(w http.ResponseWriter, r *http.Request, status int, g models.Group, err error) {
	var fixable userError
	switch {
	case err == nil:
		writeJSON(w, status, g)
	case errors.Is(err, errGroupNameTaken), errors.Is(err, store.ErrGroupCycle):
		writeError(w, r, http.StatusConflict, err)
	case errors.Is(err, errNoSuchGroup):
		writeError(w, r, http.StatusNotFound, err)
	case errors.As(err, &fixable):
		writeError(w, r, http.StatusBadRequest, err)
	default:
		writeError(w, r, http.StatusInternalServerError, err)
	}
}
