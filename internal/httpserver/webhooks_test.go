package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func TestDeriveKeyIgnoresLabelOrder(t *testing.T) {
	t.Parallel()

	baseTime := time.Date(2025, 12, 7, 20, 7, 0, 0, time.UTC)
	evA := models.Event{
		Source:    "universal",
		Labels:    map[string]string{"severity": "critical", "alertname": "HighCPU"},
		Universal: &models.UniversalEvent{URL: "custom", Time: baseTime},
	}
	evB := models.Event{
		Source:    "universal",
		Labels:    map[string]string{"alertname": "HighCPU", "severity": "critical"},
		Universal: &models.UniversalEvent{URL: "custom", Time: baseTime},
	}

	if keyA, keyB := deriveKey(evA), deriveKey(evB); keyA != keyB {
		t.Fatalf("expected deterministic key, got %s and %s", keyA, keyB)
	}
}

// The wanted values are what hashFingerprint produced before the event model,
// so a card posted by the previous release is still found by its key.
func TestDeriveKeyMatchesThePreviousRelease(t *testing.T) {
	t.Parallel()

	at := time.Date(2025, 12, 7, 20, 7, 0, 0, time.UTC)
	labels := map[string]string{"alertname": "HighCPU", "severity": "critical"}

	tests := []struct {
		name string
		ev   models.Event
		want string
	}{
		{
			name: "universal",
			ev: models.Event{
				Source:    models.SourceUniversal,
				Labels:    labels,
				Universal: &models.UniversalEvent{URL: "https://example.com/gen", Time: at},
			},
			want: "a477e1342ae8e0f6e12d2219a9c18f32767497e3771047d97c3596073da851c2",
		},
		{
			name: "alertmanager",
			ev: models.Event{
				Source:       models.SourceAlertmanager,
				Labels:       labels,
				Alertmanager: &models.AlertmanagerEvent{GeneratorURL: "https://prom.example.com/graph", StartsAt: at},
			},
			want: "48f629981ca91130d3285930d979515a7e47f27cabee1fe169f13367e5e9403c",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := deriveKey(tt.ev); got != tt.want {
				t.Fatalf("deriveKey = %s, want %s", got, tt.want)
			}
		})
	}
}

func seededServer(t *testing.T, msg *fakeMessenger) (*fakeStore, http.Handler) {
	t.Helper()

	st := seededStore()
	return st, newTestServer(t, st, msg).Handler
}

func seededStore() *fakeStore {
	st := newFakeStore()
	st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"{{ .Event.State }}"}`}
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel"}
	st.routes["route"] = models.Route{ID: "route", TemplateID: "tmpl", DestinationID: "dest", IsDefault: true}
	return st
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

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"state":"open","labels":{"alertname":"HighCPU"},"attributes":{"summary":"CPU spiking"},"key":"fp-1"}`)
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
		t.Errorf("title = %q, want the summary attribute", msg.posts[0].msg.Title)
	}
	if cardOf(msg.posts[0].msg) != `{"text":"open"}` {
		t.Errorf("card = %s, want the rendered template", cardOf(msg.posts[0].msg))
	}

	active, ok := st.activeEvents[activeEventKey("fp-1", "team", "channel")]
	if !ok {
		t.Fatal("no active event stored")
	}
	if active.MessageID != "graph-1" {
		t.Errorf("stored message ID = %q, want graph-1", active.MessageID)
	}
}

// A message with no state is /webhook/universal's baseline case: routed and
// rendered like any event, but delivered once and tracked nowhere, because
// nothing about it says there will be a later post to find and edit.
func TestUniversalMessageWithoutAStatePostsOnceAndIsNotTracked(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"app":"checkout"},"attributes":{"summary":"Deployment finished"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if msg.posts[0].msg.Title != "Deployment finished" {
		t.Errorf("title = %q, want the summary attribute", msg.posts[0].msg.Title)
	}

	if len(st.activeEvents) != 0 {
		t.Errorf("active events = %d, want none: nothing is tracked for a stateless message", len(st.activeEvents))
	}
}

// The core guarantee of the untracked path: without a key to claim
// against, a repeat post is a second message, not an edit of the first. This
// is what rules out reusing the claim-and-update path for the baseline case --
// two unrelated one-off messages that happened to compute the same key
// would otherwise silently overwrite each other's card.
func TestUniversalMessageWithoutAStatePostsFreshEachTime(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	_, handler := seededServer(t, msg)

	body := `{"labels":{},"attributes":{"summary":"build finished"}}`
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

// A state outside the lifecycle is refused rather than delivered once, so a
// sender still speaking Alertmanager's firing/resolved finds out.
func TestUniversalWebhookRejectsAnUnknownState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state string
	}{
		{name: "alertmanager vocabulary", state: "firing"},
		{name: "made up", state: "flapping"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "graph-1"}
			st, handler := seededServer(t, msg)

			rec := postWebhook(t, handler, "/webhook/universal", "token", `{"state":"`+tt.state+`","labels":{},"key":"fp"}`)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("POST = %d, want 400 (body %s)", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tt.state) {
				t.Errorf("body = %s, want it to name the state", rec.Body.String())
			}
			if len(msg.posts) != 0 || len(st.activeEvents) != 0 {
				t.Errorf("posts = %d, active events = %d, want nothing delivered", len(msg.posts), len(st.activeEvents))
			}
		})
	}
}

func TestUniversalWebhookDeliversDirectContentWithoutATemplate(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)
	st.routes["direct"] = models.Route{
		ID:            "direct",
		LabelSelector: map[string]string{"team": "direct"},
		DestinationID: "dest",
		Priority:      10,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"open","labels":{"team":"direct"},"key":"fp-direct",`+
			`"title":"Disk full","text":"**disk** almost full","card":{"text":"raw card"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if msg.posts[0].msg.Title != "Disk full" {
		t.Errorf("title = %q, want the payload's title", msg.posts[0].msg.Title)
	}
	if msg.posts[0].msg.Text != "<p><strong>disk</strong> almost full</p>" {
		t.Errorf("text = %q, want the sanitized markdown", msg.posts[0].msg.Text)
	}
	if cardOf(msg.posts[0].msg) != `{"text":"raw card"}` {
		t.Errorf("card = %s, want the payload's card passed through", cardOf(msg.posts[0].msg))
	}
	if cards := msg.posts[0].msg.Cards; len(cards) != 2 || !strings.Contains(string(cards[1]), "No template is defined") {
		t.Errorf("cards = %s, want the payload's card followed by the hint", cards)
	}
}

// A message no route claims, with no default route either, lands in the
// global default destination instead of being rejected.
func TestUniversalWebhookFallsBackToTheGlobalDefault(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	st, handler := seededServer(t, msg)
	delete(st.routes, "route")
	st.destinations["dest"] = models.Destination{ID: "dest", TeamID: "team", ChannelID: "channel", IsDefault: true}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"labels":{"team":"nobody"},"title":"Unclaimed"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 1 || msg.posts[0].channelID != "channel" {
		t.Fatalf("posts = %+v, want one to the global default", msg.posts)
	}
	if msg.posts[0].msg.Title != "Unclaimed" {
		t.Errorf("title = %q, want the payload's", msg.posts[0].msg.Title)
	}
}

func TestUniversalWebhookTemplateWinsOverDirectContent(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{messageID: "graph-1"}
	_, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"open","labels":{"alertname":"HighCPU"},"key":"fp-both",`+
			`"title":"Ignored title","text":"ignored text","card":{"text":"ignored card"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	if cardOf(msg.posts[0].msg) != `{"text":"open"}` {
		t.Errorf("card = %s, want the route's template rendered, not the payload's card", cardOf(msg.posts[0].msg))
	}
}

// A message with no template and nothing direct to send gets the built-in
// default rather than a 502, followed by the hint card.
func TestUntemplatedMessageGetsTheBuiltInDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		externalURL string
		wantLink    string
	}{
		{name: "with an external URL", externalURL: "https://teamster.example.com/", wantLink: `href="https://teamster.example.com/admin#templates"`},
		{name: "without one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{messageID: "graph-1"}
			st, _ := seededServer(t, msg)
			st.routes["direct"] = models.Route{
				ID: "direct", LabelSelector: map[string]string{"team": "direct"}, DestinationID: "dest", Priority: 10,
			}
			cfg := config.Config{
				Server:  config.ServerConfig{Addr: ":0", ExternalURL: tt.externalURL},
				Webhook: config.WebhookConfig{Token: "token"},
				Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
			}
			handler := mustServer(t, cfg, st, msg).Handler

			rec := postWebhook(t, handler, "/webhook/universal", "token",
				`{"state":"open","labels":{"team":"direct","alertname":"DiskFull"},"attributes":{"description":"disk *almost* full"},"key":"fp-empty"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
			}
			if len(msg.posts) != 1 {
				t.Fatalf("posted %d messages, want 1", len(msg.posts))
			}
			posted := msg.posts[0].msg
			if posted.Title != "DiskFull" {
				t.Errorf("title = %q, want the alertname", posted.Title)
			}
			for _, want := range []string{"<strong>State:</strong> open", "disk <em>almost</em> full", "<pre><code", "&#34;alertname&#34;: &#34;DiskFull&#34;"} {
				if !strings.Contains(posted.Text, want) {
					t.Errorf("text = %q, want it to contain %q", posted.Text, want)
				}
			}
			// A channel post is one Teams message, so the hint follows the text as a line.
			if len(posted.Cards) != 0 {
				t.Fatalf("cards = %d, want none beside the text", len(posted.Cards))
			}
			hint := posted.Text
			if !strings.Contains(hint, "No template is defined") {
				t.Errorf("text = %s, want it to say no template is defined", hint)
			}
			if tt.wantLink != "" && !strings.Contains(hint, tt.wantLink) {
				t.Errorf("text = %s, want %s", hint, tt.wantLink)
			}
			if tt.wantLink == "" && strings.Contains(hint, "href=") {
				t.Errorf("text = %s, want no link without an external URL", hint)
			}
		})
	}
}

// testPostedAt stands in for "this card exists". A row carrying a message id
// must carry a posting time too -- the schema's CHECK says so.
var testPostedAt = time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

func TestRepeatedOpenEventUpdatesTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeEvents[activeEventKey("fp-1", "team", "channel")] = models.ActiveEvent{
		Key:       "fp-1",
		State:     models.StateOpen,
		TeamID:    "team",
		ChannelID: "channel",
		MessageID: "graph-1", ConversationID: "conversation-graph-1",
		PostedAt: testPostedAt,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"state":"open","labels":{},"key":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 0 {
		t.Errorf("posted %d new messages, want 0", len(msg.posts))
	}
	if len(msg.updates) != 1 || msg.updates[0].messageID != "graph-1" {
		t.Errorf("updates = %+v, want one update of graph-1", msg.updates)
	}
	if _, ok := st.activeEvents[activeEventKey("fp-1", "team", "channel")]; !ok {
		t.Error("active event was removed, want it kept while open")
	}
}

func TestClosedEventUpdatesAndClearsTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeEvents[activeEventKey("fp-1", "team", "channel")] = models.ActiveEvent{
		Key: "fp-1", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", ConversationID: "conversation-graph-1", PostedAt: testPostedAt,
	}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"state":"closed","labels":{},"key":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(msg.updates))
	}
	if _, ok := st.activeEvents[activeEventKey("fp-1", "team", "channel")]; ok {
		t.Error("active event still stored, want it deleted once closed")
	}
}

func TestClosedEventWithoutAnActiveCardIsANoOp(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"state":"closed","labels":{},"key":"unknown"}`)
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

// The group an alert arrived in reaches the template beside the alert itself.
func TestAlertmanagerGroupFieldsReachTheTemplate(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.templates["tmpl"] = models.Template{
		ID: "tmpl",
		Title: "{{ .Event.State }} {{ .Event.Alertmanager.Receiver }} {{ .Event.Alertmanager.GroupKey }} " +
			"{{ .Event.Alertmanager.GroupLabels.alertname }} {{ .Event.Alertmanager.CommonLabels.team }} " +
			"{{ .Event.Alertmanager.CommonAnnotations.runbook }} {{ .Event.Alertmanager.ExternalURL }} " +
			"{{ .Event.Alertmanager.Annotations.summary }} {{ .Event.Alertmanager.GeneratorURL }}",
	}

	rec := postWebhook(t, handler, "/webhook/alertmanager", "token", `{
		"receiver":"teamster","status":"firing","groupKey":"{}:{alertname=\"Disk\"}",
		"groupLabels":{"alertname":"Disk"},"commonLabels":{"team":"db"},
		"commonAnnotations":{"runbook":"rb"},"externalURL":"http://am.example",
		"alerts":[{"status":"firing","labels":{"alertname":"Disk","team":"db"},
			"annotations":{"summary":"disk full"},"generatorURL":"http://prom.example","fingerprint":"fp"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	want := `open teamster {}:{alertname="Disk"} Disk db rb http://am.example disk full http://prom.example`
	if got := msg.posts[0].msg.Title; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
	if _, ok := st.activeEvents[activeEventKey("fp", "team", "channel")]; !ok {
		t.Error("no active event under the Alertmanager fingerprint, want it used as the key")
	}
}

func TestEventWithoutSummaryOrAlertnameGetsAGenericSummary(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/universal", "token", `{"state":"open","labels":{},"key":"fp"}`)

	if len(msg.posts) != 1 || msg.posts[0].msg.Title != "Update" {
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
		Title: "{{ .Event.Labels.alertname }} {{ .Event.State }}",
		Text:  "<p>{{ .Event.Universal.Attributes.summary }}</p><script>steal()</script>",
	}

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"open","labels":{"alertname":"HighCPU"},"attributes":{"summary":"CPU spiking"},"key":"fp"}`)

	if len(msg.posts) != 1 {
		t.Fatalf("posted %d messages, want 1", len(msg.posts))
	}
	posted := msg.posts[0].msg
	if posted.Title != "HighCPU open" {
		t.Errorf("title = %q, want the rendered template", posted.Title)
	}
	if posted.Text != "<p>CPU spiking</p>" {
		t.Errorf("text = %q, want it sanitized before it leaves the service", posted.Text)
	}
	if len(posted.Cards) != 0 {
		t.Errorf("cards = %s, want none", posted.Cards)
	}
}

func TestEventWithoutAKeyGetsADerivedOne(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/universal", "token", `{"state":"open","labels":{"alertname":"HighCPU"}}`)

	if len(st.activeEvents) != 1 {
		t.Fatalf("stored %d active events, want 1", len(st.activeEvents))
	}
	for _, active := range st.activeEvents {
		if len(active.Key) != 64 {
			t.Errorf("key = %q, want a SHA-256 hex digest", active.Key)
		}
	}
}

func TestProcessEventFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		setup   func(*fakeStore, *fakeMessenger)
		wantErr string
	}{
		{
			name:    "no route matches",
			body:    `{"state":"open","labels":{},"key":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.routes, "route") },
			wantErr: "no routes configured",
		},
		{
			name:    "template is missing",
			body:    `{"state":"open","labels":{},"key":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.templates, "tmpl") },
			wantErr: "template:",
		},
		{
			name:    "destination is missing",
			body:    `{"state":"open","labels":{},"key":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { delete(st.destinations, "dest") },
			wantErr: "destination:",
		},
		{
			name: "template does not render",
			body: `{"state":"open","labels":{},"key":"fp"}`,
			setup: func(st *fakeStore, _ *fakeMessenger) {
				st.templates["tmpl"] = models.Template{ID: "tmpl", Body: "{{"}
			},
			wantErr: "render:",
		},
		{
			name:    "graph rejects the post",
			body:    `{"state":"open","labels":{},"key":"fp"}`,
			setup:   func(_ *fakeStore, msg *fakeMessenger) { msg.postErr = errors.New("graph down") },
			wantErr: "channel post:",
		},
		{
			name:    "graph rejects the one-shot post",
			body:    `{"labels":{},"key":"fp"}`,
			setup:   func(_ *fakeStore, msg *fakeMessenger) { msg.postErr = errors.New("graph down") },
			wantErr: "channel post:",
		},
		{
			name: "graph rejects the update",
			body: `{"state":"open","labels":{},"key":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeEvents[activeEventKey("fp", "team", "channel")] = models.ActiveEvent{
					Key: "fp", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", ConversationID: "conversation-graph-1", PostedAt: testPostedAt,
				}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "channel update:",
		},
		{
			name: "graph rejects the close update",
			body: `{"state":"closed","labels":{},"key":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeEvents[activeEventKey("fp", "team", "channel")] = models.ActiveEvent{
					Key: "fp", TeamID: "team", ChannelID: "channel", MessageID: "graph-1", ConversationID: "conversation-graph-1", PostedAt: testPostedAt,
				}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "channel update:",
		},
		{
			name:    "claiming the card fails while open",
			body:    `{"state":"open","labels":{},"key":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("ClaimActiveEvent") },
			wantErr: "claim active event:",
		},
		{
			name:    "active event lookup fails while closing",
			body:    `{"state":"closed","labels":{},"key":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("ListActiveEvents") },
			wantErr: "active event lookup:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := &fakeMessenger{}
			st := seededStore()
			srv, logs := newLoggedTestServer(t, st, msg)
			if tt.setup != nil {
				tt.setup(st, msg)
			}

			rec := postWebhook(t, srv.Handler, "/webhook/universal", "token", tt.body)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("POST = %d, want 502 (body %s)", rec.Code, rec.Body.String())
			}
			// The sender gets the status and a reference; the cause is logged.
			if strings.Contains(rec.Body.String(), tt.wantErr) || !strings.Contains(rec.Body.String(), rec.Header().Get(requestIDHeader)) {
				t.Errorf("body = %s, want only the status and the request id", rec.Body.String())
			}
			if !strings.Contains(logs.String(), tt.wantErr) {
				t.Errorf("log = %q, want it to contain %q", logs.String(), tt.wantErr)
			}
		})
	}
}

// nestedServer seeds a parent route with a child that sends the same event to a
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
		`{"state":"open","labels":{"severity":"critical","team":"payments"},"key":"fp"}`)

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
	// One card per channel, so closing later can update both.
	if len(st.activeEvents) != 2 {
		t.Errorf("stored %d cards, want one per channel", len(st.activeEvents))
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
	st.templates["tmpl"] = models.Template{ID: "tmpl", Body: `{"text":"{{ .Event.State }}"}`}
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
		`{"state":"open","labels":{"team":"payments"},"key":"fp"}`)

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
	if len(st.activeEvents) != 2 {
		t.Errorf("stored %d cards, want one per channel", len(st.activeEvents))
	}
}

func TestGreedyChildTakesDeliveryFromItsParent(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := nestedServer(t, msg, true)

	postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"open","labels":{"severity":"critical","team":"payments"},"key":"fp"}`)

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
		`{"state":"open","labels":{"severity":"critical","team":"payments"},"key":"fp"}`)

	for _, post := range msg.posts {
		if cardOf(post.msg) != `{"text":"open"}` {
			t.Errorf("card in %s = %s, want the inherited template's", post.channelID, cardOf(post.msg))
		}
	}
}

func TestClosingClearsEveryCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := nestedServer(t, msg, false)

	opened := `{"state":"open","labels":{"severity":"critical","team":"payments"},"key":"fp"}`
	postWebhook(t, handler, "/webhook/universal", "token", opened)
	postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"closed","labels":{"severity":"critical","team":"payments"},"key":"fp"}`)

	if len(msg.updates) != 2 {
		t.Fatalf("updated %d cards, want both: %+v", len(msg.updates), msg.updates)
	}
	if len(st.activeEvents) != 0 {
		t.Errorf("cards still stored = %+v, want all of them cleared", st.activeEvents)
	}
}

// One channel refusing the message must not cost the other one its card, and
// the failure still has to be reported.
func TestOneFailedDeliveryDoesNotStopTheOthers(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{postErrFor: "escalation-channel", postErr: errors.New("graph down")}
	st, handler := nestedServer(t, msg, false)

	rec := postWebhook(t, handler, "/webhook/universal", "token",
		`{"state":"open","labels":{"severity":"critical","team":"payments"},"key":"fp"}`)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("POST = %d, want 502 so the sender retries", rec.Code)
	}
	if len(st.activeEvents) != 1 {
		t.Fatalf("stored %d cards, want the one that was posted", len(st.activeEvents))
	}
	for _, card := range st.activeEvents {
		if card.ChannelID != "channel" {
			t.Errorf("stored card is for %q, want the channel that accepted it", card.ChannelID)
		}
	}
}

// A notice follows the card in a channel; a chat carries one card, so there the
// notice takes the card's place or becomes a line of text.
func TestTheNoticeRidesAlong(t *testing.T) {
	t.Parallel()

	notice := json.RawMessage(`{"notice":true}`)
	card := json.RawMessage(`{"card":true}`)
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0", ExternalURL: "https://teamster.example.com"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	srv := &Server{cfg: cfg}

	tests := []struct {
		name      string
		rendered  templates.Message
		wantCards []string
		wantChat  string
		wantText  string
		// wantChannelText is matched against the channel message's HTML text.
		wantChannelText string
		wantSummary     string
	}{
		{name: "no notice", rendered: templates.Message{Card: card}, wantCards: []string{`{"card":true}`}, wantChat: `{"card":true}`},
		{name: "a notice alone", rendered: templates.Message{Notice: notice}, wantCards: []string{`{"notice":true}`}, wantChat: `{"notice":true}`},
		{
			name: "a card and a notice", rendered: templates.Message{Text: "<p>hi</p>", Card: card, Notice: notice},
			wantCards: []string{`{"card":true}`, `{"notice":true}`}, wantChat: `{"card":true}`,
			wantText: "(https://teamster.example.com/admin#templates)", wantSummary: "hi",
		},
		{
			// A channel post is one Teams message, so beside text the notice is text too.
			name: "text and a notice", rendered: templates.Message{Text: "<p>hi</p>", Notice: notice},
			wantCards: []string{}, wantChat: `{"notice":true}`, wantChannelText: "https://teamster.example.com/admin#templates",
			wantSummary: "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			channel := srv.channelMessage(tt.rendered)
			if !strings.Contains(channel.Text, tt.wantChannelText) {
				t.Errorf("channel text = %q, want it to contain %q", channel.Text, tt.wantChannelText)
			}
			got := make([]string, 0, len(channel.Cards))
			for _, c := range channel.Cards {
				got = append(got, string(c))
			}
			if strings.Join(got, ",") != strings.Join(tt.wantCards, ",") {
				t.Errorf("channel cards = %v, want %v", got, tt.wantCards)
			}

			chat, err := srv.chatMessage(tt.rendered)
			if err != nil {
				t.Fatalf("chatMessage: %v", err)
			}
			if string(chat.Card) != tt.wantChat {
				t.Errorf("chat card = %s, want %s", chat.Card, tt.wantChat)
			}
			if !strings.Contains(chat.Text, tt.wantText) {
				t.Errorf("chat text = %q, want it to contain %q", chat.Text, tt.wantText)
			}
			if chat.Summary != tt.wantSummary {
				t.Errorf("chat summary = %q, want %q", chat.Summary, tt.wantSummary)
			}
		})
	}
}
