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

	for _, class := range index {
		for _, rec := range class.Records {
			row := views.MayRow{Type: class.Type, ID: rec.ID, Name: rec.Name, Href: rec.Href, CanShare: rec.CanShare}
			resource := authz.Resource{Type: class.Type, ID: rec.ID}
			granted := false
			for _, action := range recordActions {
				if !snapshot.AllowFor(p, action, resource) {
					continue
				}
				row.Actions = append(row.Actions, action)
				for _, policy := range snapshot.Explain(p, action, resource).Policies {
					var via string
					switch {
					case policy.Grant != nil:
						granted = true
						via = grantReason(ctx, labels, *policy.Grant, self, selfID)
					case policy.Role != "":
						via = i18n.T(ctx, "access.via_role", policy.Role)
					default:
						continue
					}
					if !slices.Contains(row.Via, via) {
						row.Via = append(row.Via, via)
					}
				}
			}
			// A record only a base role reaches is the role's, shown in Roles.
			if !granted {
				continue
			}
			if perm, ok := own[class.Type+":"+rec.ID]; ok {
				row.Direct = &perm
			}
			out.Records = append(out.Records, row)
		}
	}
	out.Webhooks = webhookLevel(snapshot, p)
	return out, nil
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
		return g.PrincipalID
	case kind == models.PrincipalRole:
		return i18n.T(ctx, "access.via_role", g.PrincipalID)
	case kind == models.PrincipalUser:
		return labelOf(labels, "user", g.PrincipalID)
	}
	return g.PrincipalID
}

// webhookLevel asks the authorizer for the rows levelOf reads, so the level
// follows groups and roles too.
func webhookLevel(snapshot *authz.Authorizer, p authz.Principal) string {
	var rows []models.Permission
	for _, id := range webhookIDs {
		res := authz.WebhookResource(id)
		var actions []string
		for _, action := range []string{authz.ActionUse, authz.ActionAdminister} {
			if snapshot.AllowFor(p, action, res) {
				actions = append(actions, action)
			}
		}
		if len(actions) > 0 {
			rows = append(rows, models.Permission{ResourceID: id, Actions: actions})
		}
	}
	return levelOf(rows)
}
