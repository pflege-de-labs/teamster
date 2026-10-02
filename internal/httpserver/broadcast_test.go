package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// broadcastFixture is the addressed fixture with a token that may broadcast,
// a linked recipient the directory does not know, and one who is Alice.
func broadcastFixture(t *testing.T) addressedFixture {
	t.Helper()
	f := newAddressedFixture(t, nil)
	f.issue(t, "tst_all", "admin", authz.MessagesEveryone)
	f.store.recipients["r-dan"] = models.Recipient{ID: "r-dan", Subject: "dan", ConversationID: "a:dan", ServiceURL: "https://smba.example/", TenantID: "t"}
	f.store.recipients["r-alice"] = models.Recipient{ID: "r-alice", Subject: "alice", AADObjectID: "oid-alice", ConversationID: "a:alice-linked", ServiceURL: "https://smba.example/", TenantID: "t"}
	for _, u := range f.store.directory {
		u.Eligible = true
		f.store.directory[u.AADObjectID] = u
	}
	return f
}

// conversations are the chats messages went to, sorted.
func (f addressedFixture) conversations() []string {
	var out []string
	for _, s := range f.sentTo() {
		conversation, _, _ := strings.Cut(s, ": ")
		out = append(out, conversation)
	}
	return out
}

const broadcastBody = `{"labels":{"kind":"password"},"text":"All hands at noon","broadcast":true}`

func TestBroadcastRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setup      func(t *testing.T, f addressedFixture)
		token      string
		body       string
		wantStatus int
		wantError  string
	}{
		{"with recipients", nil, "tst_all", `{"labels":{"kind":"password"},"broadcast":true,"recipients":["bob@corp.example"]}`, http.StatusBadRequest, "drop recipients"},
		{"with the label", nil, "tst_all", `{"labels":{"kind":"password","teamster_recipient":"bob@corp.example"},"broadcast":true}`, http.StatusBadRequest, "drop recipients"},
		{"with a state", nil, "tst_all", `{"state":"open","labels":{"kind":"password"},"broadcast":true}`, http.StatusBadRequest, "drop state"},
		{"the deployment token", nil, "token", broadcastBody, http.StatusForbidden, "may not name recipients"},
		{"a token that may name anyone", nil, senderToken, broadcastBody, http.StatusForbidden, "may not broadcast"},
		{
			name: "an everyone token whose creator lost the grant",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.users["olga"] = models.User{Subject: "olga", Source: "oidc", Roles: []string{"editor"}}
				f.issue(t, "tst_olga", "olga", authz.MessagesEveryone)
			},
			token: "tst_olga", body: broadcastBody, wantStatus: http.StatusForbidden, wantError: "may not broadcast",
		},
		{
			name: "no route addresses people",
			setup: func(t *testing.T, f addressedFixture) {
				f.store.routes["pw"] = models.Route{ID: "pw", Name: "Passwords", RecipientID: "r-dan", LabelSelector: map[string]string{"kind": "password"}}
			},
			token: "tst_all", body: broadcastBody, wantStatus: http.StatusUnprocessableEntity, wantError: reasonNoAddressedRoute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := broadcastFixture(t)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			rec := postWebhook(t, f.handler, "/webhook/universal", tt.token, tt.body)
			if rec.Code != tt.wantStatus || !strings.Contains(rec.Body.String(), tt.wantError) {
				t.Fatalf("status = %d %s, want %d saying %q", rec.Code, rec.Body.String(), tt.wantStatus, tt.wantError)
			}
			if len(f.store.broadcasts) != 0 {
				t.Errorf("a refused broadcast was queued: %v", f.store.broadcasts)
			}
		})
	}
}

func TestBroadcastIsQueuedAndDelivered(t *testing.T) {
	t.Parallel()

	f := broadcastFixture(t)
	rec := postWebhook(t, f.handler, "/webhook/universal", "tst_all", broadcastBody)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("broadcast = %d %s, want 202", rec.Code, rec.Body.String())
	}
	var answer broadcastAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Status != "accepted" || answer.Broadcast.State != models.RunRequested || answer.StatusURL != "/webhook/broadcasts/"+answer.Broadcast.ID {
		t.Fatalf("answer = %+v", answer)
	}
	if got := f.sentTo(); len(got) != 0 {
		t.Fatalf("sent before the run: %v", got)
	}

	// Alice through the directory only, Bob, and Dan through his link; Carol has no chat.
	f.bot.errFor = map[string]error{"a:bob": &bot.APIError{Code: "MessageWritesBlocked"}}
	ran, err := f.server.broadcastStep(t.Context(), "replica-1")
	if !ran || err != nil {
		t.Fatalf("broadcastStep = %v, %v", ran, err)
	}
	want := []string{"a:alice", "a:bob", "a:dan"}
	if got := f.conversations(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("sent = %v, want %v", got, want)
	}
	b := f.store.broadcasts[answer.Broadcast.ID]
	if b.State != models.RunDone || b.Total != 3 || b.Delivered != 2 || b.Unreachable != 1 || b.Failed != 0 || b.Cursor != "rcp:r-dan" {
		t.Errorf("broadcast = %+v", b)
	}
	if ran, _ := f.server.broadcastStep(t.Context(), "replica-1"); ran {
		t.Error("a finished broadcast ran again")
	}

	// The status is the creator's tokens' to read, and nobody else's.
	status := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, answer.StatusURL, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		f.handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := status(senderToken); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"delivered":2`) {
		t.Errorf("status for the creator = %d %s", rec.Code, rec.Body.String())
	}
	f.store.users["olga"] = models.User{Subject: "olga", Source: "oidc", Roles: []string{"editor"}}
	f.issue(t, "tst_olga", "olga", authz.MessagesNone)
	if rec := status("tst_olga"); rec.Code != http.StatusNotFound {
		t.Errorf("status for someone else = %d", rec.Code)
	}
	if rec := status("token"); rec.Code != http.StatusNotFound {
		t.Errorf("status for the deployment token = %d", rec.Code)
	}
	if rec := status("nope"); rec.Code != http.StatusUnauthorized {
		t.Errorf("status without a valid token = %d", rec.Code)
	}
}

func TestBroadcastResumesAndYields(t *testing.T) {
	t.Parallel()

	f := broadcastFixture(t)
	now := time.Now().UTC()
	queued := func(id string, mutate func(*models.Broadcast)) {
		event, _ := json.Marshal(models.Event{Source: models.SourceUniversal, Text: "Hi", Universal: &models.UniversalEvent{Broadcast: true}})
		b := models.Broadcast{ID: id, RequestedBy: "admin", RequestedAt: now, State: models.RunRequested, Event: event, Plan: []byte(`[{"route_id":"pw","route_name":"Passwords","kind":"addressed","template_id":""}]`)}
		mutate(&b)
		f.store.broadcasts[id] = b
	}

	// A broadcast whose owner went quiet after Alice carries on from Bob.
	queued("stale", func(b *models.Broadcast) {
		b.State, b.Owner, b.HeartbeatAt, b.Cursor = models.RunRunning, "gone", now.Add(-time.Hour), "aad:oid-alice"
		b.Delivered = 1
	})
	if ran, err := f.server.broadcastStep(t.Context(), "replica-2"); !ran || err != nil {
		t.Fatalf("takeover = %v, %v", ran, err)
	}
	if got := f.conversations(); strings.Join(got, "|") != "a:bob|a:dan" {
		t.Errorf("sent after the takeover = %v", got)
	}
	if b := f.store.broadcasts["stale"]; b.State != models.RunDone || b.Delivered != 3 || b.Owner != "replica-2" {
		t.Errorf("taken-over broadcast = %+v", b)
	}

	// One running with a fresh heartbeat is somebody else's.
	queued("busy", func(b *models.Broadcast) { b.State, b.Owner, b.HeartbeatAt = models.RunRunning, "other", now })
	if ran, _ := f.server.broadcastStep(t.Context(), "replica-2"); ran {
		t.Error("a live broadcast was taken over")
	}
	delete(f.store.broadcasts, "busy")

	// Progress that cannot be recorded leaves it running, to be carried on.
	queued("paused", func(*models.Broadcast) {})
	f.store.fail("HeartbeatBroadcast")
	if _, err := f.server.broadcastStep(t.Context(), "replica-2"); !errors.Is(err, errBroadcastPaused) {
		t.Errorf("a failed heartbeat = %v, want paused", err)
	}
	if b := f.store.broadcasts["paused"]; b.State != models.RunRunning {
		t.Errorf("paused broadcast = %s, want still running", b.State)
	}
	delete(f.store.broadcasts, "paused")

	// A broadcast nothing can decode fails, and says why.
	queued("broken", func(b *models.Broadcast) { b.Plan = []byte("{") })
	if _, err := f.server.broadcastStep(t.Context(), "replica-2"); err == nil {
		t.Error("an undecodable broadcast succeeded")
	}
	if b := f.store.broadcasts["broken"]; b.State != models.RunFailed || !strings.Contains(b.LastError, "decode broadcast plan") {
		t.Errorf("broken broadcast = %+v", b)
	}
}

func TestBroadcastLosesTheLease(t *testing.T) {
	t.Parallel()

	f := broadcastFixture(t)
	if rec := postWebhook(t, f.handler, "/webhook/universal", "tst_all", broadcastBody); rec.Code != http.StatusAccepted {
		t.Fatalf("broadcast = %d", rec.Code)
	}
	var id string
	for k := range f.store.broadcasts {
		id = k
	}
	// Another replica takes it over while the first is sending.
	f.bot.errFor = map[string]error{}
	f.store.mu.Lock()
	b := f.store.broadcasts[id]
	f.store.mu.Unlock()
	claimed, _ := f.store.ClaimBroadcast(t.Context(), id, "replica-1", time.Now(), time.Now())
	if !claimed {
		t.Fatal("claim")
	}
	f.store.mu.Lock()
	b = f.store.broadcasts[id]
	b.Owner = "replica-2"
	f.store.broadcasts[id] = b
	f.store.mu.Unlock()
	b.Owner = "replica-1"
	if err := f.server.executeBroadcast(t.Context(), b, "replica-1"); err != nil {
		t.Errorf("a lost lease = %v, want a quiet stop", err)
	}
	if got := f.store.broadcasts[id]; got.State != models.RunRunning || got.Owner != "replica-2" {
		t.Errorf("after losing the lease = %+v", got)
	}
}

func TestBroadcastsPage(t *testing.T) {
	t.Parallel()

	seed := func(st *fakeStore) {
		st.broadcasts["mine"] = models.Broadcast{ID: "mine", RequestedBy: "tester", TokenName: "notices", RequestedAt: time.Now(), State: models.RunDone, BroadcastCounts: models.BroadcastCounts{Total: 4, Delivered: 3, Failed: 1}}
		st.broadcasts["theirs"] = models.Broadcast{ID: "theirs", RequestedBy: "olga", RequestedAt: time.Now(), State: models.RunFailed, LastError: "boom"}
	}
	admin := sessionAs(newFakeStore(), authz.RoleAdmin)
	seed(admin)
	h := newTestServer(t, admin, &fakeMessenger{}).Handler
	body := call(t, h, http.MethodGet, "/admin/broadcasts", "").Body.String()
	for _, want := range []string{"mine", "theirs", "3 of 4 delivered", "boom"} {
		if !strings.Contains(body, want) {
			t.Errorf("the admin's page lacks %q", want)
		}
	}

	viewer := sessionAs(newFakeStore(), authz.RoleViewer)
	seed(viewer)
	h = newTestServer(t, viewer, &fakeMessenger{}).Handler
	rec := call(t, h, http.MethodGet, "/api/broadcasts", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mine"`) || strings.Contains(rec.Body.String(), "theirs") {
		t.Errorf("GET /api/broadcasts as a viewer = %d %s, want only their own", rec.Code, rec.Body.String())
	}
	if rec := call(t, h, http.MethodPost, "/api/broadcasts", ""); rec.Code != http.StatusForbidden && rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rec.Code)
	}
}

func TestRunBroadcastsStopsWithItsContext(t *testing.T) {
	t.Parallel()

	f := broadcastFixture(t)
	var run func(context.Context)
	WithBroadcasts(&run)(f.server)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		run(ctx)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runBroadcasts did not stop")
	}
}

func TestTheNavOffersBroadcastsToBroadcasters(t *testing.T) {
	t.Parallel()

	st := sessionAs(newFakeStore(), authz.RoleAdmin)
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
		Bot:     notificationsBotConfig(),
	}
	h := mustServer(t, cfg, st, &fakeMessenger{}).Handler
	if body := call(t, h, http.MethodGet, "/admin/me", "").Body.String(); !strings.Contains(body, `href="/admin/broadcasts"`) {
		t.Error("an admin with a bot is not offered broadcasts")
	}
}
