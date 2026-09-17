package httpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func webhookUIStore() *fakeStore {
	st := seededUIStore()
	st.webhooks["hook"] = models.WebhookEndpoint{
		ID: "hook", TeamSlug: "platform", ChannelSlug: "alerts",
		DestinationID: "dest", TokenHash: hashToken("old-token"),
	}
	return st
}

func TestAdminPageListsWebhookEndpoints(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, webhookUIStore(), &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodGet, "/admin", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/teamsv2/platform/alerts") {
		t.Error("the admin page does not show the endpoint's URL")
	}
	// The token is a digest in the store and must not leak back out of it.
	if strings.Contains(rec.Body.String(), hashToken("old-token")) {
		t.Error("the admin page shows the stored token digest")
	}
}

func TestCreatingAWebhookShowsTheURLOnce(t *testing.T) {
	t.Parallel()

	st := seededUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{"team_slug": {"Platform"}, "channel_slug": {"Alerts"}, "destination_id": {"dest"}}
	rec := postForm(t, handler, "/admin/webhooks", form, map[string]string{"Sec-Fetch-Site": "same-origin"})

	// Rendered, not redirected: a redirect would put the token in a query
	// string, and from there into the browser history and the access log.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 and a rendered page: %s", rec.Code, rec.Header().Get("Location"))
	}

	created, ok := st.webhooks["generated"]
	if !ok {
		t.Fatal("no endpoint was created")
	}
	if created.TeamSlug != "platform" || created.ChannelSlug != "alerts" {
		t.Errorf("slugs = %s/%s, want them lower cased", created.TeamSlug, created.ChannelSlug)
	}

	token := tokenFromPage(t, rec.Body.String())
	if hashToken(token) != created.TokenHash {
		t.Error("the URL on the page does not carry the token that was stored")
	}

	// Asking for the page again must not show it a second time.
	again := do(t, handler, http.MethodGet, "/admin", "")
	if strings.Contains(again.Body.String(), token) {
		t.Error("the token is still on the page after the post that generated it")
	}
}

func TestSavingAWebhookKeepsItsToken(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{
		"id": {"hook"}, "team_slug": {"platform"}, "channel_slug": {"incidents"}, "destination_id": {"dest"},
	}
	rec := postForm(t, handler, "/admin/webhooks", form, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body)
	}

	updated := st.webhooks["hook"]
	if updated.ChannelSlug != "incidents" {
		t.Errorf("ChannelSlug = %q, want the edit applied", updated.ChannelSlug)
	}
	if updated.TokenHash != hashToken("old-token") {
		t.Error("editing an endpoint replaced the token its sender is using")
	}
}

func TestRotatingAWebhookTokenReplacesIt(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	form := url.Values{"id": {"hook"}}
	rec := postForm(t, handler, "/admin/webhooks/rotate", form, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 and a rendered page", rec.Code)
	}

	rotated := st.webhooks["hook"]
	if rotated.TokenHash == hashToken("old-token") {
		t.Fatal("the token was not replaced")
	}
	if hashToken(tokenFromPage(t, rec.Body.String())) != rotated.TokenHash {
		t.Error("the URL on the page does not carry the new token")
	}
}

func TestDeletingAWebhook(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := postForm(t, handler, "/admin/webhooks/delete", url.Values{"id": {"hook"}},
		map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body)
	}
	if _, ok := st.webhooks["hook"]; ok {
		t.Error("the endpoint is still there")
	}
}

func TestWebhookFormRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		path      string
		form      url.Values
		wantError string
	}{
		{
			name:      "a slug with a slash in it",
			path:      "/admin/webhooks",
			form:      url.Values{"team_slug": {"a/b"}, "channel_slug": {"alerts"}, "destination_id": {"dest"}},
			wantError: "URL segment",
		},
		{
			name:      "a slug that is punctuation",
			path:      "/admin/webhooks",
			form:      url.Values{"team_slug": {"platform!"}, "channel_slug": {"alerts"}, "destination_id": {"dest"}},
			wantError: "URL segment",
		},
		{
			name:      "an empty slug",
			path:      "/admin/webhooks",
			form:      url.Values{"team_slug": {""}, "channel_slug": {"alerts"}, "destination_id": {"dest"}},
			wantError: "URL segment",
		},
		{
			name:      "no destination",
			path:      "/admin/webhooks",
			form:      url.Values{"team_slug": {"platform"}, "channel_slug": {"alerts"}},
			wantError: "needs a destination",
		},
		{
			name:      "rotating one that is not there",
			path:      "/admin/webhooks/rotate",
			form:      url.Values{"id": {"missing"}},
			wantError: "not found",
		},
		{
			name:      "deleting one that is not there",
			path:      "/admin/webhooks/delete",
			form:      url.Values{"id": {"missing"}},
			wantError: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, seededUIStore(), &fakeMessenger{}).Handler
			rec := postForm(t, handler, tt.path, tt.form, map[string]string{"Sec-Fetch-Site": "same-origin"})

			if rec.Code != http.StatusSeeOther {
				t.Fatalf("status = %d, want a redirect carrying the error", rec.Code)
			}
			location := rec.Header().Get("Location")
			if !strings.Contains(location, url.QueryEscape(tt.wantError)) {
				t.Errorf("Location = %q, want it to mention %q", location, tt.wantError)
			}
		})
	}
}

func TestWebhookFormsRejectACrossOriginPost(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/admin/webhooks", "/admin/webhooks/rotate"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			st := webhookUIStore()
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			rec := postForm(t, handler, path, url.Values{"id": {"hook"}},
				map[string]string{"Sec-Fetch-Site": "cross-site"})

			if rec.Code != http.StatusForbidden {
				t.Errorf("status = %d, want 403", rec.Code)
			}
			if st.webhooks["hook"].TokenHash != hashToken("old-token") {
				t.Error("a cross-origin post changed the token")
			}
		})
	}
}

func TestWebhookAPI(t *testing.T) {
	t.Parallel()

	st := webhookUIStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodPost, "/api/webhooks",
		`{"team_slug":"ops","channel_slug":"pages","destination_id":"dest"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201: %s", rec.Code, rec.Body)
	}

	var created struct {
		ID    string `json:"id"`
		Token string `json:"token"`
		URL   string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Token == "" {
		t.Error("the response carries no token, which is the only time it exists in the clear")
	}
	if !strings.HasSuffix(created.URL, "/teamsv2/ops/pages/"+created.Token) {
		t.Errorf("url = %q, want it to end in the endpoint path and token", created.URL)
	}

	// Reading it back must not repeat the secret.
	rec = do(t, handler, http.MethodGet, "/api/webhooks/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), created.Token) {
		t.Error("reading an endpoint back returned its token")
	}

	rec = do(t, handler, http.MethodPost, "/api/webhooks/"+created.ID+"/rotate", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var rotated struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if rotated.Token == "" || rotated.Token == created.Token {
		t.Error("rotating returned no new token")
	}
	if st.webhooks[created.ID].TokenHash != hashToken(rotated.Token) {
		t.Error("the rotated token was not the one stored")
	}

	rec = do(t, handler, http.MethodDelete, "/api/webhooks/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE status = %d, want 200: %s", rec.Code, rec.Body)
	}
	if _, ok := st.webhooks[created.ID]; ok {
		t.Error("the endpoint is still there")
	}
}

func TestWebhookAPIRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{name: "no id", method: http.MethodGet, path: "/api/webhooks/", wantStatus: http.StatusNotFound},
		{name: "unknown id", method: http.MethodGet, path: "/api/webhooks/missing", wantStatus: http.StatusNotFound},
		{name: "not JSON", method: http.MethodPost, path: "/api/webhooks", body: "{", wantStatus: http.StatusBadRequest},
		{
			name: "a slug no URL could carry", method: http.MethodPost, path: "/api/webhooks",
			body: `{"team_slug":"a b","channel_slug":"c","destination_id":"dest"}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "no destination", method: http.MethodPost, path: "/api/webhooks",
			body: `{"team_slug":"a","channel_slug":"c"}`, wantStatus: http.StatusBadRequest,
		},
		{name: "collection does not take PUT", method: http.MethodPut, path: "/api/webhooks", wantStatus: http.StatusMethodNotAllowed},
		{name: "item does not take PATCH", method: http.MethodPatch, path: "/api/webhooks/hook", wantStatus: http.StatusMethodNotAllowed},
		{name: "rotate does not take GET", method: http.MethodGet, path: "/api/webhooks/hook/rotate", wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, webhookUIStore(), &fakeMessenger{}).Handler
			rec := do(t, handler, tt.method, tt.path, tt.body)
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d: %s", rec.Code, tt.wantStatus, rec.Body)
			}
		})
	}
}

func TestWebhookAPIReportsStoreFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		failOn string
		method string
		path   string
		body   string
	}{
		{name: "list", failOn: "ListWebhookEndpoints", method: http.MethodGet, path: "/api/webhooks"},
		{
			name: "create", failOn: "CreateWebhookEndpoint", method: http.MethodPost, path: "/api/webhooks",
			body: `{"team_slug":"a","channel_slug":"b","destination_id":"dest"}`,
		},
		{
			name: "update", failOn: "UpdateWebhookEndpoint", method: http.MethodPut, path: "/api/webhooks/hook",
			body: `{"team_slug":"a","channel_slug":"b","destination_id":"dest"}`,
		},
		{name: "rotate", failOn: "RotateWebhookEndpointToken", method: http.MethodPost, path: "/api/webhooks/hook/rotate"},
		{name: "delete", failOn: "DeleteWebhookEndpoint", method: http.MethodDelete, path: "/api/webhooks/hook"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := webhookUIStore().fail(tt.failOn)
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			rec := do(t, handler, tt.method, tt.path, tt.body)
			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want 500: %s", rec.Code, rec.Body)
			}
		})
	}
}

// tokenFromPage reads the last path segment of the URL the page reveals, which
// is the only place the token is ever readable.
func tokenFromPage(t *testing.T, body string) string {
	t.Helper()

	// The absolute URL, not the relative one the list and the hint text also
	// contain. httptest.NewRequest sends every request to example.com.
	const marker = "http://example.com/teamsv2/"
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatal("no webhook URL on the page")
	}
	rest := body[start:]
	if end := strings.IndexAny(rest, "<\" \n"); end >= 0 {
		rest = rest[:end]
	}
	parts := strings.Split(strings.TrimPrefix(rest, marker), "/")
	if len(parts) != 3 {
		t.Fatalf("URL %q has no token segment", rest)
	}
	return parts[2]
}
