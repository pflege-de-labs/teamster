package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// The catch-all renders with its configured template, not the payload's own
// content (ADR 0050).
func TestTheGlobalDefaultRendersWithItsTemplate(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)
	delete(st.routes, "route")
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel", IsDefault: true}
	st.templates["catch"] = models.Template{ID: "catch", Title: "Caught {{ .Event.Title }}", Body: "{}"}
	st.globalDefaultTemplate = "catch"

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"team":"nobody"},"title":"Unclaimed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "Caught Unclaimed" {
		t.Fatalf("posts = %+v, want one rendered with the catch-all template", msg.posts)
	}
}

func TestTheGlobalDefaultTemplateAPI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		role       authz.Role
		method     string
		body       string
		wantStatus int
		wantStored string
	}{
		{name: "read", role: authz.RoleViewer, method: http.MethodGet, wantStatus: http.StatusOK, wantStored: "tmpl"},
		{name: "set", role: authz.RoleEditor, method: http.MethodPut, body: `{"template_id":"other"}`, wantStatus: http.StatusOK, wantStored: "other"},
		{name: "back to the built-in", role: authz.RoleEditor, method: http.MethodPut, body: `{"template_id":""}`, wantStatus: http.StatusOK},
		{name: "an unknown template", role: authz.RoleEditor, method: http.MethodPut, body: `{"template_id":"gone"}`, wantStatus: http.StatusBadRequest, wantStored: "tmpl"},
		{name: "a viewer may not change it", role: authz.RoleViewer, method: http.MethodPut, body: `{"template_id":"other"}`, wantStatus: http.StatusForbidden, wantStored: "tmpl"},
		{name: "invalid JSON", role: authz.RoleEditor, method: http.MethodPut, body: `{`, wantStatus: http.StatusBadRequest, wantStored: "tmpl"},
		{name: "wrong method", role: authz.RoleEditor, method: http.MethodPost, body: `{}`, wantStatus: http.StatusMethodNotAllowed, wantStored: "tmpl"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(seededStore(), tt.role)
			st.templates["other"] = models.Template{ID: "other", Name: "Other", Body: "{}"}
			st.globalDefaultTemplate = "tmpl"
			handler := newTestServer(t, st, &fakeMessenger{}).Handler

			rec := asRole(t, handler, tt.method, "/api/routes/global-default", tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("%s = %d, want %d (%s)", tt.method, rec.Code, tt.wantStatus, rec.Body.String())
			}
			if st.globalDefaultTemplate != tt.wantStored {
				t.Errorf("stored = %q, want %q", st.globalDefaultTemplate, tt.wantStored)
			}
			if tt.name == "read" && !strings.Contains(rec.Body.String(), `"template_id":"tmpl"`) {
				t.Errorf("body = %s, want the stored template", rec.Body.String())
			}
		})
	}
}

func TestTheGlobalDefaultTemplateForm(t *testing.T) {
	t.Parallel()

	st := seededStore()
	st.destinations["dest"] = models.Destination{ID: "dest", Name: "Ops", TeamID: "team", ChannelID: "channel", IsDefault: true}
	st.templates["tmpl"] = models.Template{ID: "tmpl", Name: "Critical card", Body: "{}"}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	page := do(t, handler, http.MethodGet, "/admin", "").Body.String()
	for _, want := range []string{`action="/admin/routes/global-default"`, "built-in default message"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %s", want)
		}
	}

	rec := postForm(t, handler, "/admin/routes/global-default", url.Values{"template_id": {"tmpl"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303", rec.Code)
	}
	if st.globalDefaultTemplate != "tmpl" {
		t.Errorf("stored = %q, want tmpl", st.globalDefaultTemplate)
	}
	if page := do(t, handler, http.MethodGet, "/admin", "").Body.String(); !strings.Contains(page, `<option value="tmpl" selected>`) {
		t.Error("page does not show the chosen template selected")
	}

	rec = postForm(t, handler, "/admin/routes/global-default", url.Values{"template_id": {"gone"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if location := rec.Header().Get("Location"); !strings.Contains(location, "does+not+exist") {
		t.Errorf("Location = %q, want the unknown template named", location)
	}
}
