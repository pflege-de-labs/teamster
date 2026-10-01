package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

const scopedSecret = "tst_scoped"

// admitted is any answer past authentication.
const admitted = -1

// scopedTokenStore holds one alertmanager-only token made by creator.
func scopedTokenStore(creator string, user *models.User) *fakeStore {
	st := newFakeStore()
	st.accessTokens["scoped"] = models.AccessToken{ID: "scoped", Name: "scoped", TokenHash: hashToken(scopedSecret), CreatedBy: creator, Scope: []string{"alertmanager"}}
	st.accessTokens["legacy"] = models.AccessToken{ID: "legacy", Name: "legacy", TokenHash: hashToken("tst_legacy"), CreatedBy: "whoever"}
	if user != nil {
		st.users[user.Subject] = *user
	}
	// The token's policy is part of the model.
	st.authzGen = 1
	return st
}

func send(t *testing.T, h http.Handler, webhook, secret string) int {
	t.Helper()
	body := `{"alerts":[]}`
	if webhook == "universal" {
		body = `{"title":"x","text":"y"}`
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook/"+webhook, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+secret)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code
}

func TestScopedTokensAtTheWebhook(t *testing.T) {
	t.Parallel()

	editor := &models.User{Subject: "s-ed", Source: "oidc", Roles: []string{"editor"}}
	tests := []struct {
		name    string
		creator string
		user    *models.User
		grant   bool
		webhook string
		secret  string
		want    int
	}{
		{name: "in scope", creator: "s-ed", user: editor, webhook: "alertmanager", secret: scopedSecret, want: http.StatusOK},
		{name: "out of scope", creator: "s-ed", user: editor, webhook: "universal", secret: scopedSecret, want: http.StatusForbidden},
		{
			name: "the creator was disabled", creator: "s-ed", webhook: "alertmanager", secret: scopedSecret, want: http.StatusForbidden,
			user: &models.User{Subject: "s-ed", Source: "oidc", Roles: []string{"editor"}, DisabledAt: time.Now()},
		},
		{
			name: "the creator is a viewer now", creator: "s-ed", webhook: "alertmanager", secret: scopedSecret, want: http.StatusForbidden,
			user: &models.User{Subject: "s-ed", Source: "oidc", Roles: []string{"viewer"}},
		},
		{
			name: "a viewer the webhook was granted to", creator: "s-v", grant: true, webhook: "alertmanager", secret: scopedSecret, want: http.StatusOK,
			user: &models.User{Subject: "s-v", Source: "oidc", Roles: []string{"viewer"}},
		},
		{name: "an unknown creator", creator: "s-gone", webhook: "alertmanager", secret: scopedSecret, want: http.StatusForbidden},
		{name: "the local admin, never signed in", creator: "admin", webhook: "alertmanager", secret: scopedSecret, want: http.StatusOK},
		{
			name: "the local admin, signed in", creator: "admin", webhook: "alertmanager", secret: scopedSecret, want: http.StatusOK,
			user: &models.User{Subject: "admin", Source: "local"},
		},
		// Admitted: what delivery then makes of an event with no route is another matter.
		{name: "a token from before scopes, either webhook", creator: "s-gone", webhook: "universal", secret: "tst_legacy", want: admitted},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := scopedTokenStore(tt.creator, tt.user)
			if tt.grant {
				grantTo(st, models.PrincipalUser, tt.creator, "Webhook", "alertmanager", "use")
			}
			h := newTestServer(t, st, &fakeMessenger{}).Handler
			got := send(t, h, tt.webhook, tt.secret)
			if tt.want == admitted && got != http.StatusUnauthorized && got != http.StatusForbidden {
				return
			}
			if got != tt.want {
				t.Errorf("POST /webhook/%s = %d, want %d", tt.webhook, got, tt.want)
			}
		})
	}
}

// Taking the grant away takes it from the token at its next use.
func TestRevokingTheCreatorsGrantRevokesTheToken(t *testing.T) {
	t.Parallel()

	st := scopedTokenStore("s-v", &models.User{Subject: "s-v", Source: "oidc", Roles: []string{"viewer"}, IdPGroups: []string{"senders"}})
	st.groups["g"] = models.Group{ID: "g", Name: "senders"}
	st.members = []models.GroupMember{{GroupID: "g", Type: models.MemberIdPGroup, ID: "senders"}}
	grantTo(st, models.PrincipalGroup, "g", "Webhook", "*", "use")
	h := newTestServer(t, st, &fakeMessenger{}).Handler

	if got := send(t, h, "alertmanager", scopedSecret); got != http.StatusOK {
		t.Fatalf("before = %d, want 200", got)
	}
	if err := st.RemoveGroupMember(t.Context(), st.members[0]); err != nil {
		t.Fatal(err)
	}
	if got := send(t, h, "alertmanager", scopedSecret); got != http.StatusForbidden {
		t.Errorf("after the group lost its member = %d, want 403", got)
	}
}

func TestScopedTokensFailClosed(t *testing.T) {
	t.Parallel()

	editor := &models.User{Subject: "s-ed", Source: "oidc", Roles: []string{"editor"}}
	for _, failOn := range []string{"AuthzGeneration", "GetUser"} {
		st := scopedTokenStore("s-ed", editor).fail(failOn)
		h := newTestServer(t, st, &fakeMessenger{}).Handler
		if got := send(t, h, "alertmanager", scopedSecret); got != http.StatusServiceUnavailable {
			t.Errorf("with %s failing = %d, want 503", failOn, got)
		}
	}
}
