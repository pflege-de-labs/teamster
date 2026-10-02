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

var errLevel = errors.New("a webhook level is none, alertmanager, universal, all or admin, for a user, group, idp_group or role")

// webhookIDs are the resources a webhook grant names.
var webhookIDs = []string{authz.WebhookAlertmanager, authz.WebhookUniversal, authz.WebhooksAll}

// levelRows is the permission row a webhook level becomes.
func levelRows(level string) (string, []string, bool) {
	switch level {
	case "none":
		return "", nil, true
	case "alertmanager", "universal":
		return level, []string{authz.ActionUse}, true
	case "all":
		return authz.WebhooksAll, []string{authz.ActionUse}, true
	case "admin":
		return authz.WebhooksAll, []string{authz.ActionUse, authz.ActionAdminister}, true
	}
	return "", nil, false
}

// levelOf reads a principal's webhook rows back as the level the form offers.
func levelOf(rows []models.Permission) string {
	var am, uni, all, admin bool
	for _, p := range rows {
		switch {
		case p.ResourceID == authz.WebhooksAll && slices.Contains(p.Actions, authz.ActionAdminister):
			admin = true
		case p.ResourceID == authz.WebhooksAll:
			all = true
		case p.ResourceID == authz.WebhookAlertmanager:
			am = true
		case p.ResourceID == authz.WebhookUniversal:
			uni = true
		}
	}
	switch {
	case admin:
		return "admin"
	case all, am && uni:
		return "all"
	case am:
		return "alertmanager"
	case uni:
		return "universal"
	}
	return "none"
}

// setWebhookLevel replaces a principal's webhook rows with the level's one row.
func (s *Server) setWebhookLevel(r *http.Request, kind models.PrincipalType, principal, level string) error {
	principal = strings.TrimSpace(principal)
	id, actions, ok := levelRows(level)
	if !ok || !kind.Valid() || principal == "" {
		return userError{errLevel}
	}
	return s.store.WithTx(r.Context(), func(ctx context.Context, tx store.Store) error {
		for _, webhook := range webhookIDs {
			if _, err := tx.PutPermission(ctx, models.Permission{
				PrincipalType: kind, PrincipalID: principal, ResourceType: "Webhook", ResourceID: webhook,
			}); err != nil {
				return err
			}
		}
		if id == "" {
			return nil
		}
		_, err := tx.PutPermission(ctx, models.Permission{
			PrincipalType: kind, PrincipalID: principal, ResourceType: "Webhook", ResourceID: id,
			Actions: actions, CreatedBy: principalSubject(r),
		})
		return err
	})
}

func (s *Server) webhookLevelForm(r *http.Request) (string, error) {
	err := s.setWebhookLevel(r, models.PrincipalType(r.PostFormValue("principal_type")), r.PostFormValue("principal_id"), r.PostFormValue("level"))
	if err != nil {
		return "", err
	}
	return "Webhook permission saved.", nil
}

// handleWebhookLevelAPI is PUT /api/access/webhooks.
func (s *Server) handleWebhookLevelAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		PrincipalType string `json:"principal_type"`
		PrincipalID   string `json:"principal_id"`
		Level         string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	err := s.setWebhookLevel(r, models.PrincipalType(body.PrincipalType), body.PrincipalID, body.Level)
	switch {
	case errors.Is(err, errLevel):
		writeError(w, r, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, body)
	}
}

var errMessageLevel = errors.New("a message level is none or anyone, for a user, group, idp_group or role")

// messageGrantLevels are the levels an admin grants; self is everyone's (ADR 0082).
var messageGrantLevels = map[string][]string{
	"none":               nil,
	authz.MessagesAnyone: {authz.ActionMessage},
}

// messageLevelOf reads a principal's People row back as the level the form offers.
func messageLevelOf(p models.Permission) string {
	if slices.Contains(p.Actions, authz.ActionMessage) {
		return authz.MessagesAnyone
	}
	return "none"
}

// setMessageLevel replaces a principal's People row with the level's actions.
func (s *Server) setMessageLevel(r *http.Request, kind models.PrincipalType, principal, level string) error {
	principal = strings.TrimSpace(principal)
	actions, ok := messageGrantLevels[level]
	if !ok || !kind.Valid() || principal == "" {
		return userError{errMessageLevel}
	}
	_, err := s.store.PutPermission(r.Context(), models.Permission{
		PrincipalType: kind, PrincipalID: principal,
		ResourceType: authz.PeopleResource.Type, ResourceID: authz.PeopleResource.ID,
		Actions: actions, CreatedBy: principalSubject(r),
	})
	return err
}

func (s *Server) messageLevelForm(r *http.Request) (string, error) {
	err := s.setMessageLevel(r, models.PrincipalType(r.PostFormValue("principal_type")), r.PostFormValue("principal_id"), r.PostFormValue("level"))
	if err != nil {
		return "", err
	}
	return "Message permission saved.", nil
}

// handleMessageLevelAPI is PUT /api/access/messages.
func (s *Server) handleMessageLevelAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", http.MethodPut)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		PrincipalType string `json:"principal_type"`
		PrincipalID   string `json:"principal_id"`
		Level         string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	err := s.setMessageLevel(r, models.PrincipalType(body.PrincipalType), body.PrincipalID, body.Level)
	switch {
	case errors.Is(err, errMessageLevel):
		writeError(w, r, http.StatusBadRequest, err)
	case err != nil:
		writeError(w, r, http.StatusInternalServerError, err)
	default:
		writeJSON(w, http.StatusOK, body)
	}
}

// principalLabels names users and groups for the overviews.
func (s *Server) principalLabels(ctx context.Context) (map[string]string, error) {
	labels := map[string]string{}
	users, err := s.store.ListUsers(ctx, "", usersPageSize)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		labels["user:"+u.Subject] = displayName(u)
	}
	groups, err := s.store.ListGroups(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		labels["group:"+g.ID] = g.Name
	}
	return labels, nil
}

func labelOf(labels map[string]string, kind, id string) string {
	if name, ok := labels[kind+":"+id]; ok {
		return name + " (" + id + ")"
	}
	return id
}

func (s *Server) handleAccessPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	query := r.URL.Query()
	snapshot := s.policies(r)
	page := views.Access{
		Viewer:     s.viewerFor(r),
		Filter:     query.Get("filter"),
		BaseText:   authz.BaseText(),
		Generated:  snapshot.Policies(),
		Generation: snapshot.Generation(),
		Notice:     query.Get("notice"),
		Error:      query.Get("error"),
	}

	labels, err := s.principalLabels(ctx)
	if err != nil {
		page.Error = failureText(ctx, "load principals", err)
	}
	rows, err := s.store.ListPermissions(ctx)
	if err != nil && page.Error == "" {
		page.Error = failureText(ctx, "list permissions", err)
	}

	byPrincipal := map[[2]string][]models.Permission{}
	var order [][2]string
	for _, p := range rows {
		label := labelOf(labels, string(p.PrincipalType), p.PrincipalID)
		if p.ResourceType == "Webhook" {
			key := [2]string{string(p.PrincipalType), p.PrincipalID}
			if _, seen := byPrincipal[key]; !seen {
				order = append(order, key)
			}
			byPrincipal[key] = append(byPrincipal[key], p)
		}
		if p.ResourceType == authz.PeopleResource.Type {
			page.Messages = append(page.Messages, views.WebhookAccess{
				PrincipalType: string(p.PrincipalType), PrincipalID: p.PrincipalID, Label: label, Level: messageLevelOf(p),
			})
		}
		resource := p.ResourceType + " " + p.ResourceID
		if page.Filter != "" && !strings.Contains(strings.ToLower(label+" "+resource), strings.ToLower(page.Filter)) {
			continue
		}
		page.Grants = append(page.Grants, views.LabelledGrant{Permission: p, Principal: label, Resource: resource})
	}
	for _, key := range order {
		page.Webhooks = append(page.Webhooks, views.WebhookAccess{
			PrincipalType: key[0], PrincipalID: key[1], Label: labelOf(labels, key[0], key[1]), Level: levelOf(byPrincipal[key]),
		})
	}

	if subject := strings.TrimSpace(query.Get("subject")); subject != "" {
		page.Check = s.checkAccess(r, snapshot, subject, query.Get("action"), query.Get("type"), query.Get("id"))
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.AccessPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render access page", err)
	}
}

// checkAccess answers for a user as of their last sign-in.
func (s *Server) checkAccess(r *http.Request, snapshot *authz.Authorizer, subject, action, typ, id string) *views.AccessCheck {
	check := &views.AccessCheck{Subject: subject, Action: action, ResourceType: typ, ResourceID: id}
	principal := authz.Principal{Subject: subject}
	if user, err := s.store.GetUser(r.Context(), subject); err == nil {
		check.Known = true
		principal.Roles = authz.Decode(strings.Join(user.Roles, " "))
		principal.IdPGroups = user.IdPGroups
	}
	check.Result = snapshot.Explain(principal, action, authz.Resource{Type: typ, ID: id})
	return check
}

// handleMyAccess is /admin/me, for anyone signed in: it asks the engine
// itself, because it sits outside authorize.
func (s *Server) handleMyAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	snapshot, err := s.engine.Authorizer(ctx)
	if err != nil {
		logError(ctx, "authorization snapshot", err)
		http.Error(w, "authorization is unavailable, try again", http.StatusServiceUnavailable)
		return
	}
	p := principalFor(r)
	page := views.MyAccess{Viewer: s.viewerFor(r), Subject: p.Subject, IdPGroups: p.IdPGroups, Policies: snapshot.PoliciesFor(p)}
	for _, role := range p.Roles {
		page.Roles = append(page.Roles, string(role))
	}
	labels, err := s.principalLabels(ctx)
	if err != nil {
		page.Error = failureText(ctx, "load principals", err)
	}
	for _, group := range snapshot.GroupsOf(p) {
		page.Groups = append(page.Groups, labelOf(labels, "group", group))
	}
	for _, webhook := range []string{authz.WebhookAlertmanager, authz.WebhookUniversal} {
		if snapshot.AllowFor(p, authz.ActionUse, authz.WebhookResource(webhook)) {
			page.Webhooks = append(page.Webhooks, webhook)
		}
	}
	page.Messages = snapshot.MessageLevel(p)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.MyAccessPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render my access page", err)
	}
}
