package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/pflege-de-labs/teamster/internal/store/migrations"
)

// MigrateMode decides what opening a store does about a schema that is not the
// one this build expects.
//
// The default is to apply what is missing, which is what a single instance
// wants and what every release before the version ledger did implicitly. A
// deployment that runs migrations as a step of its own — because the schema
// change should be something an operator can watch fail, or because the
// application's database role is not allowed to change the schema — sets
// Verify, and the process refuses to serve a database it cannot read rather
// than discovering it one query at a time.
type MigrateMode string

const (
	// MigrateAuto applies pending migrations when the store is opened.
	MigrateAuto MigrateMode = "auto"
	// MigrateVerify refuses to open a database with migrations pending.
	MigrateVerify MigrateMode = "verify"
	// MigrateOff opens the database as it is, and asks no questions. It exists
	// for the case where the caller knows better, and it is how a read-only
	// command avoids writing to a database it was only asked to look at.
	MigrateOff MigrateMode = "off"
)

func applyMigrations(ctx context.Context, dialect migrations.Dialect, db *sql.DB, mode MigrateMode) error {
	switch mode {
	// The zero value is the default rather than an error: a Config built in Go
	// rather than parsed by kong has no mode, and refusing it would make the
	// store harder to use from a test than from the command line.
	case MigrateAuto, "":
		return migrations.Up(ctx, dialect, db)
	case MigrateVerify:
		return migrations.Verify(ctx, dialect, db)
	case MigrateOff:
		return nil
	default:
		return fmt.Errorf("unknown migrate mode %q, want auto, verify or off", mode)
	}
}
