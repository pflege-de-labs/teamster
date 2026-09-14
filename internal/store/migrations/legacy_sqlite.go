package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/pressly/goose/v3"
)

// legacySQLiteMigrations is the pre-goose past made explicit.
//
// Before there was a version ledger the store converged the schema on every
// open, by inspection: it added whatever columns were missing, rebuilt
// active_alerts if it still had the old key, and refused a database whose
// timestamps were declared the wrong type. That left three populations in the
// field — fresh, already converged by a recent build, and genuinely old — and
// only the last needs anything done.
//
// Replaying that history as ordinary ALTER TABLE steps would fail on the
// second population, because SQLite has no ADD COLUMN IF NOT EXISTS and the
// columns are already there. So the inspection moves here verbatim, runs once,
// and is recorded. After this, every change is an ordinary numbered migration
// and no PRAGMA is read anywhere in the codebase.
func legacySQLiteMigrations() []*goose.Migration {
	return []*goose.Migration{
		goose.NewGoMigration(2, &goose.GoFunc{RunTx: convergeLegacySQLite}, nil),
	}
}

// addedColumns are columns a later release introduced. CREATE TABLE IF NOT
// EXISTS leaves an existing table alone, so a database created before them
// needs them added rather than the whole schema replayed.
var addedColumns = []struct {
	table, column, definition string
}{
	{"templates", "title", "TEXT NOT NULL DEFAULT ''"},
	{"templates", "message_text", "TEXT NOT NULL DEFAULT ''"},
	{"routes", "parent_id", "TEXT NOT NULL DEFAULT ''"},
	{"routes", "greedy", "INTEGER NOT NULL DEFAULT 0"},
	{"sessions", "role", "TEXT NOT NULL DEFAULT ''"},
}

// timestampColumns are read back as time.Time only while they are declared
// DATETIME; a database written before that fix silently fails every Scan.
var timestampColumns = map[string][]string{
	"templates":     {"created_at", "updated_at"},
	"grants":        {"created_at", "updated_at"},
	"destinations":  {"created_at", "updated_at"},
	"routes":        {"created_at", "updated_at"},
	"active_alerts": {"last_update"},
	"sessions":      {"created_at", "expires_at"},
	"login_flows":   {"expires_at"},
}

func convergeLegacySQLite(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx); err != nil {
		return err
	}
	if err := rekeyActiveAlerts(ctx, tx); err != nil {
		return err
	}
	return checkTimestampColumns(ctx, tx)
}

// A template written before this migration is card-only, and renders the title
// it always had: templates.DefaultTitle fills in for an empty one.
func addMissingColumns(ctx context.Context, tx *sql.Tx) error {
	for _, add := range addedColumns {
		declared, err := columnTypes(ctx, tx, add.table)
		if err != nil {
			return err
		}
		if _, ok := declared[add.column]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", add.table, add.column, add.definition)); err != nil {
			return fmt.Errorf("add %s.%s: %w", add.table, add.column, err)
		}
	}
	return nil
}

func checkTimestampColumns(ctx context.Context, tx *sql.Tx) error {
	// Sorted, so a database with two bad columns names the same one every run.
	tables := make([]string, 0, len(timestampColumns))
	for table := range timestampColumns {
		tables = append(tables, table)
	}
	sort.Strings(tables)

	for _, table := range tables {
		declared, err := columnTypes(ctx, tx, table)
		if err != nil {
			return err
		}
		for _, column := range timestampColumns[table] {
			if got := strings.ToUpper(declared[column]); got != "" && got != "DATETIME" {
				return fmt.Errorf("table %s column %s is declared %s, expected DATETIME; "+
					"this database was created by an older build and cannot be read, delete it and restart",
					table, column, got)
			}
		}
	}
	return nil
}

// An alert that fans out has one card per channel, so active_alerts is keyed by
// the channel as well as the fingerprint. A database written before that has the
// old single-column key, which SQLite can only change by rebuilding the table —
// the rows survive, because one card per alert is a valid fan-out of one.
//
// goose runs this inside a transaction of its own, and SQLite makes DDL
// transactional, so a rebuild interrupted half way rolls back whole rather than
// leaving the scratch table behind.
func rekeyActiveAlerts(ctx context.Context, tx *sql.Tx) error {
	key, err := primaryKey(ctx, tx, "active_alerts")
	if err != nil {
		return err
	}
	if len(key) != 1 || key[0] != "fingerprint" {
		return nil
	}

	_, err = tx.ExecContext(ctx, `
CREATE TABLE active_alerts_rekeyed (
	fingerprint TEXT NOT NULL,
	status TEXT NOT NULL,
	team_id TEXT NOT NULL,
	channel_id TEXT NOT NULL,
	message_id TEXT NOT NULL,
	last_update DATETIME NOT NULL,
	PRIMARY KEY (fingerprint, team_id, channel_id)
);
INSERT INTO active_alerts_rekeyed SELECT fingerprint, status, team_id, channel_id, message_id, last_update FROM active_alerts;
DROP TABLE active_alerts;
ALTER TABLE active_alerts_rekeyed RENAME TO active_alerts;`)
	if err != nil {
		return fmt.Errorf("rekey active alerts: %w", err)
	}
	return nil
}

// primaryKey returns the key columns in key order, which is what distinguishes
// the rebuilt active_alerts table from the one that preceded it.
func primaryKey(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	type keyColumn struct {
		name     string
		position int
	}
	var key []keyColumn
	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		if pk > 0 {
			key = append(key, keyColumn{name: name, position: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(key, func(i, j int) bool { return key[i].position < key[j].position })
	names := make([]string, 0, len(key))
	for _, column := range key {
		names = append(names, column.name)
	}
	return names, nil
}

func columnTypes(ctx context.Context, tx *sql.Tx, table string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return nil, fmt.Errorf("inspect %s: %w", table, err)
	}
	defer func() { _ = rows.Close() }()

	types := map[string]string{}
	for rows.Next() {
		var (
			cid        int
			name, kind string
			notNull    int
			dflt       sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &kind, &notNull, &dflt, &pk); err != nil {
			return nil, fmt.Errorf("scan %s column: %w", table, err)
		}
		types[name] = kind
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return types, nil
}
