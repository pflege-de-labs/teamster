package httpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// Between a route's template and the built-in message sits the source's
// default (ADR 0055).
func TestRoutedMessagesFallBackToTheSourceDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		routeTemplate *models.Template
		defaultTitle  string
		defaultSource []string
		wantTitle     string
		wantNotice    bool
	}{
		{name: "no template, no default", wantTitle: "Own title", wantNotice: true},
		{name: "no template, a default", defaultTitle: "from default", wantTitle: "from default"},
		{name: "a default that stopped handling the source", defaultTitle: "from default",
			defaultSource: []string{models.SourceAlertmanager}, wantTitle: "Own title", wantNotice: true},
		{name: "the route's own template wins", routeTemplate: &models.Template{ID: "route", Title: "from route"},
			defaultTitle: "from default", wantTitle: "from route"},
		{name: "a route template for another source", routeTemplate: &models.Template{ID: "route", Title: "from route", Sources: []string{models.SourceAlertmanager}},
			defaultTitle: "from default", wantTitle: "from default"},
		{name: "a route template for another source, no default", routeTemplate: &models.Template{ID: "route", Title: "from route", Sources: []string{models.SourceAlertmanager}},
			wantTitle: "Own title"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "graph-1"}
			st, handler := seededServer(t, msg)
			if tt.routeTemplate != nil {
				st.templates[tt.routeTemplate.ID] = *tt.routeTemplate
				for id, route := range st.routes {
					route.TemplateID = tt.routeTemplate.ID
					st.routes[id] = route
				}
			} else {
				for id, route := range st.routes {
					route.TemplateID = ""
					st.routes[id] = route
				}
			}
			if tt.defaultTitle != "" {
				st.templates["default"] = models.Template{ID: "default", Title: tt.defaultTitle}
				st.sourceDefaults[models.SourceUniversal] = "default"
				if tt.defaultSource != nil {
					// Written directly: the store would refuse it, but a template
					// edited after it became a default ends up just like this.
					st.templates["default"] = models.Template{ID: "default", Title: tt.defaultTitle, Sources: tt.defaultSource}
				}
			}

			rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"a":"b"},"title":"Own title"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST = %d, want 200 (%s)", rec.Code, rec.Body.String())
			}
			if len(msg.posts) != 1 {
				t.Fatalf("posts = %d, want 1", len(msg.posts))
			}
			got := msg.posts[0].msg
			if got.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tt.wantTitle)
			}
			if hasNotice := strings.Contains(string(joinCards(got.Cards)), "No template is defined"); hasNotice != tt.wantNotice {
				t.Errorf("notice = %v, want %v (%s)", hasNotice, tt.wantNotice, joinCards(got.Cards))
			}
		})
	}
}

func joinCards(cards []json.RawMessage) []byte {
	var out []byte
	for _, card := range cards {
		out = append(out, card...)
	}
	return out
}

func TestTeamsV2EndpointsFallBackToTheTeamsV2Default(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		endpoint   *models.Template
		wantTitle  string
		wantNotice bool
	}{
		{name: "no endpoint template", wantTitle: "from default"},
		{name: "an endpoint template for another source", endpoint: &models.Template{ID: "tmpl", Title: "x", Sources: []string{models.SourceAlertmanager}},
			wantTitle: "from default"},
		{name: "the endpoint's own template wins", endpoint: &models.Template{ID: "tmpl", Title: "from endpoint"}, wantTitle: "from endpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "posted"}
			st, handler := teamsV2Server(t, msg)
			if tt.endpoint != nil {
				st.templates[tt.endpoint.ID] = *tt.endpoint
				hook := st.webhooks["hook"]
				hook.TemplateID = tt.endpoint.ID
				st.webhooks["hook"] = hook
			}
			st.templates["default"] = models.Template{ID: "default", Title: "from default", Sources: []string{models.SourceTeamsV2}}
			st.sourceDefaults[models.SourceTeamsV2] = "default"

			rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, `{"@type":"MessageCard","title":"Build failed","text":"x"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
			}
			if len(msg.posts) != 1 || msg.posts[0].msg.Title != tt.wantTitle {
				t.Fatalf("posts = %+v, want title %q", msg.posts, tt.wantTitle)
			}
			if strings.Contains(string(joinCards(msg.posts[0].msg.Cards)), "No template is defined") {
				t.Error("a message rendered by the default carries the no-template notice")
			}
		})
	}
}

func TestASourceDefaultReadFailureFailsTheDelivery(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)
	for id, route := range st.routes {
		route.TemplateID = ""
		st.routes[id] = route
	}
	st.failOn = map[string]bool{"GetSourceDefaultTemplate": true}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"a":"b"},"title":"Own title"}`)
	if rec.Code == http.StatusOK || len(msg.posts) != 0 {
		t.Errorf("POST = %d with %d posts, want the delivery to fail", rec.Code, len(msg.posts))
	}
}

func TestTheSourceDefaultsAPI(t *testing.T) {
	t.Parallel()

	st := seededStore()
	st.templates["am"] = models.Template{ID: "am", Name: "AM", Body: "{}", Sources: []string{models.SourceAlertmanager}}
	st.templates["any"] = models.Template{ID: "any", Name: "Any", Body: "{}"}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{name: "valid", body: `{"templates":{"alertmanager":"am","universal":"any"}}`, wantStatus: http.StatusOK},
		{name: "unknown source", body: `{"templates":{"email":"any"}}`, wantStatus: http.StatusBadRequest},
		{name: "unknown template", body: `{"templates":{"universal":"gone"}}`, wantStatus: http.StatusBadRequest},
		{name: "another source's template", body: `{"templates":{"teamsv2":"am"}}`, wantStatus: http.StatusBadRequest},
		{name: "not JSON", body: `{`, wantStatus: http.StatusBadRequest},
	}
	for _, tt := range tests {
		if rec := do(t, handler, http.MethodPut, "/api/templates/source-defaults", tt.body); rec.Code != tt.wantStatus {
			t.Errorf("%s: PUT = %d, want %d (%s)", tt.name, rec.Code, tt.wantStatus, rec.Body.String())
		}
	}

	rec := do(t, handler, http.MethodGet, "/api/templates/source-defaults", "")
	var got sourceDefaultTemplates
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("GET body: %v", err)
	}
	if len(got.Templates) != 2 || got.Templates[models.SourceAlertmanager] != "am" || got.Templates[models.SourceUniversal] != "any" {
		t.Errorf("GET = %+v, want the valid PUT and nothing from the refused ones", got.Templates)
	}
	if rec := do(t, handler, http.MethodDelete, "/api/templates/source-defaults", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE = %d, want 405", rec.Code)
	}

	st.failOn = map[string]bool{"SourceDefaultTemplates": true}
	if rec := do(t, handler, http.MethodGet, "/api/templates/source-defaults", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("GET failing = %d, want 500", rec.Code)
	}
}

func TestTheSourceDefaultsForm(t *testing.T) {
	t.Parallel()

	st := seededStore()
	st.templates["am"] = models.Template{ID: "am", Name: "AM preset", Body: "{}", Sources: []string{models.SourceAlertmanager}}
	st.sourceDefaults[models.SourceUniversal] = "tmpl"
	handler := newTestServer(t, st, &fakeMessenger{}).Handler
	headers := map[string]string{"Sec-Fetch-Site": "same-origin"}

	form := url.Values{"template_alertmanager": {"am"}, "template_universal": {""}, "template_teamsv2": {""}}
	rec := postForm(t, handler, "/admin/templates/source-defaults", form, headers)
	if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "error") {
		t.Fatalf("POST = %d to %s, want a redirect without an error", rec.Code, rec.Header().Get("Location"))
	}
	if st.sourceDefaults[models.SourceAlertmanager] != "am" || st.sourceDefaults[models.SourceUniversal] != "" {
		t.Errorf("defaults = %v, want alertmanager set and universal cleared", st.sourceDefaults)
	}

	rec = postForm(t, handler, "/admin/templates/source-defaults", url.Values{"template_teamsv2": {"am"}}, headers)
	if !strings.Contains(rec.Header().Get("Location"), "error") {
		t.Errorf("POST another source's template redirected to %s, want an error", rec.Header().Get("Location"))
	}

	page := do(t, handler, http.MethodGet, "/admin", "").Body.String()
	for _, want := range []string{`action="/admin/templates/source-defaults"`, `name="template_teamsv2"`, "Default for Alertmanager", `id="template-preset"`, `data-preset=`} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %s", want)
		}
	}
	// The teamsv2 select offers only the templates that handle it.
	start := strings.Index(page, `name="template_teamsv2"`)
	end := strings.Index(page[start:], "</select>")
	if strings.Contains(page[start:start+end], `value="am"`) {
		t.Error("the teamsv2 default offers an Alertmanager-only template")
	}
}
