package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// auditedServer is the server as serve wires it: writes go through audit.Wrap.
func auditedServer(t *testing.T, st *fakeStore) http.Handler {
	t.Helper()
	rec := audit.NewRecorder(quietLog, nil, 1, audit.NewDBSink(st))
	cfg := config.Config{
		Server:  config.ServerConfig{Addr: ":0"},
		Webhook: config.WebhookConfig{Token: "token"},
		Admin:   config.AdminConfig{Username: "admin", Password: "pass"},
	}
	srv, err := NewServer(quietLog, cfg, audit.Wrap(st, rec), &fakeMessenger{}, nil, &fakeMessenger{}, metrics.Disabled(), nil)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv.Handler
}

func TestAuditRecordsWhoChangedWhat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		send      func(t *testing.T, h http.Handler) (requestID string)
		wantActor models.Actor
	}{
		{
			name: "the API with the local credentials",
			send: func(t *testing.T, h http.Handler) string {
				req := httptest.NewRequest(http.MethodPost, "/api/templates", strings.NewReader(`{"name":"api","body":"{}"}`))
				req.SetBasicAuth("admin", "pass")
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, req)
				if rec.Code != http.StatusCreated {
					t.Fatalf("POST /api/templates = %d %s", rec.Code, rec.Body.String())
				}
				return rec.Header().Get(requestIDHeader)
			},
			wantActor: models.Actor{Subject: "admin", Name: "admin", Via: models.ViaBasic},
		},
		{
			name: "a form posted from a session",
			send: func(t *testing.T, h http.Handler) string {
				form := url.Values{"name": {"form"}, "body": {"{}"}}
				rec := postFormAs(t, h, "/admin/templates", form)
				if rec.Code != http.StatusSeeOther || strings.Contains(rec.Header().Get("Location"), "error=") {
					t.Fatalf("POST /admin/templates = %d %s", rec.Code, rec.Header().Get("Location"))
				}
				return rec.Header().Get(requestIDHeader)
			},
			wantActor: models.Actor{Subject: "tester", Name: "tester", Via: models.ViaSession},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := sessionAs(newFakeStore(), authz.RoleEditor)
			requestID := tt.send(t, auditedServer(t, st))

			st.mu.Lock()
			defer st.mu.Unlock()
			if len(st.auditEvents) != 1 {
				t.Fatalf("recorded %d events, want 1", len(st.auditEvents))
			}
			e := st.auditEvents[0]
			if e.Before != nil {
				t.Errorf("a create has a before snapshot: %s", e.Before)
			}
			if e.Action != "template.create" || e.Actor != tt.wantActor || e.RequestID != requestID || requestID == "" {
				t.Errorf("event = %+v, want template.create by %+v in request %q", e, tt.wantActor, requestID)
			}
		})
	}
}

func seededAuditStore(n int) *fakeStore {
	st := newFakeStore()
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	for i := range n {
		actor := "alice"
		if i%2 == 1 {
			actor = "bob"
		}
		st.auditEvents = append(st.auditEvents, models.AuditEvent{
			ID:           fmt.Sprintf("ev-%03d", i),
			OccurredAt:   t0.Add(time.Duration(i) * time.Minute),
			Actor:        models.Actor{Subject: actor, Via: models.ViaSession},
			Action:       "template.update",
			ResourceType: "Template",
			ResourceID:   "t1",
			RequestID:    fmt.Sprintf("req-%03d", i),
			Before:       json.RawMessage(`{"name":"old-name","body":"{}"}`),
			After:        json.RawMessage(`{"name":"new-name","body":"{}"}`),
		})
	}
	return st
}

func TestAuditPage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		events    int
		query     string
		want      []string
		unwanted  []string
		wantOlder bool
	}{
		{name: "empty", query: "", want: []string{"Nothing recorded yet."}},
		{name: "lists with the diff", events: 3, want: []string{"template.update", "t1", "old-name", "new-name", "req-002"}},
		{name: "filters by actor", events: 4, query: "?actor=bob", want: []string{"req-003", "req-001"}, unwanted: []string{"req-002", "req-000"}},
		{name: "a full page links older ones", events: auditPageSize + 1, wantOlder: true},
		{name: "the cursor resumes", events: auditPageSize + 1, query: "?cursor=ev-001&at=2026-09-01T12:01:00Z", want: []string{"req-000"}, unwanted: []string{"req-050"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := newTestServer(t, seededAuditStore(tt.events), &fakeMessenger{}).Handler
			rec := do(t, handler, http.MethodGet, "/admin/audit"+tt.query, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /admin/audit = %d", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range tt.want {
				if !strings.Contains(body, want) {
					t.Errorf("page lacks %q", want)
				}
			}
			for _, unwanted := range tt.unwanted {
				if strings.Contains(body, unwanted) {
					t.Errorf("page shows %q", unwanted)
				}
			}
			if older := strings.Contains(body, "Older changes"); older != tt.wantOlder {
				t.Errorf("older link shown = %v, want %v", older, tt.wantOlder)
			}
		})
	}

	handler := newTestServer(t, newFakeStore().fail("ListAuditEvents"), &fakeMessenger{}).Handler
	if rec := do(t, handler, http.MethodGet, "/admin/audit", ""); !strings.Contains(rec.Body.String(), "Something went wrong. Reference: ") {
		t.Error("a failing store is not reported on the page")
	}
	if rec := do(t, handler, http.MethodPost, "/admin/audit", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /admin/audit = %d, want 405", rec.Code)
	}
}

func TestAuditAPI(t *testing.T) {
	t.Parallel()

	handler := newTestServer(t, seededAuditStore(5), &fakeMessenger{}).Handler

	rec := do(t, handler, http.MethodGet, "/api/audit?limit=2&actor=alice", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/audit = %d", rec.Code)
	}
	var page auditResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 2 || page.Events[0].ID != "ev-004" || page.Next == nil || page.Next.Cursor != "ev-002" {
		t.Fatalf("page = %+v", page)
	}

	next := fmt.Sprintf("/api/audit?limit=2&actor=alice&cursor=%s&at=%s", page.Next.Cursor, page.Next.At.Format(time.RFC3339Nano))
	rec = do(t, handler, http.MethodGet, next, "")
	page = auditResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Events[0].ID != "ev-000" || page.Next != nil {
		t.Errorf("last page = %+v", page)
	}

	empty := newTestServer(t, newFakeStore(), &fakeMessenger{}).Handler
	if rec := do(t, empty, http.MethodGet, "/api/audit", ""); !strings.Contains(rec.Body.String(), `"events":[]`) {
		t.Errorf("empty trail = %s, want an empty list", rec.Body.String())
	}
	failing := newTestServer(t, newFakeStore().fail("ListAuditEvents"), &fakeMessenger{}).Handler
	if rec := do(t, failing, http.MethodGet, "/api/audit", ""); rec.Code != http.StatusInternalServerError {
		t.Errorf("failing store = %d, want 500", rec.Code)
	}
	if rec := do(t, empty, http.MethodDelete, "/api/audit", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/audit = %d, want 405", rec.Code)
	}
}

func TestAuditIsTheAdminsAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		role     authz.Role
		path     string
		wantCode int
		wantNav  bool
	}{
		{"admin reads the page", authz.RoleAdmin, "/admin/audit", http.StatusOK, true},
		{"admin reads the API", authz.RoleAdmin, "/api/audit", http.StatusOK, true},
		{"editor is refused the page", authz.RoleEditor, "/admin/audit", http.StatusForbidden, false},
		{"viewer is refused the API", authz.RoleViewer, "/api/audit", http.StatusForbidden, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			handler := newTestServer(t, sessionAs(newFakeStore(), tt.role), &fakeMessenger{}).Handler
			if rec := asRole(t, handler, http.MethodGet, tt.path, ""); rec.Code != tt.wantCode {
				t.Errorf("GET %s as %s = %d, want %d", tt.path, tt.role, rec.Code, tt.wantCode)
			}
			nav := asRole(t, handler, http.MethodGet, "/admin", "").Body.String()
			if got := strings.Contains(nav, `href="/admin/audit"`); got != tt.wantNav {
				t.Errorf("nav offers the audit trail = %v, want %v", got, tt.wantNav)
			}
		})
	}
}

func TestAuditChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		before, after string
		want          []views.AuditChange
	}{
		{name: "nothing", want: nil},
		{name: "a create lists every field", after: `{"b":2,"a":"x"}`, want: []views.AuditChange{{Field: "a", After: `"x"`}, {Field: "b", After: "2"}}},
		{name: "a delete lists every field", before: `{"a":1}`, want: []views.AuditChange{{Field: "a", Before: "1"}}},
		{name: "an update lists what differs", before: `{"a":1,"b":{"x": 1}}`, after: `{"a":1,"b":{"x":2},"c":true}`, want: []views.AuditChange{{Field: "b", Before: `{"x":1}`, After: `{"x":2}`}, {Field: "c", After: "true"}}},
		{name: "not objects", before: `[1]`, after: `[2]`, want: []views.AuditChange{{Before: "[1]", After: "[2]"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var before, after json.RawMessage
			if tt.before != "" {
				before = json.RawMessage(tt.before)
			}
			if tt.after != "" {
				after = json.RawMessage(tt.after)
			}
			got := auditChanges(before, after)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("auditChanges() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
