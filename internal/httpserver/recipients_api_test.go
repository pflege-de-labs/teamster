package httpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestListRecipientsAPI(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["r1"] = models.Recipient{
		ID: "r1", Subject: "alice@example.com", Name: "Alice",
		ConversationID: "19:chat", ServiceURL: "https://smba.example/teams/",
		BotChannelID: "msteams", AADObjectID: "aad-1", TenantID: "tenant-1",
	}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/api/recipients", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/recipients = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var got []recipientAPI
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].ID != "r1" || got[0].Name != "Alice" {
		t.Fatalf("recipients = %+v, want the seeded recipient", got)
	}

	// The conversation reference is operational plumbing this API's consumer
	// has no use for -- it must not appear in the response at all.
	for _, leaked := range []string{"19:chat", "smba.example", "aad-1", "tenant-1"} {
		if strings.Contains(rec.Body.String(), leaked) {
			t.Errorf("response leaks conversation reference %q: %s", leaked, rec.Body.String())
		}
	}
}

func TestListRecipientsAPIMethodNotAllowed(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/api/recipients", "{}")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /api/recipients = %d, want 405", rec.Code)
	}
}

func TestDeleteRecipientAPI(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.recipients["r1"] = models.Recipient{ID: "r1", Subject: "alice@example.com", ConversationID: "19:chat", BotChannelID: "msteams"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodDelete, "/api/recipients/r1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("DELETE /api/recipients/r1 = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if _, ok := st.recipients["r1"]; ok {
		t.Error("recipient was not deleted")
	}
}

func TestDeleteRecipientAPIRefusedForAViewer(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["r1"] = models.Recipient{ID: "r1", Subject: "alice@example.com", ConversationID: "19:chat", BotChannelID: "msteams"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodDelete, "/api/recipients/r1", "")
	if rec.Code != http.StatusForbidden {
		t.Errorf("DELETE /api/recipients/r1 as a viewer = %d, want 403", rec.Code)
	}
	if _, ok := st.recipients["r1"]; !ok {
		t.Error("a refused delete still removed the recipient")
	}
}

func TestRecipientByIDAPIMissingIDIsNotFound(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodDelete, "/api/recipients/", "")
	if rec.Code != http.StatusNotFound {
		t.Errorf("DELETE /api/recipients/ = %d, want 404", rec.Code)
	}
}

func TestRecipientByIDAPIMethodNotAllowed(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.recipients["r1"] = models.Recipient{ID: "r1", Subject: "alice@example.com", ConversationID: "19:chat", BotChannelID: "msteams"}

	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/api/recipients/r1", "")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /api/recipients/r1 = %d, want 405", rec.Code)
	}
}
