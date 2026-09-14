package cli

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/pflege-de-labs/teamster/internal/config"
)

func migrateConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "migrate.db")},
	}
}

func TestMigrateUpThenStatus(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := migrateConfig(t)

	if err := (&MigrateUpCmd{}).Run(ctx, cfg); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	db, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var name string
	if err := db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='routes'").Scan(&name); err != nil {
		t.Fatalf("routes table after migrate up: %v", err)
	}

	// Twice is the ordinary case — every start of every instance runs it.
	if err := (&MigrateUpCmd{}).Run(ctx, cfg); err != nil {
		t.Errorf("second migrate up: %v", err)
	}
	if err := (&MigrateStatusCmd{}).Run(ctx, cfg); err != nil {
		t.Errorf("migrate status: %v", err)
	}
}

// Down unwinds one step, which is the whole point of it being one step: the
// baseline drops every table, and doing that by accident should take two
// commands rather than one.
func TestMigrateDownRollsBackOneMigration(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	cfg := migrateConfig(t)
	if err := (&MigrateUpCmd{}).Run(ctx, cfg); err != nil {
		t.Fatalf("migrate up: %v", err)
	}

	// 0002 has no down: the convergence it performs cannot be undone, and
	// goose records the version anyway.
	if err := (&MigrateDownCmd{}).Run(ctx, cfg); err != nil {
		t.Fatalf("first migrate down: %v", err)
	}
	if err := (&MigrateDownCmd{}).Run(ctx, cfg); err != nil {
		t.Fatalf("second migrate down: %v", err)
	}

	db, err := sql.Open("sqlite", cfg.Database.Path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	var name string
	err = db.QueryRowContext(ctx, "SELECT name FROM sqlite_master WHERE type='table' AND name='routes'").Scan(&name)
	if err == nil {
		t.Error("routes still exists after rolling back the baseline")
	}
}

func TestMigrateReportsAnUnusablePath(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		Database: config.DatabaseConfig{Path: filepath.Join(t.TempDir(), "missing-dir", "migrate.db")},
	}

	err := (&MigrateUpCmd{}).Run(t.Context(), cfg)
	if err == nil {
		t.Fatal("migrate up = nil error, want failure for a path that cannot be created")
	}
	if !strings.Contains(err.Error(), "migrate.db") {
		t.Errorf("migrate up = %v, want an error naming the database", err)
	}
}
