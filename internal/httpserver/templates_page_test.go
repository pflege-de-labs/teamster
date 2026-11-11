package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// checkBody asserts what a page has and what it must not have.
func checkBody(t *testing.T, body string, want, unwanted []string) {
	t.Helper()

	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("page is missing %q", w)
		}
	}
	for _, u := range unwanted {
		if strings.Contains(body, u) {
			t.Errorf("page has %q", u)
		}
	}
}

func TestTemplateList(t *testing.T) {
	t.Parallel()

	withDefault := func(role authz.Role) func() *fakeStore {
		return func() *fakeStore {
			st := sessionAs(seededUIStore(), role)
			st.templates["other"] = models.Template{ID: "other", Name: "Quiet card", Body: "{}"}
			st.sourceDefaults[models.SourceAlertmanager] = "tmpl"
			return st
		}
	}
	tests := []struct {
		name     string
		store    func() *fakeStore
		want     []string
		unwanted []string
	}{
		{
			name:     "admin",
			store:    withDefault(authz.RoleAdmin),
			want:     []string{`href="/admin/templates/tmpl"`, `href="/admin/templates/other"`, "Default for Alertmanager", `href="/admin/templates/new"`, "New template", `id="source-defaults"`},
			unwanted: []string{`action="/admin/templates"`, `href="/admin/templates?edit=`},
		},
		{
			name:     "viewer",
			store:    withDefault(authz.RoleViewer),
			want:     []string{`href="/admin/templates/tmpl"`, "Critical card"},
			unwanted: []string{"/admin/templates/new", `action="/admin/templates"`},
		},
		{
			name:     "share only",
			store:    sharedStore,
			want:     []string{"Shared card", `href="/admin/templates/t1"`},
			unwanted: []string{"Private card", "/admin/templates/new", `href="/admin/templates/t2"`},
		},
		{
			name:  "empty",
			store: func() *fakeStore { return sessionAs(newFakeStore(), authz.RoleAdmin) },
			want:  []string{"No templates yet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}
}

func TestTemplateListDefaultBadge(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	st.templates["other"] = models.Template{ID: "other", Name: "Quiet card", Body: "{}"}
	st.sourceDefaults[models.SourceAlertmanager] = "tmpl"
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates", "").Body.String()

	const badge = `>Default for Alertmanager</span>`
	// The badge is a row's own: it appears once, on the row that is the default.
	rowOf := func(name string) string {
		start := strings.Index(body, ">"+name+"</a>")
		if start < 0 {
			t.Fatalf("no row for %q", name)
		}
		end := strings.Index(body[start:], "</li>")
		return body[start : start+end]
	}
	if !strings.Contains(rowOf("Critical card"), badge) {
		t.Error("the default template's row carries no badge")
	}
	if strings.Contains(rowOf("Quiet card"), "Default for") {
		t.Error("a template that is no default carries a badge")
	}
}

func TestTemplateListOldEditLink(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to string }{
		{"list", "/admin/templates?edit=abc&notice=x", "/admin/templates/abc?edit&notice=x"},
		{"list with an error", "/admin/templates?edit=abc&error=e", "/admin/templates/abc?edit&error=e"},
		{"tab", "/admin?edit=templates&id=abc", "/admin/templates/abc?edit"},
		{"tab escapes the id", "/admin?edit=templates&id=a%2Fb&error=e", "/admin/templates/a%2Fb?edit&error=e"},
		{"tab list", "/admin?tab=templates", "/admin/templates"},
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

func TestTemplateNewPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		role authz.Role
		want int
		body []string
	}{
		{"editor", authz.RoleEditor, http.StatusOK, []string{
			`action="/admin/templates"`, `href="/admin/templates" data-record-cancel`, "New template", "Save template",
			`id="template-preset"`, `class="snippet`, `id="card-starter"`, `name="sources" value="alertmanager"`,
			`id="preview-button"`, `id="preview-live"`, `id="preview-sample"`, `id="preview-views"`, `id="preview-output"`,
		}},
		{"viewer", authz.RoleViewer, http.StatusForbidden, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, sessionAs(seededUIStore(), tt.role), &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates/new", "")
			if rec.Code != tt.want {
				t.Fatalf("GET = %d, want %d", rec.Code, tt.want)
			}
			checkBody(t, rec.Body.String(), tt.body, nil)
			if tt.want == http.StatusOK && strings.Contains(rec.Body.String(), "data-preview-auto") {
				t.Error("an empty form is previewed on load")
			}
		})
	}
}

func TestTemplateRecord(t *testing.T) {
	t.Parallel()

	const record = "/admin/templates/tmpl"
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
				"Critical card", `href="/admin/templates/tmpl?edit"`, "data-record-edit", `action="/admin/templates/delete"`,
				`<form method="post" action="/admin/templates" id="template-form"`, `name="id" value="tmpl"`, `data-mode="edit" hidden`,
				`id="sharing"`, "data-preview-auto", `id="preview-button"`, `id="preview-output"`,
			},
			unwanted: []string{`data-mode="view" hidden`, `<form id="template-form" hidden>`},
		},
		{
			name:  "admin editing",
			store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) },
			path:  record + "?edit",
			want:  []string{`data-mode="view" hidden`, `name="id" value="tmpl"`, `value="Critical card"`, "Update template", "data-record-cancel"},
			unwanted: []string{
				`data-mode="edit" hidden`, `<form id="template-form" hidden>`,
			},
		},
		{
			name:  "viewer",
			store: func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleViewer) },
			path:  record + "?edit",
			want:  []string{"Critical card", `<form id="template-form" hidden>`, `name="body" value="{&#34;type&#34;:&#34;AdaptiveCard&#34;}"`, "data-preview-auto"},
			unwanted: []string{
				"data-record-edit", `data-mode="edit"`, `action="/admin/templates/delete"`, `action="/admin/templates"`, `name="sources"`,
			},
		},
		{
			name:     "editor",
			store:    func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleEditor) },
			path:     record,
			want:     []string{"data-record-edit", `action="/admin/templates/delete"`, `action="/admin/templates"`},
			unwanted: []string{`<form id="template-form" hidden>`},
		},
		{
			name:     "share with update but not delete",
			store:    sharedStore,
			path:     "/admin/templates/t1",
			want:     []string{"Shared card", "data-record-edit", `name="id" value="t1"`},
			unwanted: []string{`action="/admin/templates/delete"`},
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

func TestTemplateRecordNotFound(t *testing.T) {
	t.Parallel()

	shared := func() *fakeStore {
		st := sharedStore()
		grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read")
		return st
	}
	admin := func() *fakeStore { return sessionAs(seededUIStore(), authz.RoleAdmin) }
	tests := []struct {
		name  string
		store func() *fakeStore
		path  string
		want  int
	}{
		{"missing", admin, "/admin/templates/nope", http.StatusNotFound},
		{"not shared", shared, "/admin/templates/t2", http.StatusNotFound},
		{"not shared, editing", shared, "/admin/templates/t2?edit", http.StatusNotFound},
		{"delete is a post", admin, "/admin/templates/delete", http.StatusNotFound},
		{"shared", shared, "/admin/templates/t1", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store(), &fakeMessenger{}).Handler, http.MethodGet, tt.path, "")
			if rec.Code != tt.want {
				t.Fatalf("GET %s = %d, want %d", tt.path, rec.Code, tt.want)
			}
			if tt.want == http.StatusNotFound && strings.Contains(rec.Body.String(), "Private card") {
				t.Error("404 body leaks the template")
			}
		})
	}
}

func TestTemplateRecordUsedBy(t *testing.T) {
	t.Parallel()

	build := func() *fakeStore {
		st := sharedStore()
		st.destinations["dest"] = models.Destination{ID: "dest", Name: "Ops", TeamID: "team", ChannelID: "c"}
		st.routes["r1"] = models.Route{ID: "r1", Name: "Readable route", TemplateID: "t1", DestinationID: "dest", LabelSelector: map[string]string{"a": "b"}}
		st.routes["r2"] = models.Route{ID: "r2", TemplateID: "t1", DestinationID: "dest", LabelSelector: map[string]string{"severity": "critical"}}
		st.routes["r3"] = models.Route{ID: "r3", Name: "Other template", TemplateID: "t2", DestinationID: "dest"}
		st.webhooks["w1"] = models.WebhookEndpoint{ID: "w1", TeamSlug: "ops", ChannelSlug: "alerts", DestinationID: "dest", TemplateID: "t1"}
		st.webhooks["w2"] = models.WebhookEndpoint{ID: "w2", TeamSlug: "dev", ChannelSlug: "noise", DestinationID: "dest", TemplateID: "t2"}
		grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read")
		grantTo(st, models.PrincipalUser, "tester", "Route", "r1", "read")
		return st
	}
	admin := build()
	admin.sessions[testSessionID] = sessionAs(newFakeStore(), authz.RoleAdmin).sessions[testSessionID]
	defaults := build()
	defaults.sourceDefaults[models.SourceAlertmanager] = "t1"
	defaults.globalDefaultTemplate = "t1"

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
			unwanted: []string{"Other template", "dev/noise", "Default for", "Global default"},
		},
		{
			name:     "share only",
			store:    build(),
			want:     []string{"Readable route", `href="/admin/routing"`},
			unwanted: []string{"severity=critical", "ops/alerts", "Other template", "dev/noise", "No route, webhook or default uses"},
		},
		{
			name:  "source and global default",
			store: defaults,
			want:  []string{`href="/admin/templates#source-defaults"`, "Default for Alertmanager", `href="/admin/routes"`, "Global default"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, newTestServer(t, tt.store, &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates/t1", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET = %d, want 200", rec.Code)
			}
			checkBody(t, rec.Body.String(), tt.want, tt.unwanted)
		})
	}

	t.Run("unused", func(t *testing.T) {
		t.Parallel()

		body := asRole(t, newTestServer(t, sessionAs(seededUIStore(), authz.RoleAdmin), &fakeMessenger{}).Handler, http.MethodGet, "/admin/templates/tmpl", "").Body.String()
		checkBody(t, body, []string{`href="/admin/routes?edit=route"`}, []string{"No route, webhook or default uses"})
	})
}

func TestTemplateSave(t *testing.T) {
	t.Parallel()

	same := map[string]string{"Sec-Fetch-Site": "same-origin"}
	tests := []struct {
		name     string
		form     url.Values
		headers  map[string]string
		want     int
		location string
		body     []string
		unwanted []string
	}{
		{
			name: "create", headers: same, want: http.StatusSeeOther,
			form:     url.Values{"name": {"New card"}, "body": {"{}"}},
			location: "/admin/templates/generated?notice=Template+created.",
		},
		{
			name: "update", headers: same, want: http.StatusSeeOther,
			form:     url.Values{"id": {"tmpl"}, "name": {"Renamed"}, "body": {"{}"}},
			location: "/admin/templates/tmpl?notice=Template+updated.",
		},
		{
			name: "create without any content", headers: same, want: http.StatusUnprocessableEntity,
			form:     url.Values{"name": {"Typed name"}, "sources": {models.SourceAlertmanager}},
			body:     []string{"a template needs a title, text or a card", `value="Typed name"`, `action="/admin/templates"`, "New template", "Save template", `id="preview-button"`},
			unwanted: []string{`name="id"`},
		},
		{
			name: "create with an unknown source", headers: same, want: http.StatusUnprocessableEntity,
			form: url.Values{"name": {"Typed name"}, "body": {"{}"}, "sources": {"email"}},
			body: []string{`value="Typed name"`, "New template"},
		},
		{
			name: "update without any content", headers: same, want: http.StatusUnprocessableEntity,
			form: url.Values{"id": {"tmpl"}, "name": {"Typed name"}},
			body: []string{
				"a template needs a title, text or a card", `value="Typed name"`, `name="id" value="tmpl"`, `data-mode="view" hidden`,
				"Update template", "Critical card",
			},
			unwanted: []string{`data-mode="edit" hidden`},
		},
		{name: "cross-origin", headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden, form: url.Values{"name": {"x"}, "body": {"{}"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededUIStore(), authz.RoleAdmin)
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/templates", tt.form, tt.headers)
			if rec.Code != tt.want {
				t.Fatalf("POST = %d, want %d (%s)", rec.Code, tt.want, rec.Body.String())
			}
			if got := rec.Header().Get("Location"); tt.location != "" && got != tt.location {
				t.Errorf("Location = %q, want %q", got, tt.location)
			}
			checkBody(t, rec.Body.String(), tt.body, tt.unwanted)
			if tt.want != http.StatusSeeOther {
				if got := st.templates["tmpl"]; got.Name != "Critical card" || len(st.templates) != 1 {
					t.Errorf("a refused save wrote: %d templates, tmpl = %+v", len(st.templates), got)
				}
			}
		})
	}
}

func TestTemplateSaveKeepsWhatWasTyped(t *testing.T) {
	t.Parallel()

	st := sessionAs(seededUIStore(), authz.RoleAdmin)
	rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/templates", url.Values{
		"id": {"tmpl"}, "name": {"Typed name"}, "title": {""}, "message_text": {""}, "body": {""}, "sources": {models.SourceAlertmanager},
	}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("POST = %d, want 422", rec.Code)
	}
	checkBody(t, rec.Body.String(), []string{`name="sources" value="alertmanager" checked`}, nil)
}

func TestTemplateSaveFailurePaths(t *testing.T) {
	t.Parallel()

	form := func(id string) url.Values {
		return url.Values{"id": {id}, "name": {"Typed"}, "body": {"{}"}}
	}
	tests := []struct {
		name     string
		store    *fakeStore
		form     url.Values
		want     int
		body     []string
		unwanted []string
	}{
		{name: "unreadable id", store: sharedStore(), form: form("t2"), want: http.StatusNotFound, unwanted: []string{"Private card"}},
		{name: "missing id", store: sharedStore(), form: form("nope"), want: http.StatusNotFound},
		{
			name: "update refused after the read", store: func() *fakeStore {
				st := sessionAs(newFakeStore())
				st.templates["t1"] = models.Template{ID: "t1", Name: "Shared card", Body: "{}"}
				grantTo(st, models.PrincipalUser, "tester", "Template", "t1", "read")
				return st
			}(), form: form("t1"), want: http.StatusUnprocessableEntity,
			body: []string{"Shared card"}, unwanted: []string{`name="sources"`, "data-record-edit"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rec := sessionPost(t, newTestServer(t, tt.store, &fakeMessenger{}).Handler, "/admin/templates", tt.form)
			if rec.Code != tt.want {
				t.Fatalf("POST = %d, want %d", rec.Code, tt.want)
			}
			checkBody(t, rec.Body.String(), tt.body, tt.unwanted)
			if got := tt.store.templates["t2"].Name; got != "" && got != "Private card" {
				t.Errorf("the other template was written: name = %q", got)
			}
			if got := tt.store.templates["t1"].Name; got != "Shared card" {
				t.Errorf("the template was written: name = %q", got)
			}
		})
	}
}

func TestTemplateSaveUnexpectedFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		failOn string
		form   url.Values
		body   []string
	}{
		{"create", "CreateTemplate", url.Values{"name": {"Typed"}, "body": {"{}"}}, []string{`value="Typed"`, "New template"}},
		{"update", "UpdateTemplate", url.Values{"id": {"tmpl"}, "name": {"Typed"}, "body": {"{}"}}, []string{`value="Typed"`, `data-mode="view" hidden`, `name="id" value="tmpl"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededUIStore(), authz.RoleAdmin)
			st.fail(tt.failOn)
			rec := sessionPost(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/templates", tt.form)
			if rec.Code != http.StatusInternalServerError || rec.Header().Get("Location") != "" {
				t.Fatalf("POST = %d, want a 500 re-render (%s)", rec.Code, rec.Body.String())
			}
			checkBody(t, rec.Body.String(), append(tt.body, "Something went wrong. Reference: "), []string{errStore.Error()})
		})
	}
}

func TestTemplateDelete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, id, failOn, location string
	}{
		{"success", "tmpl", "", "/admin/templates?notice=Template+deleted."},
		{"refused", "tmpl", "DeleteTemplate", "/admin/templates/tmpl?error="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededUIStore(), authz.RoleAdmin)
			if tt.failOn != "" {
				st.fail(tt.failOn)
			}
			rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/templates/delete",
				url.Values{"id": {tt.id}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
			if got := rec.Header().Get("Location"); rec.Code != http.StatusSeeOther || !strings.HasPrefix(got, tt.location) {
				t.Errorf("POST = %d to %q, want 303 to %q", rec.Code, got, tt.location)
			}
			if _, kept := st.templates["tmpl"]; kept != (tt.failOn != "") {
				t.Errorf("template kept = %v after %s", kept, tt.name)
			}
		})
	}
}
