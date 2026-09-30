package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestApplySignInClaims(t *testing.T) {
	t.Parallel()

	var identity models.Identity
	applySignInClaims(&identity, map[string]any{
		"email": "alice@corp.example", "email_verified": true, "preferred_username": "alice@corp.example",
	}, []string{"oid-alice", "ignored"})
	if identity.ObjectID != "oid-alice" || identity.Username != "alice@corp.example" || !identity.EmailVerified || identity.Email != "alice@corp.example" {
		t.Errorf("identity = %+v", identity)
	}

	var bare models.Identity
	applySignInClaims(&bare, map[string]any{"email_verified": "yes"}, nil)
	if bare.EmailVerified || bare.ObjectID != "" {
		t.Errorf("identity from nothing = %+v", bare)
	}
}

func TestBindOwnChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		identity  models.Identity
		install   bool
		wantState bindState
		wantOID   string
	}{
		{name: "by object id", identity: models.Identity{ObjectID: "oid-alice"}, wantState: bindLinked, wantOID: "oid-alice"},
		{name: "by username as UPN", identity: models.Identity{Username: "BOB@corp.example"}, wantState: bindLinked, wantOID: "oid-bob"},
		{name: "a username that is only an alias", identity: models.Identity{Username: "b.b@corp.example"}, wantState: bindNoAccount},
		{name: "by verified email, alias included", identity: models.Identity{Email: "b.b@corp.example", EmailVerified: true}, wantState: bindLinked, wantOID: "oid-bob"},
		{name: "an unverified email", identity: models.Identity{Email: "alice@corp.example"}, wantState: bindNoAccount},
		{name: "nobody", identity: models.Identity{ObjectID: "oid-nobody"}, wantState: bindNoAccount},
		{name: "a local login", source: "local", identity: models.Identity{ObjectID: "oid-alice"}, wantState: bindLocal},
		{name: "no chat yet", identity: models.Identity{Username: "carol@corp.example"}, wantState: bindNoChat},
		{name: "no chat yet, installed now", identity: models.Identity{Username: "carol@corp.example"}, install: true, wantState: bindLinked, wantOID: "oid-carol"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			f.people.byAddr["oid-alice"] = f.store.directory["oid-alice"]
			f.people.installable = map[string]bool{"oid-carol": true}
			srv := f.server

			source := tt.source
			if source == "" {
				source = "oidc"
			}
			session := models.Session{Subject: "sub-1", Name: "Someone", Source: source, Identity: tt.identity}
			recipient, state, err := srv.bindOwnChat(t.Context(), session, tt.install)
			if err != nil {
				t.Fatalf("bindOwnChat: %v", err)
			}
			if state != tt.wantState {
				t.Fatalf("state = %s, want %s", state, tt.wantState)
			}
			if tt.wantOID == "" {
				if len(f.store.recipients) != 0 {
					t.Errorf("a recipient was bound: %+v", f.store.recipients)
				}
				return
			}
			if recipient.AADObjectID != tt.wantOID || recipient.Subject != "sub-1" || recipient.ConversationID == "" {
				t.Errorf("recipient = %+v", recipient)
			}

			// Binding again changes nothing. The fake does not keep what an
			// install did, so that case is not asked twice.
			if tt.install {
				return
			}
			if again, _, _ := srv.bindOwnChat(t.Context(), session, false); again.ID != recipient.ID || len(f.store.recipients) != 1 {
				t.Errorf("a second bind made %d recipients", len(f.store.recipients))
			}
		})
	}
}

func TestManagedNotificationsPageBindsFromSignIn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		identity  models.Identity
		wantText  string
		wantSetup bool
		wantBound bool
	}{
		{name: "found and bound", identity: models.Identity{Username: "alice@corp.example"}, wantText: "Linked", wantBound: true},
		{name: "no chat yet", identity: models.Identity{Username: "carol@corp.example"}, wantText: "Set it up now", wantSetup: true},
		{name: "not found", identity: models.Identity{Username: "nobody@corp.example"}, wantText: "could not find your Teams account"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			session := f.store.sessions[testSessionID]
			session.Source, session.Identity = "oidc", tt.identity
			f.store.sessions[testSessionID] = session

			req := httptest.NewRequest(http.MethodGet, "/admin/notifications", nil)
			req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
			rec := httptest.NewRecorder()
			f.handler.ServeHTTP(rec, req)
			body := rec.Body.String()

			if rec.Code != http.StatusOK || !strings.Contains(body, tt.wantText) {
				t.Errorf("page = %d, want it to say %q", rec.Code, tt.wantText)
			}
			for _, gone := range []string{`action="/admin/notifications/link"`, `action="/admin/notifications/cancel"`} {
				if strings.Contains(body, gone) {
					t.Errorf("the managed page still offers %s", gone)
				}
			}
			if got := strings.Contains(body, `action="/admin/notifications/setup"`); got != tt.wantSetup {
				t.Errorf("setup button = %v, want %v", got, tt.wantSetup)
			}
			if got := len(f.store.recipients) == 1; got != tt.wantBound {
				t.Errorf("bound = %v, want %v", got, tt.wantBound)
			}
		})
	}
}

func TestSetupOwnChat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		username    string
		installable bool
		want        string
	}{
		{name: "installed and bound", username: "carol@corp.example", installable: true, want: "notice=chat_set_up"},
		{name: "still no chat", username: "carol@corp.example", want: "error=chat_not_ready"},
		{name: "not found", username: "nobody@corp.example", want: "error=chat_not_found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newAddressedFixture(t, nil)
			f.people.installable = map[string]bool{"oid-carol": tt.installable}
			session := f.store.sessions[testSessionID]
			session.Source, session.Identity = "oidc", models.Identity{Username: tt.username}
			f.store.sessions[testSessionID] = session

			rec := postForm(t, f.handler, "/admin/notifications/setup", url.Values{}, nil)
			if !strings.Contains(rec.Header().Get("Location"), tt.want) {
				t.Errorf("setup = %d %s, want %s", rec.Code, rec.Header().Get("Location"), tt.want)
			}
		})
	}
}

func TestLinkCodesAreNotUsedWhenManaged(t *testing.T) {
	t.Parallel()

	f := newAddressedFixture(t, nil)

	if rec := postForm(t, f.handler, "/admin/notifications/link", url.Values{}, nil); !strings.Contains(rec.Header().Get("Location"), "codes_not_needed") {
		t.Errorf("mint = %d %s, want codes_not_needed", rec.Code, rec.Header().Get("Location"))
	}
	if rec := postForm(t, f.handler, "/admin/notifications/cancel", url.Values{}, nil); !strings.Contains(rec.Header().Get("Location"), "codes_not_needed") {
		t.Errorf("cancel = %s, want codes_not_needed", rec.Header().Get("Location"))
	}
	if rec := asRole(t, f.handler, http.MethodPost, "/api/recipients/link", ""); rec.Code != http.StatusConflict {
		t.Errorf("POST /api/recipients/link = %d, want 409", rec.Code)
	}
	if len(f.store.linkFlows) != 0 {
		t.Error("a link code was minted")
	}
}

func TestRecipientsFollowAReinstalledChat(t *testing.T) {
	t.Parallel()

	f := newBotFixtureWith(t, managed(""))
	f.store.recipients["r1"] = models.Recipient{ID: "r1", Subject: "alice", AADObjectID: "aad-1", ConversationID: "old", ServiceURL: "https://old.example/"}
	f.store.directory["aad-1"] = models.DirectoryUser{AADObjectID: "aad-1", InstallState: models.InstallRemoved, ConversationID: "old"}

	f.post(t, installActivity("conversationUpdate", "", true))

	if r := f.store.recipients["r1"]; r.ConversationID != "conv-1" {
		t.Errorf("recipient = %+v, want it moved to the new chat", r)
	}
}
