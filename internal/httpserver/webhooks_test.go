package httpserver

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestHashFingerprintDeterministic(t *testing.T) {
	baseTime := time.Date(2025, 12, 7, 20, 7, 0, 0, time.UTC)

	alertA := models.Alert{
		Source:    "universal",
		Generator: "custom",
		StartsAt:  baseTime,
		Labels: map[string]string{
			"severity":  "critical",
			"alertname": "HighCPU",
		},
	}
	alertB := models.Alert{
		Source:    "universal",
		Generator: "custom",
		StartsAt:  baseTime,
		Labels: map[string]string{
			"alertname": "HighCPU",
			"severity":  "critical",
		},
	}

	hashA := hashFingerprint(alertA)
	hashB := hashFingerprint(alertB)

	if hashA != hashB {
		t.Fatalf("expected deterministic hash, got %s and %s", hashA, hashB)
	}
}

func seededServer(t *testing.T, msg *fakeMessenger) (*fakeStore, http.Handler) {
	t.Helper()

	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"{{ .Alert.Status }}"}`}
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel"}
	st.routes["route"] = models.Route{ID: "route", TemplateID: "tmpl", DestinationID: "dest", IsDefault: true}

	return st, newTestServer(t, st, msg).Handler
}

func postWebhook(t *testing.T, handler http.Handler, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Teamster-Token", token)
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestWebhookAuthAndMethod(t *testing.T) {
	t.Parallel()

	_, handler := seededServer(t, &fakeMessenger{})

	tests := []struct {
		name       string
		method     string
		path       string
		token      string
		body       string
		wantStatus int
	}{
		{name: "alertmanager rejects GET", method: http.MethodGet, path: "/webhook/alertmanager", token: "token", wantStatus: http.StatusMethodNotAllowed},
		{name: "universal rejects GET", method: http.MethodGet, path: "/webhook/universal", token: "token", wantStatus: http.StatusMethodNotAllowed},
		{name: "alertmanager rejects a bad token", method: http.MethodPost, path: "/webhook/alertmanager", token: "wrong", body: "{}", wantStatus: http.StatusUnauthorized},
		{name: "universal rejects a bad token", method: http.MethodPost, path: "/webhook/universal", token: "wrong", body: "{}", wantStatus: http.StatusUnauthorized},
		{name: "alertmanager rejects invalid JSON", method: http.MethodPost, path: "/webhook/alertmanager", token: "token", body: "{", wantStatus: http.StatusBadRequest},
		{name: "universal rejects invalid JSON", method: http.MethodPost, path: "/webhook/universal", token: "token", body: "{", wantStatus: http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			req.Header.Set("X-Teamster-Token", tt.token)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestUniversalWebhookPostsANewCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{"alertname":"HighCPU"},"annotations":{"summary":"CPU spiking"},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if msg.posts[0].teamID != "team" || msg.posts[0].channelID != "channel" {
		t.Errorf("posted to %s/%s, want team/channel", msg.posts[0].teamID, msg.posts[0].channelID)
	}
	if msg.posts[0].msg.Title != "CPU spiking" {
		t.Errorf("title = %q, want the annotation", msg.posts[0].msg.Title)
	}
	if cardOf(msg.posts[0].msg) != `{"text":"firing"}` {
		t.Errorf("card = %s, want the rendered template", cardOf(msg.posts[0].msg))
	}

	active, ok := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]
	if !ok {
		t.Fatal("no active alert stored")
	}
	if active.MessageID != "graph-1" {
		t.Errorf("stored message ID = %q, want graph-1", active.MessageID)
	}
}

// A message with no status is /webhook/universal's baseline case: routed and
// rendered like an alert, but delivered once and tracked nowhere, because
// nothing about it says there will be a later post to find and edit.
func TestUniversalMessageWithoutAStatusPostsOnceAndIsNotTracked(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"app":"checkout"},"annotations":{"summary":"Deployment finished"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if msg.posts[0].msg.Title != "Deployment finished" {
		t.Errorf("title = %q, want the annotation", msg.posts[0].msg.Title)
	}

	if len(st.activeAlerts) != 0 {
		t.Errorf("active alerts = %d, want none: nothing is tracked for a status-less message", len(st.activeAlerts))
	}
}

// The core guarantee of the untracked path: without a fingerprint to claim
// against, a repeat post is a second message, not an edit of the first. This
// is what rules out reusing the claim-and-update path for the baseline case --
// two unrelated one-off messages that happened to compute the same fingerprint
// would otherwise silently overwrite each other's card.
func TestUniversalMessageWithoutAStatusPostsFreshEachTime(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	_, handler := seededServer(t, msg)

	body := `{"labels":{},"annotations":{"summary":"build finished"}}`
	for range 2 {
		rec := postWebhook(t, handler, "/webhook/universal", "token", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
	}

	if len(msg.posts) != 2 {
		t.Fatalf("posted %d messages, want 2 -- a second post, not an update of the first", len(msg.posts))
	}
	if len(msg.updates) != 0 {
		t.Errorf("updates = %d, want none", len(msg.updates))
	}
}

// A status the sender made up -- not "firing" or "resolved" -- is not an error
// any more: it takes the same untracked path an absent status does. Reuses the
// payload that used to be this test suite's "unknown status" failure case.
func TestUniversalMessageWithAnUnrecognizedStatusIsOneShot(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"flapping","labels":{},"fingerprint":"fp"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if len(st.activeAlerts) != 0 {
		t.Errorf("active alerts = %d, want none: an unrecognized status is not the alert lifecycle", len(st.activeAlerts))
	}
}

// testPostedAt stands in for "this card exists". A row carrying a message id
// must carry a posting time too -- the schema's CHECK says so.
var testPostedAt = time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

func TestRepeatedFiringAlertUpdatesTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = models.ActiveAlert{
		Fingerprint: "fp-1",
		Status:      "firing",
		TeamID:      "team",
		ChannelID:   "channel",
		MessageID:   "graph-1",
		PostedAt:    testPostedAt,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 0 {
		t.Errorf("posted %d new messages, want 0", len(msg.posts))
	}
	if len(msg.updates) != 1 || msg.updates[0].messageID != "graph-1" {
		t.Errorf("updates = %+v, want one update of graph-1", msg.updates)
	}
	if _, ok := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]; !ok {
		t.Error("active alert was removed, want it kept while firing")
	}
}

func TestResolvedAlertUpdatesAndClearsTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeAlerts[activeAlertKey("fp-1", "team", "channel")] = models.ActiveAlert{
		Fingerprint: "fp-1", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", PostedAt: testPostedAt,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"resolved","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(msg.updates))
	}
	if _, ok := st.activeAlerts[activeAlertKey("fp-1", "team", "channel")]; ok {
		t.Error("active alert still stored, want it deleted once resolved")
	}
}

func TestResolvedAlertWithoutAnActiveCardIsANoOp(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"resolved","labels":{},"fingerprint":"unknown"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200", rec.Code)
	}
	if len(msg.posts) != 0 || len(msg.updates) != 0 {
		t.Errorf("posts=%d updates=%d, want no Graph traffic", len(msg.posts), len(msg.updates))
	}
}

func TestAlertmanagerWebhookProcessesEveryAlert(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	body := `{"status":"firing","alerts":[
		{"status":"firing","labels":{"alertname":"A"},"fingerprint":"fp-a"},
		{"status":"firing","labels":{"alertname":"B"},"fingerprint":"fp-b"}
	]}`
	rec := postWebhook(t, handler, "/webhook/alertmanager", "token", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 2 {
		t.Errorf("posted %d messages, want 2", len(msg.posts))
	}
}

func TestAlertmanagerAlertWithoutAnnotationsUsesTheAlertname(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/alertmanager", "token",
		`{"alerts":[{"status":"firing","labels":{"alertname":"HighCPU"},"fingerprint":"fp"}]}`)

	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "HighCPU" {
		t.Errorf("summary = %+v, want it to fall back to the alertname", msg.posts)
	}
}

func TestAlertWithoutSummaryOrAlertnameGetsAGenericSummary(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp"}`)

	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "Alert update" {
		t.Errorf("summary = %+v, want the generic fallback", msg.posts)
	}
}

// A template is free to send a titled text message and no card at all, which is
// the case the Teams activity feed reads best.
func TestTemplateWithoutACardPostsTextOnly(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.templates["tmpl"] = models.Template{
		ID:    "tmpl",
		Title: "{{ .Alert.Labels.alertname }} {{ .Alert.Status }}",
		Text:  "<p>{{ .Alert.Annotations.summary }}</p><script>steal()</script>",
	}

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"alertname":"HighCPU"},"annotations":{"summary":"CPU spiking"},"fingerprint":"fp"}`)

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	posted := msg.posts[0].msg
	if posted.Title != "HighCPU firing" {
		t.Errorf("title = %q, want the rendered template", posted.Title)
	}
	if posted.Text != "<p>CPU spiking</p>" {
		t.Errorf("text = %q, want it sanitized before it leaves the service", posted.Text)
	}
	if len(posted.Cards) != 0 {
		t.Errorf("cards = %s, want none", posted.Cards)
	}
}

func TestAlertWithoutFingerprintGetsAHashedOne(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{"alertname":"HighCPU"}}`)

	if len(st.activeAlerts) != 1 {
		t.Fatalf("stored %d active alerts, want 1", len(st.activeAlerts))
	}
	for _, active := range st.activeAlerts {
		if len(active.Fingerprint) != 64 {
			t.Errorf("fingerprint = %q, want a SHA-256 hex digest", active.Fingerprint)
		}
	}
}

func TestProcessAlertFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		setup   func(*fakeStore, *fakeMessenger)
		wantErr string
	}{
		{
			name:    "no route matches",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.routes, "route") },
			wantErr: "no routes configured",
		},
		{
			name:    "template is missing",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.templates, "tmpl") },
			wantErr: "template:",
		},
		{
			name:    "destination is missing",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.destinations, "dest") },
			wantErr: "destination:",
		},
		{
			name: "template does not render",
			body: `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup: func(st *fakeStore, _ *fakeMessenger) {
				st.templates["tmpl"] = models.Template{ID: "tmpl", Body: "{{"}
			},
			wantErr: "render:",
		},
		{
			name:    "graph rejects the post",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(_ *fakeStore, msg *fakeMessenger) { msg.postErr = errors.New("graph down") },
			wantErr: "graph post:",
		},
		{
			name:    "graph rejects the one-shot post",
			body:    `{"labels":{},"fingerprint":"fp"}`,
			setup:   func(_ *fakeStore, msg *fakeMessenger) { msg.postErr = errors.New("graph down") },
			wantErr: "graph post:",
		},
		{
			name: "graph rejects the update",
			body: `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeAlerts[activeAlertKey("fp", "team", "channel")] = models.ActiveAlert{
					Fingerprint: "fp", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", PostedAt: testPostedAt,
				}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "graph update:",
		},
		{
			name: "graph rejects the resolve update",
			body: `{"status":"resolved","labels":{},"fingerprint":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeAlerts[activeAlertKey("fp", "team", "channel")] = models.ActiveAlert{
					Fingerprint: "fp", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", PostedAt: testPostedAt,
				}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "graph update:",
		},
		{
			name:    "claiming the card fails while firing",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("ClaimActiveAlert") },
			wantErr: "claim active alert:",
		},
		{
			name:    "active alert lookup fails while resolving",
			body:    `{"status":"resolved","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("ListActiveAlerts") },
			wantErr: "active alert lookup:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{}
			st, handler := seededServer(t, msg)
			if tt.setup != nil {
				tt.setup(st, msg)
			}

			rec := postWebhook(t, handler, "/webhook/universal", "token", tt.body)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("POST = %d, want 502 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.wantErr) {
				t.Errorf("body = %s, want it to contain %q", rec.Body.String(), tt.wantErr)
			}
		})
	}
}

// nestedServer seeds a parent route with a child that sends the same alert to a
// second channel, which is the whole point of the tree.
func nestedServer(t *testing.T, msg *fakeMessenger, greedy bool) (*fakeStore, http.Handler) {
	t.Helper()

	st, handler := seededServer(t, msg)
	st.routes["parent"] = models.Route{
		ID: "parent", Name: "parent", TemplateID: "tmpl", DestinationID: "dest",
		LabelSelector: map[string]string{"severity": "critical"}, Priority: 100,
	}
	st.destinations["escalation"] = models.Destination{ID: "escalation", TeamID: "team", ChannelID: "escalation-channel"}
	st.routes["child"] = models.Route{
		ID: "child", Name: "child", ParentID: "parent", Greedy: greedy,
		LabelSelector: map[string]string{"team": "payments"}, DestinationID: "escalation",
	}
	return st, handler
}

func TestNestedRoutesFanOut(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := nestedServer(t, msg, false)

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`)

	if len(msg.posts) != 2 {
		t.Fatalf("posted %d messages, want one per channel: %+v", len(msg.posts), msg.posts)
	}
	channels := map[string]bool{}
	for _, post := range msg.posts {
		channels[post.channelID] = true
	}
	if !channels["channel"] || !channels["escalation-channel"] {
		t.Errorf("channels = %v, want the parent's and the child's", channels)
	}
	// One card per channel, so resolving later can update both.
	if len(st.activeAlerts) != 2 {
		t.Errorf("stored %d cards, want one per channel", len(st.activeAlerts))
	}
}

// Two independent root routes -- no parent/child between them, unlike every
// fan-out test above -- both selecting on the same label both deliver: the
// headline behavior, proven end to end rather than only inside the router
// package.
func TestIndependentRoutesBothFire(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"{{ .Alert.Status }}"}`}
	st.destinations["ops"] = models.Destination{ID: "ops", TeamID: "team", ChannelID: "ops-channel"}
	st.destinations["audit"] = models.Destination{ID: "audit", TeamID: "team", ChannelID: "audit-channel"}
	st.routes["ops"] = models.Route{
		ID: "ops", Name: "ops", TemplateID: "tmpl", DestinationID: "ops",
		LabelSelector: map[string]string{"team": "payments"}, Priority: 100,
	}
	st.routes["audit"] = models.Route{
		ID: "audit", Name: "audit", TemplateID: "tmpl", DestinationID: "audit",
		LabelSelector: map[string]string{"team": "payments"}, Priority: 50,
	}
	handler := newTestServer(t, st, msg).Handler

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"team":"payments"},"fingerprint":"fp"}`)

	if len(msg.posts) != 2 {
		t.Fatalf("posted %d messages, want one per matching route: %+v", len(msg.posts), msg.posts)
	}
	channels := map[string]bool{}
	for _, post := range msg.posts {
		channels[post.channelID] = true
	}
	if !channels["ops-channel"] || !channels["audit-channel"] {
		t.Errorf("channels = %v, want both routes' own", channels)
	}
	if len(st.activeAlerts) != 2 {
		t.Errorf("stored %d cards, want one per channel", len(st.activeAlerts))
	}
}

func TestGreedyChildTakesDeliveryFromItsParent(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := nestedServer(t, msg, true)

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`)

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want only the child's: %+v", len(msg.posts), msg.posts)
	}
	if msg.posts[0].channelID != "escalation-channel" {
		t.Errorf("channel = %q, want the child's", msg.posts[0].channelID)
	}
}

// A child that only names a destination renders with its parent's template.
func TestChildInheritsTheParentTemplate(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := nestedServer(t, msg, false)

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`)

	for _, post := range msg.posts {
		if cardOf(post.msg) != `{"text":"firing"}` {
			t.Errorf("card in %s = %s, want the inherited template's", post.channelID, cardOf(post.msg))
		}
	}
}

func TestResolvingClearsEveryCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := nestedServer(t, msg, false)

	firing := `{"status":"firing","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`
	postWebhook(t, handler, "/webhook/universal", "token", firing)
	postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"resolved","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`)

	if len(msg.updates) != 2 {
		t.Fatalf("updated %d cards, want both: %+v", len(msg.updates), msg.updates)
	}
	if len(st.activeAlerts) != 0 {
		t.Errorf("cards still stored = %+v, want all of them cleared", st.activeAlerts)
	}
}

// One channel refusing the message must not cost the other one its card, and
// the failure still has to be reported.
func TestOneFailedDeliveryDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{postErrFor: "escalation-channel", postErr: errors.New("graph down")}
	st, handler := nestedServer(t, msg, false)

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"status":"firing","labels":{"severity":"critical","team":"payments"},"fingerprint":"fp"}`)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("POST = %d, want 502 so the sender retries", rec.Code)
	}
	if len(st.activeAlerts) != 1 {
		t.Fatalf("stored %d cards, want the one that was posted", len(st.activeAlerts))
	}
	for _, card := range st.activeAlerts {
		if card.ChannelID != "channel" {
			t.Errorf("stored card is for %q, want the channel that accepted it", card.ChannelID)
		}
	}
}
