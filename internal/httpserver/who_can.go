package httpserver

import (
	"net/http"
	"slices"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// checkUsersLimit caps how many users "Who can?" asks about.
const checkUsersLimit = 5000

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
	// One more than the limit tells a cut list from one that is exactly full.
	users, err := s.store.ListUsers(ctx, "", s.checkUsers+1)
	if err != nil {
		return views.WhoCan{}, err
	}
	labels, err := s.principalLabels(ctx)
	if err != nil {
		return views.WhoCan{}, err
	}
	kind, id, _ := strings.Cut(narrow, ":")
	out := views.WhoCan{Truncated: len(users) > s.checkUsers}
	users = users[:min(len(users), s.checkUsers)]
	for _, u := range users {
		if u.Disabled() {
			continue
		}
		p := authz.Principal{Subject: u.Subject, Roles: authz.Decode(strings.Join(u.Roles, " ")), IdPGroups: u.IdPGroups}
		switch {
		case narrow == "":
		case kind == string(models.PrincipalUser) && u.Subject == id:
		case kind == string(models.PrincipalGroup) && slices.Contains(snapshot.GroupsOf(p), id):
		case kind == string(models.PrincipalIdPGroup) && slices.Contains(u.IdPGroups, id):
		case kind == string(models.PrincipalRole) && slices.Contains(p.Roles, authz.Role(id)):
		default:
			continue
		}
		exp := snapshot.Explain(p, action, res)
		if !exp.Allowed {
			continue
		}
		via, _ := reasonsFor(ctx, labels, nil, exp, models.PrincipalUser, u.Subject)
		out.Allowed = append(out.Allowed, views.WhoRow{ID: u.Subject, Label: displayName(u), Via: via, Policies: exp.Policies})
	}
	return out, nil
}
