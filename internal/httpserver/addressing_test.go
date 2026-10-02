package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// issue gives the fixture a token for subject, as the token page would.
func (f addressedFixture) issue(t *testing.T, secret, subject, messages string) {
	t.Helper()
	if _, err := f.store.CreateAccessToken(t.Context(), models.AccessToken{
		Name: secret, TokenHash: hashToken(secret), CreatedBy: subject,
		Scope: []string{"alertmanager", "universal"}, Messages: messages,
	}); err != nil {
		t.Fatal(err)
	}
}

// TestNamingPeopleNeedsPermission covers who may name recipients (ADR 0082):
// a token's message scope, bounded by its creator's level as they are now.
func TestNamingPeopleNeedsPermission(t *testing.T) {
	t.Parallel()

	const (
		toAlice = `{"labels":{"kind":"password"},"recipients":["alice@corp.example"]}`
		toBob   = `{"labels":{"kind":"password"},"recipients":["bob@corp.example"]}`
		toBoth  = `{"labels":{"kind":"password"},"recipients":["alice@corp.example","bob@corp.example"]}`
		byLabel = `{"alerts":[{"status":"firing","fingerprint":"f1","labels":{"kind":"password","teamster_recipient":"alice@corp.example"}}]}`
		toNone  = `{"labels":{"kind":"password"}}`
	)
	tests := []struct {
		name       string
		setup      func(t *testing.T, f addressedFixture)
		token      string
		path       string
		body       string
		wantStatus int
		wantError  string
	}{
		{
			name:  "the deployment token names nobody",
			token: "token", body: toAlice,
			wantStatus: http.StatusForbidden, wantError: "may not name recipients",
		},
		{
			name:  "the deployment token still sends to no one in particular",
			token: "token", body: toNone,
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			name: "a token from before scopes names nobody",
			setup: func(t *testing.T, f addressedFixture) {
				if _, err := f.store.CreateAccessToken(t.Context(), models.AccessToken{Name: "legacy", TokenHash: hashToken("tst_legacy"), CreatedBy: "admin"}); err != nil {
					t.Fatal(err)
				}
			},
			token: "tst_legacy", body: toAlice,
			wantStatus: http.StatusForbidden, wantError: "may not name recipients",
		},
		{
			name:  "a token without a message scope names nobody, even an admin's",
			setup: func(t *testing.T, f addressedFixture) { f.issue(t, "tst_quiet", "admin", authz.MessagesNone) },
			token: "tst_quiet", body: toAlice,
			wantStatus: http.StatusForbidden, wantError: "may not name recipients",
		},
		{
			name:  "a self token names its creator",
			setup: aliceWithSelfToken,
			token: "tst_alice", body: toAlice,
			wantStatus: http.StatusOK,
		},
		{
			name:  "a self token names its creator by label on alertmanager",
			setup: aliceWithSelfToken,
			token: "tst_alice", path: "/webhook/alertmanager", body: byLabel,
			wantStatus: http.StatusOK,
		},
		{
			name:  "a self token names nobody else",
			setup: aliceWithSelfToken,
			token: "tst_alice", body: toBob,
			wantStatus: http.StatusForbidden, wantError: "only name its creator",
		},
		{
			name:  "a self token naming its creator and someone else",
			setup: aliceWithSelfToken,
			token: "tst_alice", body: toBoth,
			wantStatus: http.StatusForbidden, wantError: "only name its creator",
		},
		{
			name: "the linked chat says who the creator is",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.users["alice-sub"] = models.User{Subject: "alice-sub", Source: "oidc", Roles: []string{"editor"}}
				f.store.recipients["r-alice"] = models.Recipient{ID: "r-alice", Subject: "alice-sub", AADObjectID: "oid-alice"}
				f.issue(t, "tst_alice", "alice-sub", authz.MessagesSelf)
			},
			token: "tst_alice", body: toAlice,
			wantStatus: http.StatusOK,
		},
		{
			name: "a creator nobody knows the object id of names nobody",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.users["alice-sub"] = models.User{Subject: "alice-sub", Source: "oidc", Roles: []string{"editor"}}
				f.issue(t, "tst_alice", "alice-sub", authz.MessagesSelf)
			},
			token: "tst_alice", body: toAlice,
			wantStatus: http.StatusForbidden, wantError: "only name its creator",
		},
		{
			name: "an anyone token falls back to its creator's own level",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.users["ed"] = models.User{Subject: "ed", Source: "oidc", Roles: []string{"editor"}}
				f.issue(t, "tst_ed", "ed", authz.MessagesAnyone)
			},
			token: "tst_ed", body: toBob,
			wantStatus: http.StatusForbidden, wantError: "only name its creator",
		},
		{
			name: "an anyone token of a creator granted message",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.users["olga"] = models.User{Subject: "olga", Source: "oidc", Roles: []string{"editor"}, IdPGroups: []string{"office"}}
				f.store.permissions["p-office"] = models.Permission{
					ID: "p-office", PrincipalType: models.PrincipalIdPGroup, PrincipalID: "office",
					ResourceType: authz.PeopleResource.Type, ResourceID: authz.PeopleResource.ID, Actions: []string{authz.ActionMessage},
				}
				f.store.authzGen++
				f.issue(t, "tst_olga", "olga", authz.MessagesAnyone)
			},
			token: "tst_olga", body: toBoth,
			wantStatus: http.StatusOK,
		},
		{
			name: "a directory that cannot answer is retried, not refused",
			setup: func(t *testing.T, f addressedFixture) {
				aliceWithSelfToken(t, f)
				f.people.errs["bob@corp.example"] = errors.New("graph is down")
			},
			token: "tst_alice", body: toBob,
			wantStatus: http.StatusBadGateway,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			path := tt.path
			if path == "" {
				path = "/webhook/universal"
			}
			rec := postWebhook(t, f.handler, path, tt.token, tt.body)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d %s, want %d", rec.Code, rec.Body.String(), tt.wantStatus)
			}
			if !strings.Contains(rec.Body.String(), tt.wantError) {
				t.Errorf("body = %s, want it to say %q", rec.Body.String(), tt.wantError)
			}
			if tt.wantStatus == http.StatusForbidden && len(f.sentTo()) != 0 {
				t.Errorf("a refused message was sent: %v", f.sentTo())
			}
		})
	}
}

// aliceWithSelfToken is an editor who signed in with Alice's object id and
// holds a token that may name only her.
func aliceWithSelfToken(t *testing.T, f addressedFixture) {
	t.Helper()
	f.store.users["alice-sub"] = models.User{Subject: "alice-sub", Source: "oidc", Roles: []string{"editor"}, ObjectID: "oid-alice"}
	f.issue(t, "tst_alice", "alice-sub", authz.MessagesSelf)
}
