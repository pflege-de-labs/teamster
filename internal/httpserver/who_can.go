package httpserver

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// resourceActions are the actions of the resources that are not records.
var resourceActions = map[string][]string{
	"Webhook":                 {authz.ActionUse, authz.ActionAdminister},
	authz.PeopleResource.Type: {authz.ActionMessage, authz.ActionBroadcast},
}

func actionsOf(typ string) []string {
	if a, ok := resourceActions[typ]; ok {
		return a
	}
	return recordActions
}

// whoActions is every action the form offers; access-check.js hides those the resource lacks.
func whoActions() []string {
	return slices.Concat(recordActions, resourceActions["Webhook"], resourceActions[authz.PeopleResource.Type])
}

// whoCan asks the authorizer about every signed-in, enabled user as of their last sign-in.
func (s *Server) whoCan(r *http.Request, snapshot *authz.Authorizer, action string, res authz.Resource, narrow string) (views.WhoCan, error) {
	if !slices.Contains(actionsOf(res.Type), action) {
		return views.WhoCan{WrongAction: true}, nil
	}
	ctx := r.Context()
	labels, err := s.principalLabels(ctx)
	if err != nil {
		return views.WhoCan{}, err
	}
	var out views.WhoCan
	canRead := func(id string) bool { return s.can(r, authz.ActionRead, typeGroup, id) }
	// Keyset paging, as the store caps one page; a short page is the last.
	for after := ""; ; {
		users, err := s.store.ListUsersPage(ctx, after, s.whoPageSize)
		if err != nil {
			return views.WhoCan{}, err
		}
		for _, u := range users {
			if err := ctx.Err(); err != nil {
				return views.WhoCan{}, err
			}
			if row, ok := s.whoRow(ctx, snapshot, canRead, labels, u, action, res, narrow); ok {
				out.Allowed = append(out.Allowed, row)
			}
		}
		if len(users) < s.whoPageSize {
			break
		}
		after = users[len(users)-1].Subject
	}
	return out, nil
}

func (s *Server) whoRow(ctx context.Context, snapshot *authz.Authorizer, canRead func(string) bool, labels map[string]string, u models.User, action string, res authz.Resource, narrow string) (views.WhoRow, bool) {
	if u.Disabled() {
		return views.WhoRow{}, false
	}
	kind, id, _ := strings.Cut(narrow, ":")
	p := authz.Principal{Subject: u.Subject, Roles: authz.Decode(strings.Join(u.Roles, " ")), IdPGroups: u.IdPGroups}
	switch {
	case narrow == "":
	case kind == string(models.PrincipalUser) && u.Subject == id:
	case kind == string(models.PrincipalGroup) && slices.Contains(snapshot.GroupsOf(p), id):
	case kind == string(models.PrincipalIdPGroup) && slices.Contains(u.IdPGroups, id):
	case kind == string(models.PrincipalRole) && slices.Contains(p.Roles, authz.Role(id)):
	default:
		return views.WhoRow{}, false
	}
	exp := snapshot.Explain(p, action, res)
	if !exp.Allowed {
		return views.WhoRow{}, false
	}
	via, _ := reasonsFor(ctx, labels, canRead, nil, exp, models.PrincipalUser, u.Subject)
	return views.WhoRow{ID: u.Subject, Label: displayName(u), Via: via, Policies: exp.Policies}, true
}
