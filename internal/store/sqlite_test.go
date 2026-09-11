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

	if _, err := s.GetActiveAlert("unknown", "team", "channel"); !errors.Is(err, ErrNotFound) {
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

	got, err := s.GetActiveAlert("fp", "team", "channel")
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

	got, err = s.GetActiveAlert("fp", "team", "channel")
	if err != nil {
		t.Fatalf("GetActiveAlert after upsert: %v", err)
	}
	if got.MessageID != "msg-2" {
		t.Errorf("GetActiveAlert().MessageID = %q after upsert, want msg-2", got.MessageID)
	}

	// The same alert in a second channel is a second card, not an overwrite.
	second := alert
	second.ChannelID = "other-channel"
	second.MessageID = "msg-3"
	if err := s.UpsertActiveAlert(second); err != nil {
		t.Fatalf("UpsertActiveAlert (second channel): %v", err)
	}
	cards, err := s.ListActiveAlerts("fp")
	if err != nil {
		t.Fatalf("ListActiveAlerts: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("ListActiveAlerts() = %+v, want one card per channel", cards)
	}

	if err := s.DeleteActiveAlert("fp", "team", "channel"); err != nil {
		t.Fatalf("DeleteActiveAlert: %v", err)
	}
	if _, err := s.GetActiveAlert("fp", "team", "channel"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetActiveAlert() after delete = %v, want ErrNotFound", err)
	}
	if _, err := s.GetActiveAlert("fp", "team", "other-channel"); err != nil {
		t.Errorf("the other channel's card = %v, want it untouched", err)
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
		{"Ping", func() error { return s.Ping() }},
		{"ListGrants", func() error { _, err := s.ListGrants(); return err }},
		{"CreateGrant", func() error { _, err := s.CreateGrant(models.Grant{}); return err }},
		{"DeleteGrant", func() error { return s.DeleteGrant("id") }},
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
		{"GetActiveAlert", func() error { _, err := s.GetActiveAlert("fp", "team", "channel"); return err }},
		{"DeleteActiveAlert", func() error { return s.DeleteActiveAlert("fp", "team", "channel") }},
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

// A database written before templates could carry a title must keep working:
// the columns are added and the rows already in it are still readable.
func TestNewSQLiteStoreAddsTemplateMessageColumns(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE templates (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	body TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	updated_at DATETIME NOT NULL
);
INSERT INTO templates (id, name, body, created_at, updated_at)
VALUES ('old', 'Card', '{"type":"AdaptiveCard"}', '2026-01-01 00:00:00+00:00', '2026-01-01 00:00:00+00:00')`)
	if err != nil {
		t.Fatalf("seed old db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetTemplate("old")
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Name != "Card" || got.Body != `{"type":"AdaptiveCard"}` {
		t.Errorf("template = %+v, want the row that was already there", got)
	}
	if got.Title != "" || got.Text != "" {
		t.Errorf("template = %+v, want the new columns empty", got)
	}

	// Writing through the new columns has to work against the altered table.
	got.Title = "{{ .Alert.Status }}"
	got.Text = "<p>hi</p>"
	if _, err := store.UpdateTemplate(got); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	back, err := store.GetTemplate("old")
	if err != nil {
		t.Fatalf("GetTemplate after update: %v", err)
	}
	if back.Title != got.Title || back.Text != got.Text {
		t.Errorf("template = %+v, want the title and text stored", back)
	}
}

// An alert used to have one card, so active_alerts was keyed by fingerprint
// alone. Fan-out needs the channel in the key, which SQLite can only do by
// rebuilding the table — the cards already posted have to survive it.
func TestNewSQLiteStoreRekeysActiveAlerts(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "old-key.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE active_alerts (
	fingerprint TEXT PRIMARY KEY,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL
);
INSERT INTO active_alerts VALUES ('fp', 'firing', 'team', 'channel', 'msg-1', '2026-01-01 00:00:00+00:00')`)
	if err != nil {
		t.Fatalf("seed old db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	got, err := store.GetActiveAlert("fp", "team", "channel")
	if err != nil {
		t.Fatalf("GetActiveAlert: %v", err)
	}
	if got.MessageID != "msg-1" {
		t.Errorf("card = %+v, want the one that was already posted", got)
	}

	// The point of the rebuild: a second channel is a second card.
	second := got
	second.ChannelID = "other"
	second.MessageID = "msg-2"
	if err := store.UpsertActiveAlert(second); err != nil {
		t.Fatalf("UpsertActiveAlert: %v", err)
	}
	cards, err := store.ListActiveAlerts("fp")
	if err != nil {
		t.Fatalf("ListActiveAlerts: %v", err)
	}
	if len(cards) != 2 {
		t.Errorf("cards = %+v, want one per channel", cards)
	}
}

// A database written before sessions carried roles gains the column, and the
// sessions already in it keep working.
func TestNewSQLiteStoreAddsTheSessionRoles(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "no-role.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE sessions (
	id TEXT PRIMARY KEY,
	subject TEXT NOT NULL,
	name TEXT NOT NULL,
	source TEXT NOT NULL,
	created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL
);
INSERT INTO sessions VALUES ('old', 'tester', 'tester', 'oidc', '2026-01-01 00:00:00+00:00', '2099-01-01 00:00:00+00:00')`)
	if err != nil {
		t.Fatalf("seed old db: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	store, err := NewSQLiteStore(path)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	session, err := store.GetSession("old")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session.Subject != "tester" || session.Roles != "" {
		t.Errorf("session = %+v, want the stored row with an empty role", session)
	}

	session.ID, session.Roles = "new", "editor"
	if err := store.CreateSession(session); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	back, err := store.GetSession("new")
	if err != nil {
		t.Fatalf("GetSession after create: %v", err)
	}
	if back.Roles != "editor" {
		t.Errorf("roles = %q, want them stored", back.Roles)
	}
}

// An import is applied inside one transaction, so a failure part way through
// has to leave the configuration as it was.
func TestWithTx(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	if err := s.WithTx(func(tx Store) error {
		_, err := tx.CreateTemplate(models.Template{ID: "kept", Name: "Kept", Body: "{}"})
		return err
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if _, err := s.GetTemplate("kept"); err != nil {
		t.Errorf("GetTemplate after a committed transaction = %v, want the row", err)
	}

	wanted := errors.New("changed my mind")
	err := s.WithTx(func(tx Store) error {
		if _, err := tx.CreateTemplate(models.Template{ID: "rolled-back", Name: "Gone", Body: "{}"}); err != nil {
			return err
		}
		return wanted
	})
	if !errors.Is(err, wanted) {
		t.Errorf("WithTx() = %v, want the callback's error", err)
	}
	if _, err := s.GetTemplate("rolled-back"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetTemplate after a rollback = %v, want ErrNotFound", err)
	}

	// Nothing inside a transaction may close the database, ping it, or open a
	// second one; each would be operating on a handle that is not there.
	if err := s.WithTx(func(tx Store) error {
		if err := tx.Ping(); err == nil {
			t.Error("Ping() inside a transaction = nil, want a refusal")
		}
		if err := tx.Close(); err == nil {
			t.Error("Close() inside a transaction = nil, want a refusal")
		}
		if err := tx.WithTx(func(Store) error { return nil }); err == nil {
			t.Error("a nested WithTx = nil, want a refusal")
		}
		return nil
	}); err != nil {
		t.Fatalf("WithTx: %v", err)
	}
}

func TestGrantLifecycle(t *testing.T) {
	t.Parallel()

	s := newTestStore(t)

	grants, err := s.ListGrants()
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 0 {
		t.Errorf("ListGrants() = %+v, want none before any is created", grants)
	}

	team, err := s.CreateGrant(models.Grant{Role: "editor", TeamID: "platform"})
	if err != nil {
		t.Fatalf("CreateGrant: %v", err)
	}
	if team.ID == "" || team.CreatedAt.IsZero() {
		t.Errorf("grant = %+v, want an id and a timestamp", team)
	}
	if _, err := s.CreateGrant(models.Grant{Role: "viewer", TeamID: "payments", ChannelID: "alerts"}); err != nil {
		t.Fatalf("CreateGrant (channel): %v", err)
	}

	grants, err = s.ListGrants()
	if err != nil {
		t.Fatalf("ListGrants after create: %v", err)
	}
	// Ordered by role, so the page and its tests see them the same way twice.
	if len(grants) != 2 || grants[0].Role != "editor" || grants[1].ChannelID != "alerts" {
		t.Errorf("grants = %+v, want both, ordered by role", grants)
	}

	if err := s.DeleteGrant(team.ID); err != nil {
		t.Fatalf("DeleteGrant: %v", err)
	}
	grants, _ = s.ListGrants()
	if len(grants) != 1 {
		t.Errorf("grants = %+v, want the deleted one gone", grants)
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
