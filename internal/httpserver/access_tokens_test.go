package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestAccessTokenAPI(t *testing.T) {
	t.Parallel()

	st := newFakeStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodPost, "/api/tokens", `{"name":" staging "}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/tokens = %d, want 201 (body %s)", rec.Code, rec.Body.String())
	}
	var created accessTokenResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "staging" || !strings.HasPrefix(created.Token, accessTokenPrefix) || created.CreatedBy == "" {
		t.Errorf("created = %+v, want the trimmed name, its creator and a prefixed token", created)
	}
	if st.accessTokens[created.ID].TokenHash != hashToken(created.Token) {
		t.Error("the store holds something other than the digest of the token handed out")
	}

	// The token that was just issued is what the webhook now admits.
	if rec := postWebhookBearer(t, handler, created.Token); rec.Code != http.StatusOK {
		t.Errorf("webhook with the issued token = %d, want 200", rec.Code)
	}

	rec = do(t, handler, http.MethodGet, "/api/tokens", "")
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), created.Token) || strings.Contains(rec.Body.String(), hashToken(created.Token)) {
		t.Errorf("GET /api/tokens = %d %s, want a list without the token or its digest", rec.Code, rec.Body.String())
	}

	if rec := do(t, handler, http.MethodPost, "/api/tokens", `{"name":"staging"}`); rec.Code != http.StatusConflict {
		t.Errorf("a taken name = %d, want 409", rec.Code)
	}

	if rec := do(t, handler, http.MethodDelete, "/api/tokens/"+created.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("DELETE = %d, want 200", rec.Code)
	}
	if rec := postWebhookBearer(t, handler, created.Token); rec.Code != http.StatusUnauthorized {
		t.Errorf("webhook with a revoked token = %d, want 401", rec.Code)
	}
}

func postWebhookBearer(t *testing.T, handler http.Handler, token string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/webhook/alertmanager", strings.NewReader(`{"alerts":[]}`))
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestAccessTokenAPIRefusals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		failOn     []string
		wantStatus int
	}{
		{name: "empty name", method: http.MethodPost, path: "/api/tokens", body: `{"name":"  "}`, wantStatus: http.StatusBadRequest},
		{name: "overlong name", method: http.MethodPost, path: "/api/tokens", body: `{"name":"` + strings.Repeat("x", maxTokenNameLength+1) + `"}`, wantStatus: http.StatusBadRequest},
		{name: "invalid JSON", method: http.MethodPost, path: "/api/tokens", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "list fails", method: http.MethodGet, path: "/api/tokens", failOn: []string{"ListAccessTokens"}, wantStatus: http.StatusInternalServerError},
		{name: "create fails", method: http.MethodPost, path: "/api/tokens", body: `{"name":"a"}`, failOn: []string{"CreateAccessToken"}, wantStatus: http.StatusInternalServerError},
		{name: "delete fails", method: http.MethodDelete, path: "/api/tokens/x", failOn: []string{"DeleteAccessToken"}, wantStatus: http.StatusInternalServerError},
		{name: "collection rejects PUT", method: http.MethodPut, path: "/api/tokens", wantStatus: http.StatusMethodNotAllowed},
		{name: "item rejects GET", method: http.MethodGet, path: "/api/tokens/x", wantStatus: http.StatusMethodNotAllowed},
		{name: "nested path", method: http.MethodDelete, path: "/api/tokens/x/y", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, newFakeStore().fail(tt.failOn...), &fakeMessenger{}).Handler
			if rec := do(t, handler, tt.method, tt.path, tt.body); rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d (body %s)", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// Every token admits a sender to every route, so only an admin issues one.
func TestAccessTokensAreTheAdminsAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		role   authz.Role
		method string
		path   string
		body   string
	}{
		{name: "editor lists", role: authz.RoleEditor, method: http.MethodGet, path: "/api/tokens"},
		{name: "editor creates", role: authz.RoleEditor, method: http.MethodPost, path: "/api/tokens", body: `{"name":"a"}`},
		{name: "editor revokes", role: authz.RoleEditor, method: http.MethodDelete, path: "/api/tokens/id-1"},
		{name: "viewer opens the page", role: authz.RoleViewer, method: http.MethodGet, path: "/admin/tokens"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(tokenStore(), tt.role)
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			if rec := asRole(t, handler, tt.method, tt.path, tt.body); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s as %s = %d, want 403", tt.method, tt.path, tt.role, rec.Code)
			}
			if _, ok := st.accessTokens["id-1"]; !ok || len(st.accessTokens) != 1 {
				t.Error("a refused request changed the tokens")
			}
		})
	}
}

func TestTokensPage(t *testing.T) {
	t.Parallel()

	st := tokenStore()
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodGet, "/admin/tokens", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "alertmanager") {
		t.Fatalf("GET /admin/tokens = %d, want the page listing the token", rec.Code)
	}
	if rec := do(t, handler, http.MethodGet, "/admin", ""); !strings.Contains(rec.Body.String(), `href="/admin/tokens"`) {
		t.Error("an admin's sidebar has no link to the tokens page")
	}

	rec = postForm(t, handler, "/admin/tokens/new", url.Values{"name": {"universal-sender"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create = %d, want the page rendered in place", rec.Code)
	}
	var issued models.AccessToken
	for _, token := range st.accessTokens {
		if token.Name == "universal-sender" {
			issued = token
		}
	}
	body := rec.Body.String()
	if issued.ID == "" || !strings.Contains(body, accessTokenPrefix) || !strings.Contains(body, "/webhook/alertmanager") {
		t.Errorf("create response does not reveal the token and the Alertmanager snippet: %s", body)
	}

	rec = postForm(t, handler, "/admin/tokens/new", url.Values{"name": {"universal-sender"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Errorf("a taken name = %d %s, want a redirect with an error", rec.Code, rec.Header().Get("Location"))
	}

	if rec := postForm(t, handler, "/admin/tokens/new", url.Values{"name": {"x"}}, map[string]string{"Sec-Fetch-Site": "cross-site"}); rec.Code != http.StatusForbidden {
		t.Errorf("cross-site create = %d, want 403", rec.Code)
	}

	rec = postForm(t, handler, "/admin/tokens/delete", url.Values{"id": {issued.ID}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("revoke = %d, want 303", rec.Code)
	}
	if _, ok := st.accessTokens[issued.ID]; ok {
		t.Error("the revoked token is still stored")
	}

	if rec := do(t, handler, http.MethodPost, "/admin/tokens", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/tokens = %d, want 405", rec.Code)
	}
}

func TestTokensPageReportsAStoreFailure(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, newFakeStore().fail("ListAccessTokens"), &fakeMessenger{}).Handler
	if rec := do(t, handler, http.MethodGet, "/admin/tokens", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), errStore.Error()) {
		t.Errorf("GET /admin/tokens = %d, want the page showing the error", rec.Code)
	}

	handler = newTestServer(t, newFakeStore().fail("DeleteAccessToken"), &fakeMessenger{}).Handler
	rec := postForm(t, handler, "/admin/tokens/delete", url.Values{"id": {"x"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Errorf("failed revoke redirects to %q, want an error", rec.Header().Get("Location"))
	}
}
