package httpserver

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// whoPanel is the answer of the "who" tab and nothing else on the page: the pickers name every user too.
func whoPanel(body string) string {
	_, after, _ := strings.Cut(body, `data-who-answer`)
	answer, _, _ := strings.Cut(after, `</section>`)
	return answer
}

func TestWhoCan(t *testing.T) {
	t.Parallel()

	newStore := func() *fakeStore {
		st := sessionAs(newFakeStore(), authz.RoleAdmin)
		st.users["s-ed"] = models.User{Subject: "s-ed", Name: "Edith", Roles: []string{"editor"}}
		st.users["s-bob"] = models.User{Subject: "s-bob", Name: "Bob", Roles: []string{"viewer"}}
		st.users["s-off"] = models.User{Subject: "s-off", Name: "Offa", Roles: []string{"editor"}, DisabledAt: time.Now()}
		st.groups["g-ops"] = models.Group{ID: "g-ops", Name: "Ops"}
		st.members = []models.GroupMember{{GroupID: "g-ops", Type: models.MemberUser, ID: "s-bob"}}
		st.templates["t1"] = models.Template{ID: "t1", Name: "Card one", Body: `{"type":"AdaptiveCard"}`}
		grantTo(st, models.PrincipalGroup, "g-ops", "Template", "t1", "update")
		return st
	}
	const ask = "?tab=who&action=update&resource=Template:t1"

	tests := []struct {
		name     string
		query    string
		pageSize int // users asked per page; the store holds more than one page
		want     []string
		unwanted []string
	}{
		{name: "roles and groups", query: ask, want: []string{"Allowed (", "Edith", "role editor", "Bob", "group Ops"}, unwanted: []string{"Offa"}},
		{name: "narrowed to a group", query: ask + "&principal=group:g-ops", want: []string{"Bob"}, unwanted: []string{"Edith"}},
		{name: "narrowed to a role", query: ask + "&principal=role:editor", want: []string{"Edith"}, unwanted: []string{"Bob"}},
		{name: "wrong action", query: "?tab=who&action=broadcast&resource=Webhook:universal", want: []string{"broadcast is not an action on this resource."}, unwanted: []string{"Edith"}},
		{name: "wrong action by its label", query: "?tab=who&action=use&resource=People:*", want: []string{"send to is not an action on this resource."}},
		{name: "unknown action as typed", query: "?tab=who&action=frobnicate&resource=Template:t1", want: []string{"frobnicate is not an action on this resource."}},
		{name: "more users than a page", query: ask, pageSize: 1, want: []string{"Allowed (2)", "Edith", "Bob"}, unwanted: []string{"Offa"}},
		{name: "old subject link", query: "?subject=s-bob&action=update&resource=Template:t1", want: []string{"Bob"}, unwanted: []string{"Edith"}},
		{name: "nobody", query: "?tab=who&action=delete&resource=Template:t1&principal=user:s-bob", want: []string{"Nobody signed in may."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var opts []Option
			if tt.pageSize > 0 {
				opts = append(opts, func(s *Server) { s.whoPageSize = tt.pageSize })
			}
			srv, err := NewServer(quietLog, config.Config{
				Server:  config.ServerConfig{Addr: ":0"},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
			}, newStore(), &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), nil, opts...)
			if err != nil {
				t.Fatal(err)
			}
			rec := call(t, srv.Handler, http.MethodGet, "/admin/access"+tt.query, "")
			panel := whoPanel(rec.Body.String())
			for _, want := range tt.want {
				if !strings.Contains(panel, want) {
					t.Errorf("answer lacks %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(panel, unwanted) {
					t.Errorf("answer shows %q", unwanted)
				}
			}
		})
	}
}
