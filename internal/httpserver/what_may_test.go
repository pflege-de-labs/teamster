package httpserver

import (
	"context"
	"net/http/httptest"
	"slices"
	"testing"

	"golang.org/x/text/language"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

type wantRow struct {
	actions []string // compared when set
	via     []string
	direct  bool
	share   bool
}

func TestWhatMay(t *testing.T) {
	t.Parallel()

	ops := authz.Principal{Groups: []string{"g-ops"}}
	tests := []struct {
		name     string
		lang     language.Tag
		admin    bool
		seed     func(st *fakeStore)
		p        authz.Principal
		self     models.PrincipalType
		selfID   string
		rows     map[string]wantRow
		roles    bool
		webhooks string
		messages string
	}{
		{
			name: "group with a parent", lang: language.English, p: ops, self: models.PrincipalGroup, selfID: "g-ops",
			rows: map[string]wantRow{
				"Template:t1": {actions: []string{"read"}, via: []string{"direct"}, direct: true},
				"Template:t2": {actions: []string{"attach"}, via: []string{"group All"}},
			},
			webhooks: "alertmanager", messages: authz.MessagesEveryone,
		},
		{
			name: "admin viewer may share", lang: language.English, admin: true, p: ops, self: models.PrincipalGroup, selfID: "g-ops",
			rows: map[string]wantRow{
				"Template:t1": {via: []string{"direct"}, direct: true, share: true},
				"Template:t2": {via: []string{"group All"}, share: true},
			},
			webhooks: "alertmanager", messages: authz.MessagesEveryone,
		},
		{
			name: "german reasons", lang: language.German, p: ops, self: models.PrincipalGroup, selfID: "g-ops",
			rows: map[string]wantRow{
				"Template:t1": {via: []string{"direkt"}, direct: true},
				"Template:t2": {via: []string{"Gruppe All"}},
			},
			webhooks: "alertmanager", messages: authz.MessagesEveryone,
		},
		{
			name: "role principal has no record rows", lang: language.English,
			p: authz.Principal{Roles: []authz.Role{authz.RoleEditor}}, self: models.PrincipalRole, selfID: "editor",
			rows: map[string]wantRow{}, roles: true, webhooks: "all", messages: authz.MessagesSelf,
		},
		{
			name: "role and direct grant on one record", lang: language.English,
			seed: func(st *fakeStore) { grantTo(st, models.PrincipalUser, "u1", "Template", "t1", "read") },
			p:    authz.Principal{Subject: "u1", Roles: []authz.Role{authz.RoleEditor}}, self: models.PrincipalUser, selfID: "u1",
			rows:     map[string]wantRow{"Template:t1": {via: []string{"direct", "role editor", "role viewer"}, direct: true}},
			roles:    true,
			webhooks: "all", messages: authz.MessagesSelf,
		},
		{
			name: "provider group grant", lang: language.English,
			seed: func(st *fakeStore) { grantTo(st, models.PrincipalIdPGroup, "oncall", "Template", "t1", "read") },
			p:    authz.Principal{Subject: "u1", IdPGroups: []string{"oncall"}}, self: models.PrincipalUser, selfID: "u1",
			rows:     map[string]wantRow{"Template:t1": {actions: []string{"read"}, via: []string{"oncall"}}},
			webhooks: "none", messages: authz.MessagesSelf,
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
			if tt.seed != nil {
				tt.seed(st)
			} else {
				grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "read")
				grantTo(st, models.PrincipalGroup, "g-all", "Template", "t2", "attach")
				grantTo(st, models.PrincipalGroup, "g-all", "Webhook", authz.WebhookAlertmanager, "use")
				grantTo(st, models.PrincipalGroup, "g-all", "People", authz.PeopleResource.ID, "broadcast")
			}

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
			ctx := i18n.WithLanguage(r.Context(), bundle, tt.lang)
			if tt.admin {
				ctx = context.WithValue(ctx, roleKey, []authz.Role{authz.RoleAdmin})
			}
			got, err := srv.whatMay(r.WithContext(ctx), snapshot, tt.p, tt.self, tt.selfID)
			if err != nil {
				t.Fatal(err)
			}

			if len(got.Records) != len(tt.rows) {
				t.Fatalf("records = %+v, want keys of %v", got.Records, tt.rows)
			}
			for _, rec := range got.Records {
				key := rec.Type + ":" + rec.ID
				want, ok := tt.rows[key]
				if !ok {
					t.Errorf("unexpected row %s", key)
					continue
				}
				actions, via := slices.Clone(rec.Actions), slices.Clone(rec.Via)
				slices.Sort(actions)
				slices.Sort(via)
				if want.actions != nil && !slices.Equal(actions, want.actions) {
					t.Errorf("%s actions = %v, want %v", key, actions, want.actions)
				}
				if !slices.Equal(via, want.via) {
					t.Errorf("%s via = %v, want %v", key, via, want.via)
				}
				if (rec.Direct != nil) != want.direct {
					t.Errorf("%s direct = %v, want set %v", key, rec.Direct, want.direct)
				}
				if rec.Direct != nil && (rec.Direct.PrincipalType != tt.self || rec.Direct.PrincipalID != tt.selfID) {
					t.Errorf("%s direct = %+v, want the grant of %s:%s", key, rec.Direct, tt.self, tt.selfID)
				}
				if rec.CanShare != want.share {
					t.Errorf("%s CanShare = %v, want %v", key, rec.CanShare, want.share)
				}
			}
			if tt.roles {
				if i := slices.IndexFunc(got.Roles, func(rs views.RoleSummary) bool { return rs.Type == "Template" }); i < 0 ||
					!slices.Equal(got.Roles[i].Actions, []string{"read", "update", "delete", "attach", "create"}) {
					t.Errorf("roles = %+v, want Template with read update delete attach create", got.Roles)
				}
			} else if len(got.Roles) > 0 {
				t.Errorf("roles = %+v, want none", got.Roles)
			}
			if got.Webhooks != tt.webhooks || got.Messages != tt.messages {
				t.Errorf("webhooks, messages = %q, %q; want %q, %q", got.Webhooks, got.Messages, tt.webhooks, tt.messages)
			}
		})
	}
}
