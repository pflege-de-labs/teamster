package httpserver

import (
	"context"
	"net/http"
	"slices"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// whatMay asks the authorizer what p may do, so the answer holds groups, roles and
// hierarchy the grant rows alone do not show. self is the principal the page is about.
func (s *Server) whatMay(r *http.Request, snapshot *authz.Authorizer, p authz.Principal, self models.PrincipalType, selfID string) (views.WhatMay, error) {
	ctx := r.Context()
	labels, err := s.principalLabels(ctx)
	if err != nil {
		return views.WhatMay{}, err
	}
	index, err := s.recordIndex(ctx, r)
	if err != nil {
		return views.WhatMay{}, err
	}
	all, err := s.store.ListPermissions(ctx)
	if err != nil {
		return views.WhatMay{}, err
	}
	own := map[string]models.Permission{}
	for _, perm := range all {
		if perm.PrincipalType == self && perm.PrincipalID == selfID {
			own[perm.ResourceType+":"+perm.ResourceID] = perm
		}
	}
	out := views.WhatMay{Roles: roleSummaries(snapshot, p.Roles), Messages: snapshot.MessageLevel(p)}
	if action, ok := authz.MessageAction(out.Messages); ok {
		out.MessagesVia, _ = reasonsFor(ctx, labels, nil, snapshot.Explain(p, action, authz.PeopleResource), self, selfID)
	}

	for _, class := range index {
		for _, rec := range class.Records {
			// The index names records the viewer may not read; the answer must not.
			if !s.can(r, authz.ActionRead, class.Type, rec.ID) {
				continue
			}
			row := views.MayRow{Type: class.Type, ID: rec.ID, Name: rec.Name, Href: rec.Href, CanShare: rec.CanShare}
			resource := authz.Resource{Type: class.Type, ID: rec.ID}
			granted := false
			for _, action := range recordActions {
				if !snapshot.AllowFor(p, action, resource) {
					continue
				}
				row.Actions = append(row.Actions, action)
				var g bool
				row.Via, g = reasonsFor(ctx, labels, row.Via, snapshot.Explain(p, action, resource), self, selfID)
				granted = granted || g
			}
			// A record only a base role reaches is the role's, shown in Roles.
			if !granted {
				continue
			}
			row.CanTransfer = s.can(r, authz.ActionTransfer, class.Type, rec.ID)
			if perm, ok := own[class.Type+":"+rec.ID]; ok {
				row.Direct = &perm
			}
			out.Records = append(out.Records, row)
		}
	}
	out.Webhooks, out.WebhooksVia = webhookLevel(ctx, labels, snapshot, p, self, selfID)
	return out, nil
}

// reasonsFor adds the reasons an explanation holds to via; granted says a grant, not just a role, decided.
func reasonsFor(ctx context.Context, labels map[string]string, via []string, exp authz.Explanation, self models.PrincipalType, selfID string) ([]string, bool) {
	granted := false
	for _, policy := range exp.Policies {
		var reason string
		switch {
		case policy.Grant != nil:
			granted = true
			reason = grantReason(ctx, labels, *policy.Grant, self, selfID)
		case policy.Role != "":
			reason = i18n.T(ctx, "access.via_role", policy.Role)
		default:
			continue
		}
		if !slices.Contains(via, reason) {
			via = append(via, reason)
		}
	}
	return via, granted
}

func grantReason(ctx context.Context, labels map[string]string, g authz.Grant, self models.PrincipalType, selfID string) string {
	kind := models.PrincipalType(g.PrincipalType)
	switch {
	case kind == self && g.PrincipalID == selfID:
		return i18n.T(ctx, "access.via_direct")
	case kind == models.PrincipalGroup:
		name := g.PrincipalID
		if label, ok := labels["group:"+name]; ok {
			name = label
		}
		return i18n.T(ctx, "access.via_group", name)
	case kind == models.PrincipalIdPGroup:
		return i18n.T(ctx, "access.via_idp_group", g.PrincipalID)
	case kind == models.PrincipalRole:
		return i18n.T(ctx, "access.via_role", g.PrincipalID)
	case kind == models.PrincipalUser:
		return labelOf(labels, "user", g.PrincipalID)
	}
	return g.PrincipalID
}

// webhookLevel asks the authorizer for the rows levelOf reads, so the level
// follows groups and roles too, and gathers the reasons for it.
func webhookLevel(ctx context.Context, labels map[string]string, snapshot *authz.Authorizer, p authz.Principal, self models.PrincipalType, selfID string) (string, []string) {
	var rows []models.Permission
	var via []string
	for _, id := range webhookIDs {
		res := authz.WebhookResource(id)
		var actions []string
		for _, action := range []string{authz.ActionUse, authz.ActionAdminister} {
			exp := snapshot.Explain(p, action, res)
			if exp.Allowed {
				actions = append(actions, action)
				via, _ = reasonsFor(ctx, labels, via, exp, self, selfID)
			}
		}
		if len(actions) > 0 {
			rows = append(rows, models.Permission{ResourceID: id, Actions: actions})
		}
	}
	return levelOf(rows), via
}
