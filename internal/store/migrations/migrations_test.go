package migrations

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func openDB(t *testing.T, name string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", SQLiteDSN(filepath.Join(t.TempDir(), name)))
	if err != nil {
		t.Fatalf("open %s: %v", name, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// latest is the highest migration this build carries. Asking rather than
// hardcoding means adding a migration does not break these tests.
func latest(t *testing.T, db *sql.DB) int64 {
	t.Helper()

	provider, err := New(SQLite, db)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	var highest int64
	for _, source := range provider.ListSources() {
		if source.Version > highest {
			highest = source.Version
		}
	}
	return highest
}

func version(t *testing.T, ctx context.Context, db *sql.DB) int64 {
	t.Helper()

	provider, err := New(SQLite, db)
	if err != nil {
		t.Fatalf("provider: %v", err)
	}
	got, err := provider.GetDBVersion(ctx)
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	return got
}

func TestUpBuildsTheSchemaFromNothing(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := openDB(t, "fresh.db")
	if err := Up(ctx, SQLite, db); err != nil {
		t.Fatalf("Up: %v", err)
	}

	for _, table := range []string{"templates", "destinations", "routes", "grants", "sessions", "login_flows", "active_alerts"} {
		var name string
		err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&name)
		if err != nil {
			t.Errorf("table %s: %v", table, err)
		}
	}
	if got, want := version(t, ctx, db), latest(t, db); got != want {
		t.Errorf("version = %d, want %d", got, want)
	}
}

// Running Up on a database that is already current has to be free of effect —
// it happens on every start of every instance.
func TestUpIsIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := openDB(t, "twice.db")
	if err := Up(ctx, SQLite, db); err != nil {
		t.Fatalf("first Up: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO templates (id, name, title, message_text, body, created_at, updated_at)
		VALUES ('keep', 'Card', '', '', '{}', '2026-01-01 00:00:00+00:00', '2026-01-01 00:00:00+00:00')`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := Up(ctx, SQLite, db); err != nil {
		t.Fatalf("second Up: %v", err)
	}

	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM templates").Scan(&count); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	if count != 1 {
		t.Errorf("templates = %d, want the row to survive a second Up", count)
	}
	if got, want := version(t, ctx, db), latest(t, db); got != want {
		t.Errorf("version = %d, want %d", got, want)
	}
}

// The population this design exists for: a database an earlier build already
// converged, carrying the current schema and no ledger. Every statement in
// 0001 has to be a no-op, and 0002 has to find nothing to do.
func TestUpAdoptsAConvergedDatabase(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := openDB(t, "converged.db")

	// What the pre-goose store left behind, seeded with a row to prove the
	// adoption does not rebuild anything underneath it.
	converged := `
CREATE TABLE templates (id TEXT PRIMARY KEY, name TEXT NOT NULL, title TEXT NOT NULL DEFAULT '',
	message_text TEXT NOT NULL DEFAULT '', body TEXT NOT NULL DEFAULT '',
	created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
CREATE TABLE destinations (id TEXT PRIMARY KEY, name TEXT NOT NULL, team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
CREATE TABLE routes (id TEXT PRIMARY KEY, name TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '',
	greedy INTEGER NOT NULL DEFAULT 0, label_selector TEXT NOT NULL, destination_id TEXT NOT NULL,
	template_id TEXT NOT NULL, is_default INTEGER NOT NULL, priority INTEGER NOT NULL,
	created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
CREATE TABLE grants (id TEXT PRIMARY KEY, role TEXT NOT NULL, team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL);
CREATE TABLE sessions (id TEXT PRIMARY KEY, subject TEXT NOT NULL, name TEXT NOT NULL,
	source TEXT NOT NULL, role TEXT NOT NULL DEFAULT '', created_at DATETIME NOT NULL,
	expires_at DATETIME NOT NULL);
CREATE TABLE login_flows (state TEXT PRIMARY KEY, verifier TEXT NOT NULL, nonce TEXT NOT NULL,
	expires_at DATETIME NOT NULL);
CREATE TABLE active_alerts (fingerprint TEXT NOT NULL, status TEXT NOT NULL, team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL, message_id TEXT NOT NULL, last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id));
INSERT INTO templates (id, name, title, message_text, body, created_at, updated_at)
	VALUES ('existing', 'Card', 'title', 'text', '{}', '2026-01-01 00:00:00+00:00', '2026-01-01 00:00:00+00:00');`
	if _, err := db.ExecContext(ctx, converged); err != nil {
		t.Fatalf("seed converged database: %v", err)
	}

	if err := Up(ctx, SQLite, db); err != nil {
		t.Fatalf("Up on a converged database: %v", err)
	}

	var title, text string
	if err := db.QueryRowContext(ctx, "SELECT title, message_text FROM templates WHERE id = 'existing'").Scan(&title, &text); err != nil {
		t.Fatalf("read the row that was already there: %v", err)
	}
	if title != "title" || text != "text" {
		t.Errorf("template = (%q, %q), want the existing values untouched", title, text)
	}
	if got, want := version(t, ctx, db), latest(t, db); got != want {
		t.Errorf("version = %d, want the converged database recorded at %d", got, want)
	}
}

func TestVerifyReportsAPendingSchema(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := openDB(t, "behind.db")

	err := Verify(ctx, SQLite, db)
	if err == nil {
		t.Fatal("Verify() = nil, want a refusal for an empty database")
	}
	if !strings.Contains(err.Error(), "teamster migrate up") {
		t.Errorf("Verify() = %v, want an error naming the command that fixes it", err)
	}

	if err := Up(ctx, SQLite, db); err != nil {
		t.Fatalf("Up: %v", err)
	}
	if err := Verify(ctx, SQLite, db); err != nil {
		t.Errorf("Verify() after Up = %v, want nil", err)
	}
}
