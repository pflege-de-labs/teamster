package httpserver

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// A template lists the webhooks it handles (ADR 0053).
func TestTheTemplateFormSavesSources(t *testing.T) {
	t.Parallel()

	st := seededStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{"name": {"Pager"}, "title": {"x"}, "sources": {models.SourceAlertmanager, models.SourceTeamsV2}}
	if rec := postForm(t, handler, "/admin/templates", form, map[string]string{"Sec-Fetch-Site": "same-origin"}); rec.Code != http.StatusSeeOther {
		t.Fatalf("POST = %d, want 303", rec.Code)
	}
	var saved *models.Template
	for _, template := range st.templates {
		if template.Name == "Pager" {
			saved = &template
		}
	}
	if saved == nil || !slices.Equal(saved.Sources, []string{models.SourceAlertmanager, models.SourceTeamsV2}) {
		t.Fatalf("saved = %+v, want both sources", saved)
	}

	if rec := do(t, handler, http.MethodPost, "/api/templates", `{"name":"x","title":"x","sources":["email"]}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST unknown source = %d, want 400", rec.Code)
	}

	page := do(t, handler, http.MethodGet, "/admin", "").Body.String()
	for _, want := range []string{`name="sources" value="alertmanager"`, `data-sources="alertmanager,teamsv2"`, "any webhook"} {
		if !strings.Contains(page, want) {
			t.Errorf("page does not contain %s", want)
		}
	}
}

// An alert from a webhook its route's template does not handle gets the
// built-in message rather than a template that cannot read it.
func TestATemplateForAnotherSourceFallsBackToTheBuiltInMessage(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)
	st.templates["tmpl"] = models.Template{ID: "tmpl", Title: "templated", Body: "{}", Sources: []string{models.SourceAlertmanager}}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"a":"b"},"title":"Own title"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "Own title" {
		t.Fatalf("posts = %+v, want the payload's own title", msg.posts)
	}
}

func TestARoutePinningASourceNeedsATemplateForIt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sources    []string
		wantStatus int
	}{
		{name: "any", wantStatus: http.StatusCreated},
		{name: "the pinned one", sources: []string{models.SourceAlertmanager}, wantStatus: http.StatusCreated},
		{name: "another", sources: []string{models.SourceTeamsV2}, wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := seededStore()
			st.templates["tmpl"] = models.Template{ID: "tmpl", Name: "Card", Body: "{}", Sources: tt.sources}
			handler := newTestServer(t, st, &fakeMessenger{}).Handler

			rec := do(t, handler, http.MethodPost, "/api/routes",
				`{"name":"am","destination_id":"dest","template_id":"tmpl","label_selector":{"teamster_source":"alertmanager"}}`)
			if rec.Code != tt.wantStatus {
				t.Errorf("POST = %d, want %d (%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// An endpoint only ever receives Teams V2 payloads.
func TestAWebhookEndpointNeedsATeamsV2Template(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	st.templates["am"] = models.Template{ID: "am", Name: "Alertmanager only", Body: "{}", Sources: []string{models.SourceAlertmanager}}
	st.templates["v2"] = models.Template{ID: "v2", Name: "Teams V2", Body: "{}", Sources: []string{models.SourceTeamsV2}}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	if rec := do(t, handler, http.MethodPost, "/api/webhooks",
		`{"team_slug":"ops","channel_slug":"pages","destination_id":"dest","template_id":"am"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("POST with an Alertmanager template = %d, want 400 (%s)", rec.Code, rec.Body.String())
	}
	if rec := do(t, handler, http.MethodPost, "/api/webhooks",
		`{"team_slug":"ops","channel_slug":"pages","destination_id":"dest","template_id":"v2"}`); rec.Code != http.StatusCreated {
		t.Errorf("POST with a Teams V2 template = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}

	page := do(t, handler, http.MethodGet, "/admin", "").Body.String()
	start := strings.Index(page, `action="/admin/webhooks"`)
	end := strings.Index(page[start:], "</form>")
	form := page[start : start+end]
	if strings.Contains(form, `value="am"`) || !strings.Contains(form, `value="v2"`) {
		t.Errorf("webhook form offers %s, want only the Teams V2 template", form)
	}
}

// A template changed after an endpoint named it sends the payload as given.
func TestATeamsV2EndpointWithAnotherSourcesTemplateSendsThePayload(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "posted"}
	st, handler := teamsV2Server(t, msg)
	hook := st.webhooks["hook"]
	hook.TemplateID = "tmpl"
	st.webhooks["hook"] = hook
	st.templates["tmpl"] = models.Template{ID: "tmpl", Title: "templated", Sources: []string{models.SourceAlertmanager}}

	rec := postTeamsV2(t, handler, "/teamsv2/platform/alerts/"+testWebhookToken, `{"@type":"MessageCard","title":"Build failed","text":"x"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "Build failed" {
		t.Fatalf("posts = %+v, want the payload's own title", msg.posts)
	}
}
