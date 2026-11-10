package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
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

func TestDestinationListAppBadge(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	st.destinations["d1"] = models.Destination{ID: "d1", Name: "Ops alerts", TeamID: "t1", ChannelID: "c"}
	st.botTeams["t1"] = models.BotTeam{TeamID: "t1", ServiceURL: "https://smba.example/", UpdatedAt: time.Now()}
	msg := &fakeMessenger{teams: []graph.Team{{ID: "t1", Name: "Ops"}, {ID: "t2", Name: "Dev"}}}
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     config.BotConfig{ClientID: "bot-1"},
	}
	const badge = `href="/admin/teams" class="rounded bg-amber-100`

	installed := do(t, mustServer(t, cfg, st, msg).Handler, http.MethodGet, "/admin/destinations", "").Body.String()
	if strings.Contains(installed, badge) {
		t.Error("an installed team carries the app-missing badge")
	}

	st.destinations["d2"] = models.Destination{ID: "d2", Name: "Dev alerts", TeamID: "t2", ChannelID: "c"}
	missing := do(t, mustServer(t, cfg, st, msg).Handler, http.MethodGet, "/admin/destinations", "").Body.String()
	if !strings.Contains(missing, badge) || !strings.Contains(missing, "Teams app missing") {
		t.Error("a team without the app carries no badge linking to /admin/teams")
	}
}

func TestDestinationRecord(t *testing.T) {
	t.Parallel()

	const record = "/admin/destinations/dest"
	asDefault := func(role authz.Role) func() *fakeStore {
		return func() *fakeStore {
			st := sessionAs(seededUIStore(), role)
			d := st.destinations["dest"]
			d.IsDefault = true
			st.destinations["dest"] = d
			return st
		}
	}
	tests := []struct {
		name     string
		store    func() *fakeStore
		path     string
		want     []string
		unwanted []string
	}{
		{
			name:  "admin",
			store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) },
			path:  record,
			want: []string{
				"Ops channel", "team", "chan", "Install unknown", `href="?edit"`, `data-record-edit`, "Make global default",
				`action="/admin/destinations/delete"`, `data-mode="edit" hidden`,
			},
			unwanted: []string{`data-mode="view" hidden`, "global default</span>"},
		},
		{
			name:     "admin on the default",
			store:    asDefault(authz.RoleAdmin),
			path:     record,
			want:     []string{"global default</span>", `data-record-edit`, `action="/admin/destinations/delete"`},
			unwanted: []string{"Make global default"},
		},
		{
			name:     "admin editing",
			store:    func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) },
			path:     record + "?edit",
			want:     []string{`data-mode="view" hidden`, `name="channel_id"`, `data-record-cancel`},
			unwanted: []string{`data-mode="edit" hidden`},
		},
		{
			name:     "viewer",
			store:    func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleViewer) },
			path:     record + "?edit",
			want:     []string{"Ops channel"},
			unwanted: []string{"data-record-edit", `data-mode="edit"`, "Make global default", `/admin/destinations/delete`},
		},
		{
			name:     "editor",
			store:    func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleEditor) },
			path:     record,
			want:     []string{"data-record-edit", `action="/admin/destinations/delete"`},
			unwanted: []string{"Make global default"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
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

func TestDestinationRecordNotFound(t *testing.T) {
	t.Parallel()

	shared := func() *fakeStore {
		st := sharedStore()
		st.destinations["dest"] = models.Destination{ID: "dest", Name: "Shared channel", TeamID: "team", ChannelID: "c"}
		st.destinations["priv"] = models.Destination{ID: "priv", Name: "Private channel", TeamID: "team", ChannelID: "p"}
		grantTo(st, models.PrincipalUser, "tester", "Destination", "dest", "read")
		return st
	}
	tests := []struct {
		name  string
		store func() *fakeStore
		path  string
		want  int
	}{
		{"missing", func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) }, "/admin/destinations/nope", http.StatusNotFound},
		{"not shared", shared, "/admin/destinations/priv", http.StatusNotFound},
		{"delete is a post", func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) }, "/admin/destinations/delete", http.StatusNotFound},
		{"default is a post", func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) }, "/admin/destinations/default", http.StatusNotFound},
		{"shared", shared, "/admin/destinations/dest", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.want {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.want)
			}
			if tt.want == http.StatusOK && strings.Contains(rec.Body.String(), "Make global default") {
				t.Error("a share-only user is offered Make default")
			}
		})
	}
}

func TestDestinationPagesWithoutAccess(t *testing.T) {
	t.Parallel()

	h := newTestServer(t, sessionAs(seededUIStore()), &fakeMessenger{}).Handler
	for _, path := range []string{"/admin/destinations", "/admin/destinations/dest"} {
		rec := asRole(t, h, http.MethodGet, path, "")
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "access") {
			t.Errorf("GET %s = %d, want the 403 no-access page", path, rec.Code)
		}
	}
}

func TestDestinationRecordUsedBy(t *testing.T) {
	t.Parallel()

	build := func() *fakeStore {
		st := sharedStore()
		st.destinations["dest"] = models.Destination{ID: "dest", Name: "Shared channel", TeamID: "team", ChannelID: "c"}
		st.routes["r1"] = models.Route{ID: "r1", Name: "Readable route", DestinationID: "dest", LabelSelector: map[string]string{"a": "b"}}
		st.routes["r2"] = models.Route{ID: "r2", DestinationID: "dest", LabelSelector: map[string]string{"severity": "critical"}}
		st.routes["child"] = models.Route{ID: "child", Name: "Inheriting child", ParentID: "r1"}
		st.webhooks["w1"] = models.WebhookEndpoint{ID: "w1", TeamSlug: "ops", ChannelSlug: "alerts", DestinationID: "dest"}
		grantTo(st, models.PrincipalUser, "tester", "Destination", "dest", "read")
		grantTo(st, models.PrincipalUser, "tester", "Route", "r1", "read")
		return st
	}
	admin := build()
	admin.sessions[testSessionID] = sessionAs(newFakeStore(), authz.RoleAdmin).sessions[testSessionID]

	tests := []struct {
		name     string
		store    *fakeStore
		want     []string
		unwanted []string
	}{
		{
			name:     "admin",
			store:    admin,
			want:     []string{`href="/admin/routes?edit=r1"`, "Readable route", `href="/admin/routes?edit=r2"`, "severity=critical", `href="/admin/webhooks?edit=w1"`, "ops/alerts", `href="/admin/routing"`},
			unwanted: []string{"Inheriting child"},
		},
		{
			name:     "share only",
			store:    build(),
			want:     []string{"Readable route", `href="/admin/routing"`},
			unwanted: []string{"severity=critical", "ops/alerts", "Inheriting child"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store, &fakeMessenger{}).Handler, http.MethodGet, "/admin/destinations/dest", "")
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

func TestDestinationOldEditLink(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to string }{
		{"list", "/admin/destinations?edit=abc&notice=x", "/admin/destinations/abc?edit&notice=x"},
		{"tab", "/admin?edit=destinations&id=abc", "/admin/destinations/abc?edit"},
		{"tab escapes the id", "/admin?edit=destinations&id=a%2Fb&error=e", "/admin/destinations/a%2Fb?edit&error=e"},
		{"tab list", "/admin?tab=destinations", "/admin/destinations"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, sessionAs(seededUIStore(), authz.RoleAdmin), &fakeMessenger{}).Handler, http.MethodGet, tt.from, "")
			if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != tt.to {
				t.Errorf("GET %s = %d to %q, want 301 to %q", tt.from, rec.Code, rec.Header().Get("Location"), tt.to)
			}
		})
	}
}

func TestDestinationNewPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role authz.Role
		want int
		body []string
	}{
		{"editor", authz.RoleEditor, http.StatusOK, []string{`action="/admin/destinations"`, `href="/admin/destinations" data-record-cancel`, "New destination", `name="channel_id"`}},
		{"viewer", authz.RoleViewer, http.StatusForbidden, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, sessionAs(seededUIStore(), tt.role), &fakeMessenger{}).Handler, http.MethodGet, "/admin/destinations/new", "")
			if rec.Code != tt.want {
				t.Fatalf("GET = %d, want %d", rec.Code, tt.want)
			}
			for _, w := range tt.body {
				if !strings.Contains(rec.Body.String(), w) {
					t.Errorf("page is missing %q", w)
				}
			}
		})
	}
}

func TestDestinationSave(t *testing.T) {
	t.Parallel()

	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	tests := []struct {
		name     string
		form     url.Values
		headers  map[string]string
		want     int
		location string
		body     []string
	}{
		{
			name: "create", headers: same, want: http.StatusSeeOther,
			form:     url.Values{"name": {"New"}, "team_id": {"platform"}, "channel_id": {"incidents"}},
			location: "/admin/destinations/generated?notice=Destination+created.",
		},
		{
			name: "update", headers: same, want: http.StatusSeeOther,
			form:     url.Values{"id": {"inside"}, "name": {"Renamed"}, "team_id": {"platform"}, "channel_id": {"alerts"}},
			location: "/admin/destinations/inside?notice=Destination+updated.",
		},
		{
			name: "create refused", headers: same, want: http.StatusUnprocessableEntity,
			form: url.Values{"name": {"Sneaky"}, "team_id": {"payments"}, "channel_id": {"alerts"}},
			body: []string{"Sneaky", "not granted", `action="/admin/destinations"`, "New destination"},
		},
		{
			name: "update refused", headers: same, want: http.StatusUnprocessableEntity,
			form: url.Values{"id": {"inside"}, "name": {"Moved"}, "team_id": {"payments"}, "channel_id": {"alerts"}},
			body: []string{`value="Moved"`, "not granted", `data-mode="view" hidden`, `value="inside"`, "Platform alerts"},
		},
		{name: "cross-origin", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden, form: url.Values{"name": {"x"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := scopedStore(t, authz.RoleEditor)
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/destinations", tt.form, tt.headers)
			if rec.Code != tt.want {
				t.Fatalf("POST = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); tt.location != "" && got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			for _, w := range tt.body {
				if !strings.Contains(rec.Body.String(), w) {
					t.Errorf("page is missing %q", w)
				}
			}
		})
	}
}

func TestDestinationDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, id, location string
	}{
		{"success", "other", "/admin/destinations?notice=Destination+deleted."},
		{"refused", "dest", "/admin/destinations/dest?error="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := postForm(t, newTestServer(t, twoDestinations(), &fakeMessenger{}).Handler, "/admin/destinations/delete",
				url.Values{"id": {tt.id}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
			if got := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(got, tt.location) {
				t.Errorf("POST = %d to %q, want 303 to %q", rec.Code, got, tt.location)
			}
		})
	}
}

func TestDestinationMakeDefault(t *testing.T) {
	t.Parallel()

	rec := postForm(t, newTestServer(t, twoDestinations(), &fakeMessenger{}).Handler, "/admin/destinations/default",
		url.Values{"id": {"other"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if got, want := rec.Header().Get("Location"), "/admin/destinations/other?notice="; rec.Code != http.StatusSeeOther || !strings.HasPrefix(got, want) {
		t.Errorf("POST = %d to %q, want 303 to %q", rec.Code, got, want)
	}
}
