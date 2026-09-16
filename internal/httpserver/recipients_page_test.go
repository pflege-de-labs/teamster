package httpserver

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// recipientsUIStore seeds one linked recipient and a route that targets them,
// so the page has both a name to show and a route to name.
func recipientsUIStore() *fakeStore {
	st := newFakeStore()
	st.recipients["r1"] = models.Recipient{
		ID: "r1", Subject: "alice@example.com", Name: "Alice",
		ConversationID: "19:chat", ServiceURL: "https://smba.example/teams/",
		BotChannelID: "msteams", CreatedAt: time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
	}
	st.routes["route"] = models.Route{ID: "route", Name: "On-call alerts", RecipientID: "r1", IsDefault: true}
	return st
}

func TestRecipientsPageListsRecipientsAndTheirRoutes(t *testing.T) {
	t.Parallel()

	st := sessionAs(recipientsUIStore(), authz.RoleEditor)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()

	for _, want := range []string{"Alice", "alice@example.com", "On-call alerts"} {
		if !strings.Contains(body, want) {
			t.Errorf("recipients page is missing %q", want)
		}
	}
	// An unblocked recipient shows neither the badge nor a reason.
	if strings.Contains(body, "MessageWritesBlocked") {
		t.Error("an unblocked recipient shows a blocked reason")
	}
}

// A person who never set a display name is still identifiable by subject —
// mirroring recipientLabel in routing_api.go.
func TestRecipientsPageFallsBackToSubjectWhenNameIsEmpty(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.recipients["r1"] = models.Recipient{ID: "r1", Subject: "bob@example.com", ConversationID: "19:chat", BotChannelID: "msteams"}

	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()
	if !strings.Contains(body, "bob@example.com") {
		t.Error("a recipient with no Name is not shown by their subject")
	}
}

// A recipient nobody has routed to still needs to say so plainly: nothing
// here would silently start delivering if a route were added later without
// the admin knowing it currently delivers to nobody.
func TestRecipientsPageNamesNoRoutesWhenNoneTarget(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleEditor)
	st.recipients["r1"] = models.Recipient{ID: "r1", Subject: "carol@example.com", ConversationID: "19:chat", BotChannelID: "msteams"}

	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()
	if !strings.Contains(body, "no routes deliver to this person") {
		t.Error("the page does not say a recipient has no routes targeting them")
	}
}

// The blocked state must be visible, not a subtle icon: the reason and since
// when both show, in the page's own words.
func TestRecipientsPageShowsTheBlockedStateVisibly(t *testing.T) {
	t.Parallel()

	st := sessionAs(recipientsUIStore(), authz.RoleEditor)
	blocked := st.recipients["r1"]
	blocked.BlockedAt = time.Date(2026, 9, 10, 9, 30, 0, 0, time.UTC)
	blocked.BlockedReason = "MessageWritesBlocked"
	st.recipients["r1"] = blocked

	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()

	for _, want := range []string{"Blocked", "MessageWritesBlocked", "2026-09-10"} {
		if !strings.Contains(body, want) {
			t.Errorf("the blocked state does not show %q", want)
		}
	}
}

// Hiding is not enforcing, but a viewer must not even be offered the control.
func TestRecipientsPageHidesUnlinkFromAViewer(t *testing.T) {
	t.Parallel()

	st := sessionAs(recipientsUIStore(), authz.RoleViewer)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()

	if strings.Contains(body, `action="/admin/recipients/delete"`) {
		t.Error("a viewer is offered the unlink control")
	}
	// The list itself is still theirs to see.
	if !strings.Contains(body, "Alice") {
		t.Error("a viewer cannot see the recipient list at all")
	}
}

func TestRecipientsPageOffersUnlinkToAnEditor(t *testing.T) {
	t.Parallel()

	st := sessionAs(recipientsUIStore(), authz.RoleEditor)
	body := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodGet, "/admin/recipients", "").Body.String()

	if !strings.Contains(body, `action="/admin/recipients/delete"`) {
		t.Error("an editor is not offered the unlink control")
	}
}

// The server refuses the post regardless of what the UI hid, and does not
// touch the store while refusing it.
func TestUnlinkIsRefusedForAViewer(t *testing.T) {
	t.Parallel()

	st := sessionAs(recipientsUIStore(), authz.RoleViewer)
	rec := asRole(t, newTestServer(t, st, &fakeMessenger{}).Handler, http.MethodPost, "/admin/recipients/delete", "id=r1")
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST /admin/recipients/delete as a viewer = %d, want 403", rec.Code)
	}
	if _, ok := st.recipients["r1"]; !ok {
		t.Error("a refused unlink still removed the recipient")
	}
}

// Deleting is allowed even when a route still references it, matching how a
// destination already behaves: the routing picture is what shows the
// consequence, not a block on the delete itself.
func TestUnlinkRemovesTheRecipientEvenWhenRouted(t *testing.T) {
	t.Parallel()

	st := recipientsUIStore()
	rec := postForm(t, newTestServer(t, st, &fakeMessenger{}).Handler, "/admin/recipients/delete", url.Values{"id": {"r1"}}, nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /admin/recipients/delete = %d, want 303", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.HasPrefix(loc, "/admin/recipients") {
		t.Errorf("redirected to %q, want back to the recipients page", loc)
	}
	if _, ok := st.recipients["r1"]; ok {
		t.Error("recipient was not removed")
	}
	// The route survives, pointing at a recipient that no longer exists --
	// the routing graph is what draws that as "missing".
	if _, ok := st.routes["route"]; !ok {
		t.Error("unlinking a recipient deleted the route that targeted them")
	}
}
