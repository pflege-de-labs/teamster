package httpserver

import (
	"net/http/httptest"
	"slices"
	"testing"

	"golang.org/x/text/language"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestWhatMay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		p        authz.Principal
		self     models.PrincipalType
		selfID   string
		rows     map[string]string // "type:id" -> "actions|via"
		roles    map[string][]string
		webhooks string
		messages string
	}{
		{
			name: "group with a parent", p: authz.Principal{Groups: []string{"g-ops"}}, self: models.PrincipalGroup, selfID: "g-ops",
			rows:     map[string]string{"Template:t1": "read|direct", "Template:t2": "attach|group All (g-all)"},
			webhooks: "alertmanager", messages: authz.MessagesEveryone,
		},
		{
			name: "role", p: authz.Principal{Roles: []authz.Role{authz.RoleEditor}}, self: models.PrincipalRole, selfID: "editor",
			rows:     map[string]string{},
			roles:    map[string][]string{"Template": {"read", "update", "delete", "attach", "create"}},
			webhooks: "all", messages: authz.MessagesSelf,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			st.templates["t1"] = models.Template{ID: "t1", Name: "One", Body: "{}"}
			st.templates["t2"] = models.Template{ID: "t2", Name: "Two", Body: "{}"}
			st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
			st.groups["g-all"] = models.Group{ID: "g-all", Name: "All"}
			st.members = append(st.members, models.GroupMember{GroupID: "g-all", Type: models.MemberGroup, ID: "g-ops"})
			grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "read")
			grantTo(st, models.PrincipalGroup, "g-all", "Template", "t2", "attach")
			grantTo(st, models.PrincipalGroup, "g-all", "Webhook", authz.WebhookAlertmanager, "use")
			grantTo(st, models.PrincipalGroup, "g-all", "People", authz.PeopleResource.ID, "broadcast")

			var srv *Server
			if _, err := NewServer(quietLog, config.Config{}, st, &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), nil, func(s *Server) { srv = s }); err != nil {
				t.Fatal(err)
			}
			snapshot, err := srv.engine.Authorizer(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := i18n.New("", language.English)
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("GET", "/admin/access", nil)
			r = r.WithContext(i18n.WithLanguage(r.Context(), bundle, language.English))
			got, err := srv.whatMay(r, snapshot, tt.p, tt.self, tt.selfID)
			if err != nil {
				t.Fatal(err)
			}

			rows := map[string]string{}
			for _, rec := range got.Records {
				actions := slices.Clone(rec.Actions)
				slices.Sort(actions)
				rows[rec.Type+":"+rec.ID] = actions[0] + "|" + rec.Via[0]
				if len(rec.Actions) != 1 || len(rec.Via) != 1 {
					t.Errorf("%s:%s = %v via %v, want one action and one reason", rec.Type, rec.ID, rec.Actions, rec.Via)
				}
			}
			for key, want := range tt.rows {
				if rows[key] != want {
					t.Errorf("row %s = %q, want %q", key, rows[key], want)
				}
			}
			if len(rows) != len(tt.rows) {
				t.Errorf("rows = %v, want %v", rows, tt.rows)
			}
			for _, rs := range got.Roles {
				if want, ok := tt.roles[rs.Type]; ok && !slices.Equal(rs.Actions, want) {
					t.Errorf("role summary %s = %v, want %v", rs.Type, rs.Actions, want)
				}
			}
			if (len(got.Roles) > 0) != (len(tt.roles) > 0) {
				t.Errorf("roles = %+v, want %v", got.Roles, tt.roles)
			}
			if got.Webhooks != tt.webhooks || got.Messages != tt.messages {
				t.Errorf("webhooks, messages = %q, %q; want %q, %q", got.Webhooks, got.Messages, tt.webhooks, tt.messages)
			}
		})
	}
}
