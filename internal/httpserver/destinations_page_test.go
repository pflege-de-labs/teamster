package httpserver

import (
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestDestinationList(t *testing.T) {
	t.Parallel()

	withDefault := func() *fakeStore {
		st := seededUIStore()
		d := st.destinations["dest"]
		d.IsDefault = true
		st.destinations["dest"] = d
		st.destinations["other"] = models.Destination{ID: "other", Name: "Escalation", TeamID: "team2", ChannelID: "esc"}
		return st
	}
	tests := []struct {
		name     string
		store    func() *fakeStore
		want     []string
		unwanted []string
	}{
		{
			name:  "admin",
			store: func() *fakeStore { return sessionAs(withDefault(), authz.RoleAdmin) },
			want: []string{
				`href="/admin/destinations/dest"`, `href="/admin/destinations/other"`, "global default",
				`href="/admin/destinations/new"`, "New destination",
			},
			unwanted: []string{`action="/admin/destinations`},
		},
		{
			name:     "viewer",
			store:    func() *fakeStore { return sessionAs(withDefault(), authz.RoleViewer) },
			want:     []string{`href="/admin/destinations/dest"`, "Ops channel"},
			unwanted: []string{"/admin/destinations/new", `action="/admin/destinations`},
		},
		{
			name: "share only",
			store: func() *fakeStore {
				st := sharedStore()
				st.destinations["dest"] = models.Destination{ID: "dest", Name: "Shared channel", TeamID: "team", ChannelID: "c"}
				st.destinations["priv"] = models.Destination{ID: "priv", Name: "Private channel", TeamID: "team", ChannelID: "p"}
				grantTo(st, models.PrincipalUser, "tester", "Destination", "dest", "read")
				return st
			},
			want:     []string{"Shared channel"},
			unwanted: []string{"Private channel", "/admin/destinations/new"},
		},
		{
			name:  "empty",
			store: func() *fakeStore { st := sessionAs(newFakeStore(), authz.RoleAdmin); return st },
			want:  []string{"No destinations yet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/destinations", "")
			body := rec.Body.String()
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			for _, w := range tt.want {
				if !strings.Contains(body, w) {
					t.Errorf("page is missing %q", w)
				}
			}
			for _, u := range tt.unwanted {
				if strings.Contains(body, u) {
					t.Errorf("page has %q", u)
				}
			}
		})
	}
}
