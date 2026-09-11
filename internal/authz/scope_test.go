package authz

import (
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestScopeFor(t *testing.T) {
	t.Parallel()

	grants := []models.Grant{
		{ID: "1", Role: "editor", TeamID: "platform"},
		{ID: "2", Role: "payments-editors", TeamID: "payments", ChannelID: "alerts"},
	}

	tests := []struct {
		name             string
		roles            []Role
		grants           []models.Grant
		wantUnrestricted bool
		wantChannels     int
		wantTeams        int
	}{
		{
			// Nothing narrows this installation, so nothing is narrowed: this is
			// what an upgrade looks like before an admin adds a grant.
			name: "no grants at all", roles: []Role{RoleEditor}, wantUnrestricted: true,
		},
		{
			name:  "a grant for another role does not narrow this one",
			roles: []Role{RoleViewer}, grants: grants, wantUnrestricted: true,
		},
		{
			name: "a grant naming the role narrows it", roles: []Role{RoleEditor}, grants: grants,
			wantChannels: 1, wantTeams: 1,
		},
		{
			// "the payments editors" is a role in the provider and a grant here.
			name: "a role of the deployment's own", roles: []Role{"payments-editors"}, grants: grants,
			wantChannels: 1, wantTeams: 1,
		},
		{
			name: "several roles collect several grants", roles: []Role{RoleEditor, "payments-editors"},
			grants: grants, wantChannels: 2, wantTeams: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope := ScopeFor(tt.roles, tt.grants)
			if scope.Unrestricted != tt.wantUnrestricted {
				t.Errorf("unrestricted = %v, want %v", scope.Unrestricted, tt.wantUnrestricted)
			}
			if len(scope.Channels) != tt.wantChannels {
				t.Errorf("channels = %v, want %d", scope.Channels, tt.wantChannels)
			}
			if len(scope.Teams) != tt.wantTeams {
				t.Errorf("teams = %v, want %d", scope.Teams, tt.wantTeams)
			}
		})
	}
}

func TestAllowScoped(t *testing.T) {
	t.Parallel()

	authorizer, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	teamGrant := []models.Grant{{Role: "editor", TeamID: "platform"}}
	channelGrant := []models.Grant{{Role: "editor", TeamID: "platform", ChannelID: "alerts"}}
	// A grant only narrows the roles it names, so the viewer cases need grants
	// of their own — with the editor's, a viewer would simply be unrestricted.
	viewerTeamGrant := []models.Grant{{Role: "viewer", TeamID: "platform"}}
	viewerChannelGrant := []models.Grant{{Role: "viewer", TeamID: "platform", ChannelID: "alerts"}}

	tests := []struct {
		name     string
		roles    []Role
		grants   []models.Grant
		action   string
		resource Resource
		want     bool
	}{
		{
			name: "ungranted, so unrestricted", roles: []Role{RoleEditor}, action: ActionDeliver,
			resource: ChannelResource("anything", "at-all"), want: true,
		},
		{
			// A grant on the Team reaches the channels in it, which is the point
			// of granting a Team rather than listing its channels.
			name: "a Team grant covers its channels", roles: []Role{RoleEditor}, grants: teamGrant,
			action: ActionDeliver, resource: ChannelResource("platform", "alerts"), want: true,
		},
		{
			name: "and covers no other Team's", roles: []Role{RoleEditor}, grants: teamGrant,
			action: ActionDeliver, resource: ChannelResource("payments", "alerts"),
		},
		{
			name: "a channel grant covers that channel", roles: []Role{RoleEditor}, grants: channelGrant,
			action: ActionDeliver, resource: ChannelResource("platform", "alerts"), want: true,
		},
		{
			// Narrower than the Team: the other channels of it are not granted.
			name: "a channel grant covers no sibling", roles: []Role{RoleEditor}, grants: channelGrant,
			action: ActionDeliver, resource: ChannelResource("platform", "incidents"),
		},
		{
			name: "a viewer sees a granted channel", roles: []Role{RoleViewer}, grants: viewerTeamGrant,
			action: ActionViewChannel, resource: ChannelResource("platform", "alerts"), want: true,
		},
		{
			name: "a viewer does not see an ungranted one", roles: []Role{RoleViewer}, grants: viewerTeamGrant,
			action: ActionViewChannel, resource: ChannelResource("payments", "alerts"),
		},
		{
			name: "a viewer may not deliver to what it sees", roles: []Role{RoleViewer}, grants: viewerTeamGrant,
			action: ActionDeliver, resource: ChannelResource("platform", "alerts"),
		},
		{
			// A grant only narrows the roles it names.
			name: "a grant for the editors leaves a viewer unrestricted", roles: []Role{RoleViewer},
			grants: teamGrant, action: ActionViewChannel, resource: ChannelResource("payments", "alerts"),
			want: true,
		},
		{
			name: "a channel grant makes its Team visible", roles: []Role{RoleViewer}, grants: viewerChannelGrant,
			action: ActionViewTeam, resource: TeamResource("platform"), want: true,
		},
		{
			name: "another Team is not", roles: []Role{RoleViewer}, grants: viewerChannelGrant,
			action: ActionViewTeam, resource: TeamResource("payments"),
		},
		{
			// An admin is never scoped: the blanket admin policy covers it.
			name: "an admin reaches everything", roles: []Role{RoleAdmin}, grants: channelGrant,
			action: ActionDeliver, resource: ChannelResource("payments", "alerts"), want: true,
		},
		{
			name: "a role no policy mentions reaches nothing", roles: []Role{"auditor"},
			action: ActionDeliver, resource: ChannelResource("platform", "alerts"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scope := ScopeFor(tt.roles, tt.grants)
			if got := authorizer.AllowScoped("subject", tt.roles, tt.action, tt.resource, scope); got != tt.want {
				t.Errorf("AllowScoped(%v, %q, %+v) = %v, want %v", tt.roles, tt.action, tt.resource, got, tt.want)
			}
		})
	}
}
