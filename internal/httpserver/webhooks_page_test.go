package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

var webhookSameOrigin = map[string]string{"Sec-Fetch-Site": "same-origin"}

func TestWebhookList(t *testing.T) {
	t.Parallel()

	withTemplate := func(role authz.Role) *fakeStore {
		st := sessionAs(webhookUIStore(), role)
		e := st.webhooks["hook"]
		e.TemplateID = "tmpl"
		st.webhooks["hook"] = e
		return st
	}
	tests := []struct {
		name     string
		store    func() *fakeStore
		want     []string
		unwanted []string
	}{
		{
			name:     "admin",
			store:    func() *fakeStore { return withTemplate(authz.RoleAdmin) },
			want:     []string{`href="/admin/webhooks/hook"`, "/teamsv2/platform/alerts", "Ops channel", "Critical card", `href="/admin/webhooks/new"`},
			unwanted: []string{`action="/admin/webhooks`, hashToken("old-token")},
		},
		{
			name:     "viewer",
			store:    func() *fakeStore { return withTemplate(authz.RoleViewer) },
			want:     []string{`href="/admin/webhooks/hook"`, "/teamsv2/platform/alerts"},
			unwanted: []string{"/admin/webhooks/new"},
		},
		{
			name: "share only",
			store: func() *fakeStore {
				st := sessionAs(webhookUIStore())
				st.webhooks["private"] = models.WebhookEndpoint{ID: "private", TeamSlug: "secret", ChannelSlug: "room", DestinationID: "dest"}
				grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "hook", "read")
				return st
			},
			want: []string{`href="/admin/webhooks/hook"`, "/teamsv2/platform/alerts"},
			// The destination is not readable, so its id stands in for its name.
			unwanted: []string{"secret", "Ops channel", "/admin/webhooks/new"},
		},
		{
			name:  "empty",
			store: func() *fakeStore { return sessionAs(newFakeStore(), authz.RoleAdmin) },
			want:  []string{"No Teams V2 webhooks yet", "New webhook"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/webhooks", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}
}

func TestWebhookNewPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     authz.Role
		wantCode int
		want     []string
	}{
		{"admin", authz.RoleAdmin, http.StatusOK, []string{`name="team_slug"`, `name="channel_slug"`, `name="destination_id"`, `name="template_id"`, "Ops channel", "Critical card", `action="/admin/webhooks"`}},
		{"editor", authz.RoleEditor, http.StatusOK, []string{`name="destination_id"`}},
		{"viewer", authz.RoleViewer, http.StatusForbidden, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, sessionAs(seededUIStore(), tt.role), &fakeMessenger{}).Handler, http.MethodGet, "/admin/webhooks/new", "")
			if rec.Code != tt.wantCode {
				t.Fatalf("GET = %d, want %d", rec.Code, tt.wantCode)
			}
			checkBody(t, rec.Body.String(), tt.want, nil)
		})
	}
}

func TestWebhookRecord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		store    func() *fakeStore
		path     string
		want     []string
		unwanted []string
	}{
		{
			name:  "admin",
			store: func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleAdmin) },
			path:  "/admin/webhooks/hook",
			want: []string{
				"/teamsv2/platform/alerts/…", `action="/admin/webhooks/rotate"`, "data-record-edit", `action="/admin/webhooks/delete"`,
				`name="team_slug"`, `value="platform"`, `id="sharing"`, `data-mode="edit" hidden`, `data-record="/admin/webhooks/hook"`,
			},
			unwanted: []string{`data-mode="view" hidden`, "data-record-replace", hashToken("old-token"), "old-token"},
		},
		{
			name:     "admin editing",
			store:    func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleAdmin) },
			path:     "/admin/webhooks/hook?edit",
			want:     []string{`data-mode="view" hidden`, `name="channel_slug"`, "data-record-cancel"},
			unwanted: []string{`data-mode="edit" hidden`},
		},
		{
			name:     "viewer",
			store:    func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleViewer) },
			path:     "/admin/webhooks/hook?edit",
			want:     []string{"/teamsv2/platform/alerts/…"},
			unwanted: []string{`/admin/webhooks/rotate`, "data-record-edit", `/admin/webhooks/delete`, `name="team_slug"`, `data-mode="edit"`},
		},
		{
			name: "share only with read",
			store: func() *fakeStore {
				st := sessionAs(webhookUIStore())
				grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "hook", "read")
				return st
			},
			path:     "/admin/webhooks/hook",
			want:     []string{"/teamsv2/platform/alerts/…"},
			unwanted: []string{`/admin/webhooks/rotate`, `/admin/webhooks/delete`, `name="team_slug"`},
		},
		{
			name: "share only with update",
			store: func() *fakeStore {
				st := sessionAs(webhookUIStore())
				grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "hook", "read", "update")
				return st
			},
			path:     "/admin/webhooks/hook",
			want:     []string{`/admin/webhooks/rotate`, `name="team_slug"`},
			unwanted: []string{`/admin/webhooks/delete`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}
}

func TestWebhookRecordNotFound(t *testing.T) {
	t.Parallel()

	scoped := func(role authz.Role) *fakeStore {
		st := scopedStore(t, role)
		st.webhooks["in"] = models.WebhookEndpoint{ID: "in", TeamSlug: "platform", ChannelSlug: "alerts", DestinationID: "inside"}
		st.webhooks["out"] = models.WebhookEndpoint{ID: "out", TeamSlug: "payments", ChannelSlug: "alerts", DestinationID: "outside"}
		return st
	}
	tests := []struct {
		name  string
		store func() *fakeStore
		path  string
		want  int
	}{
		{"missing", func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleAdmin) }, "/admin/webhooks/nope", http.StatusNotFound},
		{"no role and no grant is stopped before the page", func() *fakeStore { return sessionAs(webhookUIStore()) }, "/admin/webhooks/hook", http.StatusForbidden},
		{"granted another", func() *fakeStore {
			st := sessionAs(webhookUIStore())
			st.webhooks["other"] = models.WebhookEndpoint{ID: "other", TeamSlug: "x", ChannelSlug: "y", DestinationID: "dest"}
			grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "other", "read")
			return st
		}, "/admin/webhooks/hook", http.StatusNotFound},
		{"hidden by channel scope, viewer", func() *fakeStore { return scoped(authz.RoleViewer) }, "/admin/webhooks/out", http.StatusNotFound},
		{"hidden by channel scope, editor", func() *fakeStore { return scoped(authz.RoleEditor) }, "/admin/webhooks/out?edit", http.StatusNotFound},
		{"inside the scope, editor", func() *fakeStore { return scoped(authz.RoleEditor) }, "/admin/webhooks/in", http.StatusOK},
		{"delete is a POST-only path", func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleAdmin) }, "/admin/webhooks/delete", http.StatusNotFound},
		{"rotate is a POST-only path", func() *fakeStore { return sessionAs(webhookUIStore(), authz.RoleAdmin) }, "/admin/webhooks/rotate", http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.want {
				t.Errorf("GET %s = %d, want %d", tt.path, rec.Code, tt.want)
			}
		})
	}
}

func TestWebhookOldLinksRedirect(t *testing.T) {
	t.Parallel()

	tests := []struct{ path, want string }{
		{"/admin/webhooks?edit=hook", "/admin/webhooks/hook?edit"},
		{"/admin/webhooks?edit=hook&notice=Saved.", "/admin/webhooks/hook?edit&notice=Saved."},
		{"/admin/webhooks?edit=hook&error=x", "/admin/webhooks/hook?edit&error=x"},
		{"/admin?edit=webhooks&id=hook", "/admin/webhooks/hook?edit"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()

			rec := do(t, newTestServer(t, webhookUIStore(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != tt.want {
				t.Errorf("GET %s = %d %q, want 301 %q", tt.path, rec.Code, rec.Header().Get("Location"), tt.want)
			}
		})
	}
}

func TestWebhookCreateRevealsTheURLOnce(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	form := url.Values{"team_slug": {"platform"}, "channel_slug": {"alerts"}, "destination_id": {"dest"}}
	rec := postForm(t, h, "/admin/webhooks", form, webhookSameOrigin)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 with the record", rec.Code)
	}
	var id string
	for k := range st.webhooks {
		id = k
	}
	token := tokenFromPage(t, rec.Body.String())
	checkBody(t, rec.Body.String(),
		[]string{"Copy this URL now", "/teamsv2/platform/alerts/" + token, "data-record-replace", `data-record="/admin/webhooks/` + id + `"`}, nil)

	again := do(t, h, http.MethodGet, "/admin/webhooks/"+id, "").Body.String()
	checkBody(t, again, []string{"/teamsv2/platform/alerts/…"}, []string{token, "data-record-replace", "Copy this URL now"})
}

func TestWebhookUpdateRedirects(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	form := url.Values{"id": {"hook"}, "team_slug": {"platform"}, "channel_slug": {"pages"}, "destination_id": {"dest"}}
	rec := postForm(t, h, "/admin/webhooks", form, webhookSameOrigin)
	if want := "/admin/webhooks/hook?notice=Webhook+updated."; rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != want {
		t.Fatalf("POST = %d %q, want 303 %q", rec.Code, rec.Header().Get("Location"), want)
	}
	if got := st.webhooks["hook"].ChannelSlug; got != "pages" {
		t.Errorf("ChannelSlug = %q, want pages", got)
	}
}

func TestWebhookFailedSavesRerender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		store    func() *fakeStore
		form     url.Values
		wantCode int
		want     []string
		unwanted []string
	}{
		{
			name:     "create with a bad slug",
			store:    seededUIStore,
			form:     url.Values{"team_slug": {"a/b"}, "channel_slug": {"typed-chan"}, "destination_id": {"dest"}},
			wantCode: http.StatusUnprocessableEntity,
			want:     []string{"URL segment", `value="typed-chan"`, `name="destination_id"`},
			unwanted: []string{"data-record-replace", "Copy this URL now"},
		},
		{
			name:     "create that fails in the store",
			store:    func() *fakeStore { return seededUIStore().fail("CreateWebhookEndpoint") },
			form:     url.Values{"team_slug": {"platform"}, "channel_slug": {"typed-chan"}, "destination_id": {"dest"}},
			wantCode: http.StatusInternalServerError,
			want:     []string{`value="typed-chan"`},
		},
		{
			name:     "update with a bad slug",
			store:    webhookUIStore,
			form:     url.Values{"id": {"hook"}, "team_slug": {"platform"}, "channel_slug": {"bad/slug"}, "destination_id": {"dest"}},
			wantCode: http.StatusUnprocessableEntity,
			want:     []string{"URL segment", `value="bad/slug"`, `data-mode="view" hidden`, "/teamsv2/platform/alerts/…"},
			unwanted: []string{"data-record-replace", `data-mode="edit" hidden`},
		},
		{
			name:     "update that fails in the store",
			store:    func() *fakeStore { return webhookUIStore().fail("UpdateWebhookEndpoint") },
			form:     url.Values{"id": {"hook"}, "team_slug": {"platform"}, "channel_slug": {"pages"}, "destination_id": {"dest"}},
			wantCode: http.StatusInternalServerError,
			want:     []string{`value="pages"`, `data-mode="view" hidden`},
			unwanted: []string{"data-record-replace"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := postForm(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, "/admin/webhooks", tt.form, webhookSameOrigin)
			if rec.Code != tt.wantCode {
				t.Fatalf("POST = %d, want %d", rec.Code, tt.wantCode)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}
}

func TestWebhookSaveOfAnUnreadableIDWritesNothing(t *testing.T) {
	t.Parallel()

	st := sessionAs(webhookUIStore())
	grantTo(st, models.PrincipalUser, "tester", "Destination", "dest", "read")
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	form := url.Values{"id": {"hook"}, "team_slug": {"hijacked"}, "channel_slug": {"x"}, "destination_id": {"dest"}}
	if rec := postFormAs(t, h, "/admin/webhooks", form); rec.Code != http.StatusNotFound {
		t.Errorf("POST = %d, want 404", rec.Code)
	}
	if got := st.webhooks["hook"].TeamSlug; got != "platform" {
		t.Errorf("TeamSlug = %q, want it untouched", got)
	}
}

func TestWebhookRotate(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	h := newTestServer(t, st, &fakeMessenger{}).Handler
	rec := postForm(t, h, "/admin/webhooks/rotate", url.Values{"id": {"hook"}}, webhookSameOrigin)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200", rec.Code)
	}
	token := tokenFromPage(t, rec.Body.String())
	checkBody(t, rec.Body.String(), []string{"/teamsv2/platform/alerts/" + token, "data-record-replace", "Copy this URL now"}, nil)
	if st.webhooks["hook"].TokenHash != hashToken(token) {
		t.Error("the stored digest is not the shown token's")
	}

	failing := newTestServer(t, webhookUIStore().fail("RotateWebhookEndpointToken"), &fakeMessenger{}).Handler
	rec = postForm(t, failing, "/admin/webhooks/rotate", url.Values{"id": {"hook"}}, webhookSameOrigin)
	if loc := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/admin/webhooks/hook?error=") {
		t.Errorf("failed rotate = %d %q, want 303 back to the record with the error", rec.Code, loc)
	}
}

func TestWebhookDelete(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/webhooks/delete", url.Values{"id": {"hook"}}, webhookSameOrigin)
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(rec.Header().Get("Location"), "/admin/webhooks") ||
		strings.Contains(rec.Header().Get("Location"), "/admin/webhooks/") {
		t.Errorf("POST = %d %q, want 303 to the list", rec.Code, rec.Header().Get("Location"))
	}
	if _, ok := st.webhooks["hook"]; ok {
		t.Error("the endpoint is still there")
	}
}

func TestWebhookRecordLinksOnlyWhatIsReadable(t *testing.T) {
	t.Parallel()

	linked := func(withRoute bool) *fakeStore {
		st := linkedStore()
		e := st.webhooks["hook"]
		e.TemplateID = "tmpl"
		st.webhooks["hook"] = e
		if !withRoute {
			delete(st.routes, "wroute")
		}
		return st
	}
	tests := []struct {
		name     string
		store    func() *fakeStore
		want     []string
		unwanted []string
	}{
		{
			name:     "admin",
			store:    func() *fakeStore { return sessionAs(linked(true), authz.RoleAdmin) },
			want:     []string{`href="/admin/destinations/dest"`, "Ops channel", `href="/admin/templates/tmpl"`, "Critical card", `href="/admin/routes?edit=wroute"`},
			unwanted: nil,
		},
		{
			name:     "admin, no managed route",
			store:    func() *fakeStore { return sessionAs(linked(false), authz.RoleAdmin) },
			want:     []string{`href="/admin/destinations/dest"`},
			unwanted: []string{"/admin/routes?edit="},
		},
		{
			name: "only the endpoint readable",
			store: func() *fakeStore {
				st := sessionAs(linked(true))
				grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "hook", "read")
				return st
			},
			want:     []string{"dest", "tmpl"},
			unwanted: []string{"/admin/destinations/", "/admin/templates/", "/admin/routes?edit=", "Ops channel", "Critical card"},
		},
		{
			name: "route readable too",
			store: func() *fakeStore {
				st := sessionAs(linked(true))
				grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "hook", "read")
				grantTo(st, models.PrincipalUser, "tester", "Route", "wroute", "read")
				return st
			},
			want:     []string{`href="/admin/routes?edit=wroute"`},
			unwanted: []string{"/admin/destinations/"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/webhooks/hook", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}
}

// Every webhook POST either checks a record or is refused for a holder of a record grant alone.
func TestWebhookPostsAreCheckedAgainstTheRecord(t *testing.T) {
	t.Parallel()

	posts := map[string]url.Values{
		"/admin/webhooks":        {"id": {"hook"}, "team_slug": {"a"}, "channel_slug": {"b"}, "destination_id": {"dest"}},
		"/admin/webhooks/rotate": {"id": {"hook"}},
		"/admin/webhooks/delete": {"id": {"hook"}},
	}
	for path, form := range posts {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(webhookUIStore())
			grantTo(st, models.PrincipalUser, "tester", "WebhookEndpoint", "other", "read", "update", "delete")
			api, h := capturedServer(t, st)
			postFormAs(t, h, path, form)
			if n := api.unchecked.Load(); n != 0 {
				t.Errorf("%d deferred requests were answered without a record check", n)
			}
			if got, ok := st.webhooks["hook"]; !ok || got.TeamSlug != "platform" || got.TokenHash != hashToken("old-token") {
				t.Errorf("a session with no grant on the record changed it: %+v", got)
			}
		})
	}
}
