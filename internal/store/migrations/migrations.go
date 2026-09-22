// Package migrations owns the database schema and the order it was built in.
//
// The migration files are embedded, so a release needs nothing on disk beside
// the binary — the same reason the admin UI's assets are embedded.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed sqlite/*.sql postgres/*.sql
var files embed.FS

// Dialect names a backend's migration set. It mirrors goose's own dialect
// rather than wrapping it, so adding a backend means adding a directory.
type Dialect string

const (
	SQLite   Dialect = "sqlite"
	Postgres Dialect = "postgres"
)

// New builds a provider for one open database.
//
// The global goose API — SetBaseFS, SetDialect, Up — is package-level mutable
// state shared by every caller in the process, which this project does not
// allow and which two stores opened in one test would fight over. The provider
// API carries the same state per instance, so this takes it and disables the
// global registry outright.
func New(dialect Dialect, db *sql.DB, opts ...goose.ProviderOption) (*goose.Provider, error) {
	dir := string(dialect)
	sub, err := fs.Sub(files, dir)
	if err != nil {
		return nil, fmt.Errorf("migrations for %s: %w", dialect, err)
	}

	options := []goose.ProviderOption{goose.WithDisableGlobalRegistry(true)}
	switch dialect {
	case SQLite:
		// No session locker: SQLite is a file, one instance at a time, mostly
		// run by hand against a local database. Nothing here makes concurrent
		// Up from two processes on the same file safe -- goose has no SQLite
		// locker to give it one, and a busy_timeout pragma only avoids the
		// easy failure, not the ledger race. Don't run `teamster migrate` and
		// a live store's own migration against the same SQLite file at once.
		options = append(options, goose.WithGoMigrations(legacySQLiteMigrations()...))
	case Postgres:
		// More than one instance may start at once, and they must not both
		// migrate. The session lock is held for the migration and released
		// with the session, so an instance killed mid-migration does not leave
		// the next one waiting forever.
		options = append(options, goose.WithSessionLocker(mustSessionLocker()))
	}
	options = append(options, opts...)

	provider, err := goose.NewProvider(gooseDialect(dialect), db, sub, options...)
	if err != nil {
		return nil, fmt.Errorf("migration provider for %s: %w", dialect, err)
	}
	return provider, nil
}

// mustSessionLocker builds the advisory lock. It panics rather than returning
// an error because the only failure is an invalid option constant, which is a
// programming mistake rather than a runtime condition.
func mustSessionLocker() lock.SessionLocker {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		panic(fmt.Sprintf("postgres session locker: %v", err))
	}
	return locker
}

func gooseDialect(dialect Dialect) goose.Dialect {
	switch dialect {
	case SQLite:
		return goose.DialectSQLite3
	case Postgres:
		return goose.DialectPostgres
	default:
		return goose.Dialect(dialect)
	}
}

// SQLiteDSN adds the pragmas every caller opening a SQLite file needs: busy_timeout
// so a second connection -- the store's own startup migration and an operator's
// `teamster migrate` landing at nearly the same moment -- waits for the first's
// lock instead of failing SQLITE_BUSY outright, WAL so a reader does not block
// the writer, and foreign_keys since it is off per connection unless asked for.
// This is best-effort, not a real cross-process lock; see the comment on New's
// SQLite case for what it does not cover. A path that already looks like a DSN
// is left alone, so an operator can pass options this does not know about.
func SQLiteDSN(path string) string {
	if strings.HasPrefix(path, "file:") || strings.Contains(path, "?") {
		return path
	}
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
}

// Up applies everything pending.
func Up(ctx context.Context, dialect Dialect, db *sql.DB) error {
	provider, err := New(dialect, db)
	if err != nil {
		return err
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// Verify reports what is missing without changing anything. A database behind
// the binary that reads it fails on the first query with a message about a
// column, far from the cause; this says it in one line at startup instead.
func Verify(ctx context.Context, dialect Dialect, db *sql.DB) error {
	provider, err := New(dialect, db)
	if err != nil {
		return err
	}

	pending, err := provider.HasPending(ctx)
	if err != nil {
		return fmt.Errorf("check for pending migrations: %w", err)
	}
	if pending {
		return fmt.Errorf("the database is behind this build; run `teamster migrate up` to bring it up to date")
	}
	return nil
}
