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
	if msg.posts[0].summary != "CPU spiking" {
		t.Errorf("summary = %q, want the annotation", msg.posts[0].summary)
	}
	if string(msg.posts[0].card) != `{"text":"firing"}` {
		t.Errorf("card = %s, want the rendered template", msg.posts[0].card)
	}

	active, ok := st.activeAlerts["fp-1"]
	if !ok {
		t.Fatal("no active alert stored")
	}
	if active.MessageID != "graph-1" {
		t.Errorf("stored message ID = %q, want graph-1", active.MessageID)
	}
}

func TestRepeatedFiringAlertUpdatesTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeAlerts["fp-1"] = models.ActiveAlert{
		Fingerprint: "fp-1",
		Status:      "firing",
		TeamID:      "team",
		ChannelID:   "channel",
		MessageID:   "graph-1",
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
	if _, ok := st.activeAlerts["fp-1"]; !ok {
		t.Error("active alert was removed, want it kept while firing")
	}
}

func TestResolvedAlertUpdatesAndClearsTheCard(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	st, handler := seededServer(t, msg)
	st.activeAlerts["fp-1"] = models.ActiveAlert{Fingerprint: "fp-1", MessageID: "graph-1"}

	rec := postWebhook(t, handler, "/webhook/universal", "token", `{"status":"resolved","labels":{},"fingerprint":"fp-1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	if len(msg.updates) != 1 {
		t.Fatalf("updates = %d, want 1", len(msg.updates))
	}
	if _, ok := st.activeAlerts["fp-1"]; ok {
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

	if len(msg.posts) != 1 || msg.posts[0].summary != "HighCPU" {
		t.Errorf("summary = %+v, want it to fall back to the alertname", msg.posts)
	}
}

func TestAlertWithoutSummaryOrAlertnameGetsAGenericSummary(t *testing.T) {
	t.Parallel()

	msg := &fakeMessenger{}
	_, handler := seededServer(t, msg)

	postWebhook(t, handler, "/webhook/universal", "token", `{"status":"firing","labels":{},"fingerprint":"fp"}`)

	if len(msg.posts) != 1 || msg.posts[0].summary != "Alert update" {
		t.Errorf("summary = %+v, want the generic fallback", msg.posts)
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
	for fingerprint := range st.activeAlerts {
		if len(fingerprint) != 64 {
			t.Errorf("fingerprint = %q, want a SHA-256 hex digest", fingerprint)
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
			name:    "unknown status",
			body:    `{"status":"flapping","labels":{},"fingerprint":"fp"}`,
			wantErr: "unknown status: flapping",
		},
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
			name: "graph rejects the update",
			body: `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeAlerts["fp"] = models.ActiveAlert{Fingerprint: "fp", MessageID: "graph-1"}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "graph update:",
		},
		{
			name: "graph rejects the resolve update",
			body: `{"status":"resolved","labels":{},"fingerprint":"fp"}`,
			setup: func(st *fakeStore, msg *fakeMessenger) {
				st.activeAlerts["fp"] = models.ActiveAlert{Fingerprint: "fp", MessageID: "graph-1"}
				msg.updateErr = errors.New("graph down")
			},
			wantErr: "graph update:",
		},
		{
			name:    "active alert lookup fails while firing",
			body:    `{"status":"firing","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("GetActiveAlert") },
			wantErr: "active alert lookup:",
		},
		{
			name:    "active alert lookup fails while resolving",
			body:    `{"status":"resolved","labels":{},"fingerprint":"fp"}`,
			setup:   func(st *fakeStore, _ *fakeMessenger) { st.fail("GetActiveAlert") },
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
