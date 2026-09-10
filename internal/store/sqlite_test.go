package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()

	s, err := NewSQLiteStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// closedStore exercises the error branches, which are otherwise only reachable
// when the database itself fails.
func closedStore(t *testing.T) *SQLiteStore {
	t.Helper()

	s := newTestStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return s
}

func TestNewSQLiteStoreRejectsUnusablePath(t *testing.T) {
	t.Parallel()

	if _, err := NewSQLiteStore(filepath.Join(t.TempDir(), "missing-dir", "test.db")); err == nil {
		t.Error("NewSQLiteStore() = nil error, want failure for a path that cannot be created")
	}
}

func TestTemplateCRUD(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.CreateTemplate(models.Template{Name: "card", Body: "{}"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID == "" {
		t.Error("CreateTemplate() left the ID empty, want a generated UUID")
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("CreateTemplate() left the timestamps zero")
	}

	got, err := s.GetTemplate(created.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Name != "card" || got.Body != "{}" {
		t.Errorf("GetTemplate() = %+v, want name %q body %q", got, "card", "{}")
	}

	updated, err := s.UpdateTemplate(models.Template{ID: created.ID, Name: "renamed", Body: "{\"a\":1}"})
	if err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	if updated.Name != "renamed" {
		t.Errorf("UpdateTemplate().Name = %q, want %q", updated.Name, "renamed")
	}

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) != 1 || list[0].Name != "renamed" {
		t.Errorf("ListTemplates() = %+v, want one renamed template", list)
	}

	if err := s.DeleteTemplate(created.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := s.GetTemplate(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTemplate() after delete = %v, want ErrNotFound", err)
	}
}

func TestTemplateKeepsSuppliedID(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.CreateTemplate(models.Template{ID: "fixed-id", Name: "card", Body: "{}"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if created.ID != "fixed-id" {
		t.Errorf("CreateTemplate().ID = %q, want %q", created.ID, "fixed-id")
	}
	if _, err := s.CreateTemplate(models.Template{ID: "fixed-id", Name: "dup"}); err == nil {
		t.Error("CreateTemplate() with a duplicate ID = nil error, want a primary key violation")
	}
}

func TestDestinationCRUD(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.CreateDestination(models.Destination{Name: "ops", TeamID: "team", ChannelID: "channel"})
	if err != nil {
		t.Fatalf("CreateDestination: %v", err)
	}

	got, err := s.GetDestination(created.ID)
	if err != nil {
		t.Fatalf("GetDestination: %v", err)
	}
	if got.TeamID != "team" || got.ChannelID != "channel" {
		t.Errorf("GetDestination() = %+v, want team/channel", got)
	}

	if _, err := s.UpdateDestination(models.Destination{ID: created.ID, Name: "ops2", TeamID: "t2", ChannelID: "c2"}); err != nil {
		t.Fatalf("UpdateDestination: %v", err)
	}
	got, err = s.GetDestination(created.ID)
	if err != nil {
		t.Fatalf("GetDestination after update: %v", err)
	}
	if got.Name != "ops2" || got.TeamID != "t2" {
		t.Errorf("GetDestination() after update = %+v, want the updated values", got)
	}

	list, err := s.ListDestinations()
	if err != nil {
		t.Fatalf("ListDestinations: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("ListDestinations() returned %d items, want 1", len(list))
	}

	if err := s.DeleteDestination(created.ID); err != nil {
		t.Fatalf("DeleteDestination: %v", err)
	}
	if _, err := s.GetDestination(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDestination() after delete = %v, want ErrNotFound", err)
	}
}

func TestRouteCRUD(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	created, err := s.CreateRoute(models.Route{
		Name:          "critical",
		LabelSelector: map[string]string{"severity": "critical"},
		DestinationID: "dest",
		TemplateID:    "tmpl",
		Priority:      10,
	})
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	got, err := s.GetRoute(created.ID)
	if err != nil {
		t.Fatalf("GetRoute: %v", err)
	}
	if got.LabelSelector["severity"] != "critical" {
		t.Errorf("GetRoute().LabelSelector = %v, want severity=critical", got.LabelSelector)
	}
	if got.IsDefault {
		t.Error("GetRoute().IsDefault = true, want false")
	}
	if got.Priority != 10 {
		t.Errorf("GetRoute().Priority = %d, want 10", got.Priority)
	}

	if _, err := s.UpdateRoute(models.Route{
		ID:            created.ID,
		Name:          "fallback",
		LabelSelector: nil,
		DestinationID: "dest",
		TemplateID:    "tmpl",
		IsDefault:     true,
		Priority:      1,
	}); err != nil {
		t.Fatalf("UpdateRoute: %v", err)
	}

	got, err = s.GetRoute(created.ID)
	if err != nil {
		t.Fatalf("GetRoute after update: %v", err)
	}
	if !got.IsDefault {
		t.Error("GetRoute().IsDefault = false after update, want true")
	}
	if len(got.LabelSelector) != 0 {
		t.Errorf("GetRoute().LabelSelector = %v, want empty", got.LabelSelector)
	}

	list, err := s.ListRoutes()
	if err != nil {
		t.Fatalf("ListRoutes: %v", err)
	}
	if len(list) != 1 || !list[0].IsDefault {
		t.Errorf("ListRoutes() = %+v, want the single default route", list)
	}

	if err := s.DeleteRoute(created.ID); err != nil {
		t.Fatalf("DeleteRoute: %v", err)
	}
	if _, err := s.GetRoute(created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRoute() after delete = %v, want ErrNotFound", err)
	}
}

func TestUpdateRequiresID(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	tests := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "template",
			call: func() error { _, err := s.UpdateTemplate(models.Template{}); return err },
			want: "template id is required",
		},
		{
			name: "destination",
			call: func() error { _, err := s.UpdateDestination(models.Destination{}); return err },
			want: "destination id is required",
		},
		{
			name: "route",
			call: func() error { _, err := s.UpdateRoute(models.Route{}); return err },
			want: "route id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.call()
			if err == nil || err.Error() != tt.want {
				t.Errorf("update without ID = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestActiveAlertLifecycle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	firstUpdate := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	if _, err := s.GetActiveAlert("unknown"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetActiveAlert() for an unknown fingerprint = %v, want ErrNotFound", err)
	}

	alert := models.ActiveAlert{
		Fingerprint: "fp",
		Status:      "firing",
		TeamID:      "team",
		ChannelID:   "channel",
		MessageID:   "msg-1",
		LastUpdate:  firstUpdate,
	}
	if err := s.UpsertActiveAlert(alert); err != nil {
		t.Fatalf("UpsertActiveAlert: %v", err)
	}

	got, err := s.GetActiveAlert("fp")
	if err != nil {
		t.Fatalf("GetActiveAlert: %v", err)
	}
	if got.MessageID != "msg-1" || !got.LastUpdate.Equal(firstUpdate) {
		t.Errorf("GetActiveAlert() = %+v, want message msg-1 at %v", got, firstUpdate)
	}

	alert.MessageID = "msg-2"
	alert.LastUpdate = firstUpdate.Add(time.Minute)
	if err := s.UpsertActiveAlert(alert); err != nil {
		t.Fatalf("UpsertActiveAlert (conflict): %v", err)
	}

	got, err = s.GetActiveAlert("fp")
	if err != nil {
		t.Fatalf("GetActiveAlert after upsert: %v", err)
	}
	if got.MessageID != "msg-2" {
		t.Errorf("GetActiveAlert().MessageID = %q after upsert, want msg-2", got.MessageID)
	}

	if err := s.DeleteActiveAlert("fp"); err != nil {
		t.Fatalf("DeleteActiveAlert: %v", err)
	}
	if _, err := s.GetActiveAlert("fp"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetActiveAlert() after delete = %v, want ErrNotFound", err)
	}
}

func TestStoreErrorsWhenDatabaseIsClosed(t *testing.T) {
	t.Parallel()

	s := closedStore(t)

	tests := []struct {
		name string
		call func() error
	}{
		{"ListTemplates", func() error { _, err := s.ListTemplates(); return err }},
		{"CreateTemplate", func() error { _, err := s.CreateTemplate(models.Template{}); return err }},
		{"UpdateTemplate", func() error { _, err := s.UpdateTemplate(models.Template{ID: "id"}); return err }},
		{"DeleteTemplate", func() error { return s.DeleteTemplate("id") }},
		{"GetTemplate", func() error { _, err := s.GetTemplate("id"); return err }},
		{"ListDestinations", func() error { _, err := s.ListDestinations(); return err }},
		{"CreateDestination", func() error { _, err := s.CreateDestination(models.Destination{}); return err }},
		{"UpdateDestination", func() error { _, err := s.UpdateDestination(models.Destination{ID: "id"}); return err }},
		{"DeleteDestination", func() error { return s.DeleteDestination("id") }},
		{"GetDestination", func() error { _, err := s.GetDestination("id"); return err }},
		{"ListRoutes", func() error { _, err := s.ListRoutes(); return err }},
		{"CreateRoute", func() error { _, err := s.CreateRoute(models.Route{}); return err }},
		{"UpdateRoute", func() error { _, err := s.UpdateRoute(models.Route{ID: "id"}); return err }},
		{"DeleteRoute", func() error { return s.DeleteRoute("id") }},
		{"GetRoute", func() error { _, err := s.GetRoute("id"); return err }},
		{"UpsertActiveAlert", func() error { return s.UpsertActiveAlert(models.ActiveAlert{}) }},
		{"GetActiveAlert", func() error { _, err := s.GetActiveAlert("fp"); return err }},
		{"DeleteActiveAlert", func() error { return s.DeleteActiveAlert("fp") }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if err := tt.call(); err == nil {
				t.Errorf("%s() on a closed database = nil error, want failure", tt.name)
			}
		})
	}
}

func TestSelectorSerialization(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		selector map[string]string
		raw      string
		want     map[string]string
	}{
		{name: "nil selector", selector: nil, raw: "{}", want: map[string]string{}},
		{name: "empty selector", selector: map[string]string{}, raw: "{}", want: map[string]string{}},
		{
			name:     "single label",
			selector: map[string]string{"severity": "critical"},
			raw:      `{"severity":"critical"}`,
			want:     map[string]string{"severity": "critical"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := serializeSelector(tt.selector); got != tt.raw {
				t.Errorf("serializeSelector(%v) = %q, want %q", tt.selector, got, tt.raw)
			}
			got := parseSelector(tt.raw)
			if len(got) != len(tt.want) {
				t.Fatalf("parseSelector(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for key, want := range tt.want {
				if got[key] != want {
					t.Errorf("parseSelector(%q)[%q] = %q, want %q", tt.raw, key, got[key], want)
				}
			}
		})
	}
}

func TestParseSelectorFallsBackToEmptyMap(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"", "not json", "null"} {
		if got := parseSelector(raw); got == nil || len(got) != 0 {
			t.Errorf("parseSelector(%q) = %v, want an empty non-nil map", raw, got)
		}
	}
}

func TestNewSQLiteStoreRejectsLegacyTextTimestamps(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
)`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	_, err = NewSQLiteStore(path)
	if err == nil {
		t.Fatal("NewSQLiteStore() = nil error, want a rejection of the legacy schema")
	}
	if !strings.Contains(err.Error(), "delete it and restart") {
		t.Errorf("NewSQLiteStore() = %v, want an error telling the operator to delete the database", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	session := models.Session{
		ID: "sess-1", Subject: "user-1", Name: "Jens", Source: "oidc",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	if err := s.CreateSession(session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := s.GetSession("sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.Subject != "user-1" || got.Name != "Jens" || got.Source != "oidc" {
		t.Errorf("GetSession() = %+v, want the stored identity", got)
	}

	if err := s.DeleteSession("sess-1"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.GetSession("sess-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession() after delete = %v, want ErrNotFound", err)
	}
}

// An expired session must be indistinguishable from a missing one, so no caller
// can honour it by forgetting to compare the time itself.
func TestExpiredSessionReadsAsMissing(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.CreateSession(models.Session{
		ID: "stale", Subject: "u", CreatedAt: time.Now().Add(-2 * time.Hour), ExpiresAt: time.Now().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, err := s.GetSession("stale"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSession() = %v, want an expired session to read as missing", err)
	}
}

func TestDeleteExpiredSessionsSweepsBothTables(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	past, future := time.Now().Add(-time.Hour), time.Now().Add(time.Hour)

	for _, session := range []models.Session{
		{ID: "old", ExpiresAt: past, CreatedAt: past},
		{ID: "live", ExpiresAt: future, CreatedAt: time.Now()},
	} {
		if err := s.CreateSession(session); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	for _, flow := range []models.LoginFlow{
		{State: "old", Verifier: "v", Nonce: "n", ExpiresAt: past},
		{State: "live", Verifier: "v", Nonce: "n", ExpiresAt: future},
	} {
		if err := s.CreateLoginFlow(flow); err != nil {
			t.Fatalf("CreateLoginFlow: %v", err)
		}
	}

	if err := s.DeleteExpiredSessions(); err != nil {
		t.Fatalf("DeleteExpiredSessions: %v", err)
	}

	if _, err := s.GetSession("live"); err != nil {
		t.Errorf("the live session was swept: %v", err)
	}
	if _, err := s.TakeLoginFlow("live"); err != nil {
		t.Errorf("the live flow was swept: %v", err)
	}
	if _, err := s.TakeLoginFlow("old"); !errors.Is(err, ErrNotFound) {
		t.Errorf("the expired flow survived: %v", err)
	}
}

// A state may be redeemed once, so a replayed callback finds nothing.
func TestLoginFlowIsSingleUse(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	flow := models.LoginFlow{State: "state-1", Verifier: "verifier", Nonce: "nonce", ExpiresAt: time.Now().Add(time.Minute)}

	if err := s.CreateLoginFlow(flow); err != nil {
		t.Fatalf("CreateLoginFlow: %v", err)
	}

	got, err := s.TakeLoginFlow("state-1")
	if err != nil {
		t.Fatalf("TakeLoginFlow: %v", err)
	}
	if got.Verifier != "verifier" || got.Nonce != "nonce" {
		t.Errorf("TakeLoginFlow() = %+v, want the stored verifier and nonce", got)
	}

	if _, err := s.TakeLoginFlow("state-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a state was redeemable twice: %v", err)
	}
}

func TestExpiredLoginFlowIsRefused(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)
	if err := s.CreateLoginFlow(models.LoginFlow{State: "stale", Verifier: "v", Nonce: "n", ExpiresAt: time.Now().Add(-time.Minute)}); err != nil {
		t.Fatalf("CreateLoginFlow: %v", err)
	}

	if _, err := s.TakeLoginFlow("stale"); !errors.Is(err, ErrNotFound) {
		t.Errorf("TakeLoginFlow() = %v, want an expired flow refused", err)
	}
}
