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
	// The creator a scoped token answers to, as their sign-in recorded them.
	st.users["tester"] = models.User{Subject: "tester", Source: "oidc", Roles: []string{"admin"}}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodPost, "/api/tokens", `{"name":" staging ","scope":["alertmanager","alertmanager"]}`)
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

	if rec := do(t, handler, http.MethodPost, "/api/tokens", `{"name":"staging","scope":["alertmanager"]}`); rec.Code != http.StatusConflict {
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
		{name: "empty name", method: http.MethodPost, path: "/api/tokens", body: `{"name":"  ","scope":["universal"]}`, wantStatus: http.StatusBadRequest},
		{name: "overlong name", method: http.MethodPost, path: "/api/tokens", body: `{"name":"` + strings.Repeat("x", maxTokenNameLength+1) + `","scope":["universal"]}`, wantStatus: http.StatusBadRequest},
		{name: "no scope", method: http.MethodPost, path: "/api/tokens", body: `{"name":"a"}`, wantStatus: http.StatusForbidden},
		{name: "an unknown webhook", method: http.MethodPost, path: "/api/tokens", body: `{"name":"a","scope":["teamsv2"]}`, wantStatus: http.StatusForbidden},
		{name: "invalid JSON", method: http.MethodPost, path: "/api/tokens", body: `{`, wantStatus: http.StatusBadRequest},
		{name: "list fails", method: http.MethodGet, path: "/api/tokens", failOn: []string{"ListAccessTokens"}, wantStatus: http.StatusInternalServerError},
		{name: "create fails", method: http.MethodPost, path: "/api/tokens", body: `{"name":"a","scope":["universal"]}`, failOn: []string{"CreateAccessToken"}, wantStatus: http.StatusInternalServerError},
		{name: "delete fails", method: http.MethodDelete, path: "/api/tokens/id-1", failOn: []string{"DeleteAccessToken"}, wantStatus: http.StatusInternalServerError},
		{name: "delete a missing one", method: http.MethodDelete, path: "/api/tokens/x", wantStatus: http.StatusNotFound},
		{name: "list fails on delete", method: http.MethodDelete, path: "/api/tokens/x", failOn: []string{"ListAccessTokens"}, wantStatus: http.StatusInternalServerError},
		{name: "collection rejects PUT", method: http.MethodPut, path: "/api/tokens", wantStatus: http.StatusMethodNotAllowed},
		{name: "item rejects GET", method: http.MethodGet, path: "/api/tokens/x", wantStatus: http.StatusMethodNotAllowed},
		{name: "nested path", method: http.MethodDelete, path: "/api/tokens/x/y", wantStatus: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			handler := newTestServer(t, tokenStore().fail(tt.failOn...), &fakeMessenger{}).Handler
			if rec := do(t, handler, tt.method, tt.path, tt.body); rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d (body %s)", tt.method, tt.path, rec.Code, tt.wantStatus, rec.Body.String())
			}
		})
	}
}

// Tokens are self-service: whoever may use a webhook mints for it, and sees
// and revokes their own; a webhook admin sees and revokes all (ADR 0077).
func TestTokensAreSelfService(t *testing.T) {
	t.Parallel()

	seeded := func(role authz.Role) *fakeStore {
		st := sessionAs(tokenStore(), role)
		st.accessTokens["mine"] = models.AccessToken{ID: "mine", Name: "mine", TokenHash: "h-mine", CreatedBy: "tester", Scope: []string{"alertmanager"}}
		return st
	}
	tests := []struct {
		name   string
		role   authz.Role
		method string
		path   string
		body   string
		want   int
		check  func(t *testing.T, st *fakeStore, body string)
	}{
		{name: "an editor lists their own", role: authz.RoleEditor, method: http.MethodGet, path: "/api/tokens", want: http.StatusOK, check: func(t *testing.T, _ *fakeStore, body string) {
			if !strings.Contains(body, `"mine"`) || strings.Contains(body, "id-1") {
				t.Errorf("listed %s, want only the editor's own", body)
			}
		}},
		{name: "an admin lists all", role: authz.RoleAdmin, method: http.MethodGet, path: "/api/tokens", want: http.StatusOK, check: func(t *testing.T, _ *fakeStore, body string) {
			if !strings.Contains(body, "id-1") {
				t.Errorf("listed %s, want everyone's", body)
			}
		}},
		{name: "an editor revokes their own", role: authz.RoleEditor, method: http.MethodDelete, path: "/api/tokens/mine", want: http.StatusOK},
		{name: "an editor may not revoke another's", role: authz.RoleEditor, method: http.MethodDelete, path: "/api/tokens/id-1", want: http.StatusForbidden},
		{name: "an editor mints for both webhooks", role: authz.RoleEditor, method: http.MethodPost, path: "/api/tokens", body: `{"name":"e","scope":["alertmanager","universal"]}`, want: http.StatusCreated},
		{name: "a viewer may not mint", role: authz.RoleViewer, method: http.MethodPost, path: "/api/tokens", body: `{"name":"v","scope":["alertmanager"]}`, want: http.StatusForbidden},
		{name: "a viewer opens the page", role: authz.RoleViewer, method: http.MethodGet, path: "/admin/tokens", want: http.StatusOK, check: func(t *testing.T, _ *fakeStore, body string) {
			if !strings.Contains(body, "cannot mint a token") || strings.Contains(body, "id-1") {
				t.Error("a viewer's page should explain they cannot mint, and list nobody else's tokens")
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := seeded(tt.role)
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			rec := call(t, handler, tt.method, tt.path, tt.body)
			if rec.Code != tt.want {
				t.Fatalf("%s %s as %s = %d %s, want %d", tt.method, tt.path, tt.role, rec.Code, rec.Body.String(), tt.want)
			}
			if tt.check != nil {
				tt.check(t, st, rec.Body.String())
			}
		})
	}

	// A viewer granted one webhook mints for that one and no other.
	st := sessionAs(tokenStore(), authz.RoleViewer)
	grantTo(st, models.PrincipalUser, "tester", "Webhook", "universal", "use")
	handler := newTestServer(t, st, &fakeMessenger{}).Handler
	if rec := call(t, handler, http.MethodPost, "/api/tokens", `{"name":"u","scope":["universal"]}`); rec.Code != http.StatusCreated {
		t.Errorf("minting for the granted webhook = %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(t, handler, http.MethodPost, "/api/tokens", `{"name":"a","scope":["alertmanager"]}`); rec.Code != http.StatusForbidden {
		t.Errorf("minting beyond the grant = %d", rec.Code)
	}
	if body := call(t, handler, http.MethodGet, "/admin", "").Body.String(); !strings.Contains(body, `href="/admin/tokens"`) {
		t.Error("the nav does not offer tokens to someone who may use a webhook")
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

	rec = postForm(t, handler, "/admin/tokens/new", url.Values{"name": {"universal-sender"}, "scope": {"universal"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
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

	rec = postForm(t, handler, "/admin/tokens/new", url.Values{"name": {"universal-sender"}, "scope": {"universal"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
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
	if rec := do(t, handler, http.MethodGet, "/admin/tokens", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Something went wrong. Reference: ") {
		t.Errorf("GET /admin/tokens = %d, want the page showing the error", rec.Code)
	}

	handler = newTestServer(t, newFakeStore().fail("DeleteAccessToken"), &fakeMessenger{}).Handler
	rec := postForm(t, handler, "/admin/tokens/delete", url.Values{"id": {"x"}}, map[string]string{"Sec-Fetch-Site": "same-origin"})
	if !strings.Contains(rec.Header().Get("Location"), "error=") {
		t.Errorf("failed revoke redirects to %q, want an error", rec.Header().Get("Location"))
	}
}

// TestTokenMessageLevels: a token names people no further than its creator
// may message (ADR 0082).
func TestTokenMessageLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     authz.Role
		granted  string
		messages string
		want     int
	}{
		{"no level is anyone's", authz.RoleViewer, "", "", http.StatusCreated},
		{"an editor mints a self token", authz.RoleEditor, "", authz.MessagesSelf, http.StatusCreated},
		{"an editor may not mint an anyone token", authz.RoleEditor, "", authz.MessagesAnyone, http.StatusForbidden},
		{"a granted editor may", authz.RoleEditor, authz.ActionMessage, authz.MessagesAnyone, http.StatusCreated},
		{"naming anyone is not broadcasting", authz.RoleEditor, authz.ActionMessage, authz.MessagesEveryone, http.StatusForbidden},
		{"a broadcaster mints an everyone token", authz.RoleEditor, authz.ActionBroadcast, authz.MessagesEveryone, http.StatusCreated},
		{"an admin may", authz.RoleAdmin, "", authz.MessagesEveryone, http.StatusCreated},
		{"an unknown level", authz.RoleAdmin, "", "everybody", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(tokenStore(), tt.role)
			grantTo(st, models.PrincipalUser, "tester", "Webhook", "universal", "use")
			if tt.granted != "" {
				grantTo(st, models.PrincipalUser, "tester", authz.PeopleResource.Type, authz.PeopleResource.ID, tt.granted)
			}
			handler := newTestServer(t, st, &fakeMessenger{}).Handler
			rec := call(t, handler, http.MethodPost, "/api/tokens", `{"name":"n","scope":["universal"],"messages":"`+tt.messages+`"}`)
			if rec.Code != tt.want {
				t.Fatalf("mint = %d %s, want %d", rec.Code, rec.Body.String(), tt.want)
			}
			if tt.want != http.StatusCreated {
				return
			}
			var created accessTokenResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			if got := st.accessTokens[created.ID].Messages; got != tt.messages {
				t.Errorf("stored message level = %q, want %q", got, tt.messages)
			}
		})
	}

	// The form offers what the viewer may grant, and the list says what a token may.
	st := sessionAs(tokenStore(), authz.RoleEditor)
	st.accessTokens["mine"] = models.AccessToken{ID: "mine", Name: "mine", TokenHash: "h-mine", CreatedBy: "tester", Scope: []string{"universal"}, Messages: authz.MessagesSelf}
	handler := newTestServer(t, st, &fakeMessenger{}).Handler
	body := call(t, handler, http.MethodGet, "/admin/tokens", "").Body.String()
	if !strings.Contains(body, `value="self"`) || strings.Contains(body, `value="anyone"`) || strings.Contains(body, `href="/admin/broadcasts"`) {
		t.Error("an editor should be offered self and not anyone")
	}
	if !strings.Contains(body, "only me") {
		t.Error("the token list does not show the token's message level")
	}
	form := url.Values{"name": {"f"}, "scope": {"universal"}, "messages": {"self"}}
	if rec := postFormAs(t, handler, "/admin/tokens/new", form); rec.Code != http.StatusOK {
		t.Errorf("minting a self token from the form = %d", rec.Code)
	}
}
