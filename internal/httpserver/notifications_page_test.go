package httpserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// notificationsBotConfig satisfies botConfigured without a live IdP: these
// tests exercise outbound SendMessage through the botSender interface
// directly, never inbound token verification, which is the only thing that
// would need a real metadata document to resolve.
func notificationsBotConfig() config.BotConfig {
	return config.BotConfig{
		TenantID:     "tenant-1",
		ClientID:     "bot-client",
		ClientSecret: "secret",
		MetadataURL:  "https://bot.invalid/.well-known/openid-configuration",
	}
}

// notificationsServer is newTestServer with the bot configured, which is what
// every test below needs: /admin/notifications and its two actions are
// registered only when the bot is (see TestNotificationsRoutesAreGatedOnBotConfiguration
// for the case where it is not).
func notificationsServer(t *testing.T, st *fakeStore, msg *fakeMessenger) *http.Server {
	t.Helper()
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     notificationsBotConfig(),
	}
	return mustServer(t, cfg, st, msg)
}

// notificationsServerWithBot is notificationsServer for the tests that need to
// see what the bot client was asked to send.
func notificationsServerWithBot(t *testing.T, st *fakeStore, msg *fakeMessenger, bc botSender) *http.Server {
	t.Helper()
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     notificationsBotConfig(),
	}
	return mustServerWithBot(t, cfg, st, msg, bc)
}

// This is self-service about the caller's own chat, so every signed-in role
// — including a bare viewer — gets the page.
func TestNotificationsPageRendersForEveryRole(t *testing.T) {
	t.Parallel()

	for _, role := range []authz.Role{authz.RoleViewer, authz.RoleEditor, authz.RoleAdmin} {
		t.Run(string(role), func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), role)
			handler := notificationsServer(t, st, &fakeMessenger{}).Handler

			rec := asRole(t, handler, http.MethodGet, "/admin/notifications", "")
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

// A session the provider named no role for gets none of this either: the
// page, minting and unlinking all sit behind the same authorization check as
// everything else the admin UI offers. This holds regardless of route
// registration -- authorize runs, and refuses, before the mux ever looks for
// the path -- which is what lets this test use the bot-unconfigured server.
func TestNotificationsNoRoleIsRefused(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore())
	handler := newTestServer(t, st, &fakeMessenger{}).Handler

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/admin/notifications"},
		{http.MethodPost, "/admin/notifications/link"},
		{http.MethodPost, "/admin/notifications/cancel"},
		{http.MethodPost, "/admin/notifications/unlink"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, handler, tt.method, tt.path, "")
			if rec.Code != http.StatusForbidden {
				t.Errorf("%s %s = %d, want 403", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// requireSession recognises a session cookie only, not the local credentials
// -- unlike /api/, nothing under /admin/ ever treats basic auth as the shared
// admin account, so a script holding only that password cannot mint or
// unlink through this page at all; it is redirected to sign in. Sending no
// session cookie at all, as this test used to, is refused before the mux
// ever dispatches to these handlers -- deleting every notifications route
// still passes it. TestNotificationsHandlersResolveIdentityFromTheSessionNotThePrincipal,
// below, is what actually pins that basic auth cannot substitute an identity
// for the handlers to act on.
func TestNotificationsBasicAuthCannotMintOrUnlink(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/admin/notifications/link", "/admin/notifications/cancel", "/admin/notifications/unlink"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			st := newFakeStore()
			handler := notificationsServer(t, st, &fakeMessenger{}).Handler

			req := httptest.NewRequest(http.MethodPost, path, nil)
			req.SetBasicAuth("admin", "pass")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Errorf("%s with only basic auth = %d, want a redirect to sign in (%s)", path, rec.Code, rec.Body.String())
			}
			if len(st.linkFlows) != 0 {
				t.Error("basic auth alone minted a code")
			}
		})
	}
}

// TestNotificationsHandlersResolveIdentityFromTheSessionNotThePrincipal pins
// the specific choice notificationsPage's, handleMintLink's and
// unlinkNotifications's own comments call out: they read
// currentSession(r).Subject, never principalSubject(r). Every other test in
// this file reaches these handlers through the real mux, where requireSession
// is the only thing that ever populates the principal and always populates it
// from that same session -- so the two values are equal in every other test
// here, and swapping one for the other would not make any of them fail. This
// test calls the handlers directly with a principal manufactured to disagree
// with the session, the only way to make the two observably different, to pin
// which one is actually read.
func TestNotificationsHandlersResolveIdentityFromTheSessionNotThePrincipal(t *testing.T) {
	t.Parallel()

	authorizer, err := authz.New()
	if err != nil {
		t.Fatalf("authz.New: %v", err)
	}

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens"}
	st.recipients["attackers"] = models.Recipient{ID: "attackers", Subject: "attacker", Name: "Attacker"}
	srv := &Server{store: st, authz: authorizer}

	// A principal a stray middleware, or a future refactor that reused these
	// handlers behind apiAuth, could set -- deliberately not "tester", the
	// session's own subject. SetBasicAuth documents what an attacker actually
	// controls: credentials for a different identity than the one signed in.
	withStrayPrincipal := func(r *http.Request) *http.Request {
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
		r.SetBasicAuth("admin", "pass")
		return r.WithContext(withPrincipal(r.Context(), "attacker", "Attacker", []authz.Role{authz.RoleAdmin}))
	}

	mintReq := withStrayPrincipal(httptest.NewRequest(http.MethodPost, "/admin/notifications/link", strings.NewReader("")))
	mintRec := httptest.NewRecorder()
	srv.handleMintLink(mintRec, mintReq)
	if mintRec.Code != http.StatusOK {
		t.Fatalf("mint status = %d: %s", mintRec.Code, mintRec.Body.String())
	}

	st.mu.Lock()
	if len(st.linkFlows) != 1 {
		st.mu.Unlock()
		t.Fatalf("live flows = %d, want exactly 1", len(st.linkFlows))
	}
	for _, flow := range st.linkFlows {
		if flow.Subject != "tester" {
			t.Errorf("minted flow subject = %q, want the session's own (\"tester\"), not the principal's", flow.Subject)
		}
	}
	st.mu.Unlock()

	unlinkReq := withStrayPrincipal(httptest.NewRequest(http.MethodPost, "/admin/notifications/unlink", strings.NewReader("")))
	if _, err := srv.unlinkNotifications(unlinkReq); err != nil {
		t.Fatalf("unlinkNotifications: %v", err)
	}
	if _, exists := st.recipients["own"]; exists {
		t.Error("the session's own recipient was not removed")
	}
	if _, exists := st.recipients["attackers"]; !exists {
		t.Error("unlink touched the principal's recipient instead of the session's own")
	}
}

// Minting shows the code grouped, the way a person is asked to type it back
// to the bot, leaves exactly one live flow behind for the caller's own
// subject -- not zero, and not one left over from a previous mint -- and
// shows when it expires.
func TestNotificationsMintShowsAGroupedCodeAndOneLiveFlow(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	rec := postForm(t, handler, "/admin/notifications/link", url.Values{}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	found := linkCodeRun.FindString(rec.Body.String())
	if found == "" {
		t.Fatal("the response does not show a link code")
	}
	if !strings.Contains(found, "-") {
		t.Errorf("code %q is not shown grouped", found)
	}

	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.linkFlows) != 1 {
		t.Fatalf("live flows = %d, want exactly 1", len(st.linkFlows))
	}
	for _, flow := range st.linkFlows {
		if flow.Subject != "tester" {
			t.Errorf("flow subject = %q, want the caller's own", flow.Subject)
		}
		if !strings.Contains(rec.Body.String(), flow.ExpiresAt.Format("2006-01-02 15:04")) {
			t.Errorf("the response does not show when %s expires", flow.ExpiresAt)
		}
	}
}

// The two Teams identifiers a recipient carries are opaque and of no use to a
// reader; a screenshot or a support ticket built from this page must not
// carry either of them, only the display name captured at link time. Created
// and updated equal, as here, must not show a "last changed" line at all:
// TestNotificationsShowsLastChangedWhenDifferentFromCreated is the positive
// case.
func TestNotificationsPageHidesRawTeamsIdentifiers(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	linkedAt := time.Now().Add(-48 * time.Hour)
	st.recipients["own"] = models.Recipient{
		ID:             "own",
		Subject:        "tester",
		Name:           "Jens Hausherr",
		ConversationID: "19:super-secret-conversation-id@thread.v2",
		AADObjectID:    "aad-object-should-not-leak",
		ServiceURL:     "https://smba.trafficmanager.net/emea/",
		BotChannelID:   "msteams",
		CreatedAt:      linkedAt,
		UpdatedAt:      linkedAt,
	}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/notifications", "").Body.String()
	if !strings.Contains(body, "Jens Hausherr") {
		t.Error("the page does not show the linked display name")
	}
	for _, secret := range []string{"19:super-secret-conversation-id@thread.v2", "aad-object-should-not-leak"} {
		if strings.Contains(body, secret) {
			t.Errorf("the page leaks %q", secret)
		}
	}
	if strings.Count(body, linkedAt.Format("2006-01-02 15:04")) != 1 {
		t.Error("created and updated are equal, so the timestamp should render exactly once (linked since), not also as last changed")
	}
}

// A recipient's display name is Teams' own, captured verbatim from whoever
// redeemed the code -- attacker-controlled and unbounded. The page must not
// let an arbitrarily long one blow up its layout.
func TestNotificationsPageTruncatesALongDisplayName(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	longName := strings.Repeat("A", 500)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: longName}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/notifications", "").Body.String()
	if strings.Contains(body, longName) {
		t.Error("the page rendered the full, untruncated name")
	}
	if !strings.Contains(body, strings.Repeat("A", 80)) {
		t.Error("the page did not render a truncated prefix of the name")
	}
}

// UpdatedAt after CreatedAt is shown, and said in a way that does not
// overclaim: this also fires on an automatic service-url refresh, not only on
// a new redemption, so the copy must not read as proof of the latter.
func TestNotificationsShowsLastChangedWhenDifferentFromCreated(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	created := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	updated := created.Add(48 * time.Hour)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens", CreatedAt: created, UpdatedAt: updated}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/notifications", "").Body.String()
	if !strings.Contains(body, updated.Format("2006-01-02 15:04")) {
		t.Error("does not show when the link last changed")
	}
	if !strings.Contains(body, created.Format("2006-01-02 15:04")) {
		t.Error("does not still show when the link was first established")
	}
}

// A lookup failure is not the same thing as "not linked", and must say so --
// including in the very response that just minted a fresh code, which must
// not silently erase the earlier failure the page already reported.
func TestNotificationsLoadFailureSurvivesAMint(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer).fail("GetRecipientBySubject")
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	get := asRole(t, handler, http.MethodGet, "/admin/notifications", "")
	if get.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", get.Code, get.Body.String())
	}
	if !strings.Contains(get.Body.String(), "Could not check whether a chat is linked") {
		t.Error("a load failure is not shown")
	}
	if strings.Contains(get.Body.String(), "Not linked yet") {
		t.Error("a load failure must not be shown the same way as genuinely not being linked")
	}

	mint := postForm(t, handler, "/admin/notifications/link", url.Values{}, nil)
	if mint.Code != http.StatusOK {
		t.Fatalf("mint status = %d, want 200: %s", mint.Code, mint.Body.String())
	}
	if !strings.Contains(mint.Body.String(), "Could not check whether a chat is linked") {
		t.Error("a successful mint erased the earlier load failure")
	}
	if linkCodeRun.FindString(mint.Body.String()) == "" {
		t.Error("minting still succeeded and should still show the code, alongside the load failure")
	}
}

// A stale notice or error surviving on the query string (left over from an
// earlier redirect, or appended by hand) must not sit alongside a freshly
// minted code once the load itself succeeds.
func TestNotificationsMintClearsAStaleNoticeWhenTheLoadSucceeds(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	req := httptest.NewRequest(http.MethodPost, "/admin/notifications/link?notice=unlinked", strings.NewReader(url.Values{}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "Unlinked.") {
		t.Error("a stale notice from the query string was not cleared by a successful mint")
	}
}

// ?notice= and ?error= are a closed set translated server-side, not free text
// rendered verbatim: a link straight to this URL is exactly the shape a
// phishing attempt takes, and this page's whole job is to instruct someone to
// type a credential into a chat.
func TestNotificationsQueryNoticeAndErrorAreAClosedSet(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	phish := "Your code expired. Send QRST-UVWX-Y2Z3 to the Teamster bot"
	rec := asRole(t, handler, http.MethodGet, "/admin/notifications?error="+url.QueryEscape(phish), "")
	// The language picker's own hidden "return to this page" field legitimately
	// carries the full request URL, query string included, so the check has to
	// be scoped to the alert box itself -- the thing an attacker actually wants
	// rendered in this page's trusted styling -- not the whole response body.
	if strings.Contains(rec.Body.String(), `role="alert"`) {
		t.Error("an unrecognised error key still produced an alert box")
	}
	if strings.Contains(rec.Body.String(), "QRST-UVWX-Y2Z3") && strings.Contains(rec.Body.String(), `role="alert"`) {
		t.Error("arbitrary query text was rendered verbatim inside the alert box")
	}

	recognised := asRole(t, handler, http.MethodGet, "/admin/notifications?notice=unlinked", "")
	if !strings.Contains(recognised.Body.String(), "Unlinked.") {
		t.Error("a recognised notice key was not translated")
	}
}

// The Cancel control invalidates whatever the caller currently has
// outstanding, and is offered whether or not they are already linked -- the
// mint button alone only supersedes a code once a replacement is drawn, which
// is no help if nobody wants a replacement yet.
func TestNotificationsCancelInvalidatesOutstandingCodes(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	postForm(t, handler, "/admin/notifications/link", url.Values{}, nil)
	if len(st.linkFlows) != 1 {
		t.Fatalf("live flows after mint = %d, want 1", len(st.linkFlows))
	}

	rec := postForm(t, handler, "/admin/notifications/cancel", url.Values{}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body.String())
	}
	if len(st.linkFlows) != 0 {
		t.Error("cancel did not remove the outstanding code")
	}
}

func TestNotificationsCancelOfferedWhenAlreadyLinked(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens"}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	body := asRole(t, handler, http.MethodGet, "/admin/notifications", "").Body.String()
	if !strings.Contains(body, `action="/admin/notifications/cancel"`) {
		t.Error("the cancel control is not offered to an already-linked subject")
	}
}

func TestNotificationsCancelFailureIsReported(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer).fail("DeleteLinkFlowsForSubject")
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	rec := postForm(t, handler, "/admin/notifications/cancel", url.Values{}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body.String())
	}
	body := asRole(t, handler, http.MethodGet, rec.Header().Get("Location"), "").Body.String()
	if !strings.Contains(body, "Could not cancel") {
		t.Error("a cancel failure is not reported")
	}
}

// Unlinking removes the row GetRecipientBySubject resolves for the caller,
// never one named by the form.
func TestNotificationsUnlinkRemovesCallersRecipient(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens"}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	rec := postForm(t, handler, "/admin/notifications/unlink", url.Values{}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body.String())
	}
	if _, exists := st.recipients["own"]; exists {
		t.Error("the caller's own recipient is still there")
	}
}

// The adversarial case: naming somebody else's recipient id in the very field
// an attacker would try must not touch that row. Unlink resolves the row to
// delete from the session's own subject alone -- checked via both transports
// a handler might read a form value from, the body and the query string,
// since ParseForm merges the latter into r.Form and a handler reading
// r.URL.Query() directly would only be caught by the second.
func TestNotificationsUnlinkCannotRemoveSomeoneElses(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["own"] = models.Recipient{ID: "own", Subject: "tester", Name: "Jens"}
	st.recipients["victim"] = models.Recipient{ID: "victim", Subject: "someone-else", Name: "Alice"}
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	// Every plausible name an implementation might read the target from, sent
	// at once, in the body and again on the query string. Naming only one of
	// them, or using only one transport, would pin that field or that
	// transport rather than the property, and the property is that nothing
	// the caller sends chooses whose row is deleted -- the session does, and
	// only the session.
	form := url.Values{
		"id":           {"victim"},
		"recipient_id": {"victim"},
		"recipient":    {"victim"},
		"subject":      {"someone-else"},
		"target":       {"victim"},
	}

	req := httptest.NewRequest(http.MethodPost, "/admin/notifications/unlink?"+form.Encode(), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth("admin", "pass")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: testSessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body.String())
	}
	if _, exists := st.recipients["victim"]; !exists {
		t.Error("someone else's recipient was removed by naming it in the form or the query string")
	}
	if _, exists := st.recipients["own"]; exists {
		t.Error("the caller's own recipient should have been the one removed")
	}
}

// notifyUnlinked's own best-effort reply to the chat that was just unlinked,
// exercised with a fixture that actually has a bot behind it -- s.bot is nil
// in every other test in this file, which would let this whole code path
// (and linkRemovedReply) be deleted without failing anything.
func TestNotificationsUnlinkNotifiesTheOldChat(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	st.recipients["own"] = models.Recipient{
		ID: "own", Subject: "tester", Name: "Jens",
		ConversationID: "conv-1", ServiceURL: "https://smba.example/", BotChannelID: "msteams",
	}
	fb := &fakeBotClient{}
	handler := notificationsServerWithBot(t, st, &fakeMessenger{}, fb).Handler

	rec := postForm(t, handler, "/admin/notifications/unlink", url.Values{}, nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d, want a redirect: %s", rec.Code, rec.Body.String())
	}
	if got := fb.count(linkRemovedReply); got != 1 {
		t.Errorf("bot notices with linkRemovedReply's text = %d, want exactly 1", got)
	}
}

// Cross-origin form posts are refused the same way every other admin form is:
// there is no session-bound CSRF token, only proof of where the request came
// from.
func TestNotificationsFormPostsRefuseCrossOrigin(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/admin/notifications/link", "/admin/notifications/cancel", "/admin/notifications/unlink"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			st := sessionAs(newFakeStore(), authz.RoleViewer)
			handler := notificationsServer(t, st, &fakeMessenger{}).Handler

			rec := postForm(t, handler, path, url.Values{}, map[string]string{"Sec-Fetch-Site": "cross-site"})
			if rec.Code != http.StatusForbidden {
				t.Errorf("cross-site POST %s = %d, want 403", path, rec.Code)
			}
			if len(st.linkFlows) != 0 || len(st.recipients) != 0 {
				t.Errorf("cross-site POST %s mutated the store", path)
			}
		})
	}
}

// The method guards on the page and on mint are production code with nothing
// else exercising them: no other test issues a non-GET to the page, or a GET
// to the mint action.
func TestNotificationsMethodGuards(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	for _, tt := range []struct{ method, path string }{
		{http.MethodPost, "/admin/notifications"},
		{http.MethodGet, "/admin/notifications/link"},
		{http.MethodGet, "/admin/notifications/cancel"},
		{http.MethodGet, "/admin/notifications/unlink"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, handler, tt.method, tt.path, "")
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// Every response here can show a live code, or who currently holds one, so it
// must never be cached or replayed from history.
func TestNotificationsResponsesAreNeverCached(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := notificationsServer(t, st, &fakeMessenger{}).Handler

	get := asRole(t, handler, http.MethodGet, "/admin/notifications", "")
	if got := get.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("GET Cache-Control = %q, want no-store", got)
	}
	if got := get.Header().Get("Pragma"); got != "no-cache" {
		t.Errorf("GET Pragma = %q, want no-cache", got)
	}

	mint := postForm(t, handler, "/admin/notifications/link", url.Values{}, nil)
	if got := mint.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("mint Cache-Control = %q, want no-store", got)
	}
}

// The whole page, the nav link (checked in TestNotificationsNavLinkFollowsBotConfiguration
// below) and its two actions exist only when the bot is configured: a code
// minted here can never be redeemed on a deployment that never registered the
// endpoint it would be typed into.
func TestNotificationsRoutesAreGatedOnBotConfiguration(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleViewer)
	handler := newTestServer(t, st, &fakeMessenger{}).Handler // bot left unconfigured

	for _, tt := range []struct{ method, path string }{
		{http.MethodGet, "/admin/notifications"},
		{http.MethodPost, "/admin/notifications/link"},
		{http.MethodPost, "/admin/notifications/cancel"},
		{http.MethodPost, "/admin/notifications/unlink"},
	} {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			t.Parallel()

			rec := asRole(t, handler, tt.method, tt.path, "")
			if rec.Code != http.StatusNotFound {
				t.Errorf("%s %s with the bot unconfigured = %d, want 404 (no route registered)", tt.method, tt.path, rec.Code)
			}
		})
	}
}

// The nav link follows the same switch: offering it when the route behind it
// does not exist would send someone to mint a code for a bot that isn't
// there.
func TestNotificationsNavLinkFollowsBotConfiguration(t *testing.T) {
	t.Parallel()

	without := asRole(t, newTestServer(t, sessionAs(newFakeStore(), authz.RoleViewer), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if strings.Contains(without, `href="/admin/notifications"`) {
		t.Error("the nav offers notifications when the bot is not configured")
	}

	with := asRole(t, notificationsServer(t, sessionAs(newFakeStore(), authz.RoleViewer), &fakeMessenger{}).Handler, http.MethodGet, "/admin", "").Body.String()
	if !strings.Contains(with, `href="/admin/notifications"`) {
		t.Error("the nav does not offer notifications when the bot is configured")
	}
}
