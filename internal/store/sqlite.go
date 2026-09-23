package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/pflege-de-labs/teamster/internal/store/migrations"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// The three operations that need the database itself, rather than somewhere to
// run a statement, used to be guarded by a nil field that the other twenty-odd
// methods trusted silently. Now a transaction is a type that cannot perform
// them.
var (
	errInTransaction        = errors.New("cannot do this inside a transaction")
	errAlreadyInTransaction = errors.New("already in a transaction")
)

type SQLiteStore struct {
	queryAdapter
	queries *sqlitedb.Queries
	db      *sql.DB
	path    string
}

// sqliteTx is the store as the function inside WithTx sees it: the same data
// methods, and a refusal on the three that would reach past the transaction.
type sqliteTx struct {
	queryAdapter
}

func (sqliteTx) Close() error { return errInTransaction }
func (sqliteTx) WithSerializableTx(context.Context, func(context.Context, Store) error) error {
	return errAlreadyInTransaction
}
func (sqliteTx) Ping(context.Context) error { return errInTransaction }
func (sqliteTx) WithTx(context.Context, func(context.Context, Store) error) error {
	return errAlreadyInTransaction
}

// NewSQLiteStore opens the file and brings the schema to the version this
// build expects, or checks that somebody else already did — see MigrateMode.
func NewSQLiteStore(ctx context.Context, path string, migrate MigrateMode) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", migrations.SQLiteDSN(path))
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// One connection, deliberately. SQLite takes a single writer, and left to
	// itself database/sql opens as many connections to the file as there are
	// concurrent callers -- which turns two requests for the same alert into
	// SQLITE_BUSY rather than into one of them waiting its turn. With a single
	// connection the queueing happens in Go, where it costs nothing at this
	// volume and cannot fail.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := applyMigrations(ctx, migrations.SQLite, db, migrate); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &SQLiteStore{
		queryAdapter: queryAdapter{q: sqlitedb.New(db)},
		db:           db,
		path:         path,
	}, nil
}

// tx binds the queries to one transaction. It reaches for the concrete type
// because WithTx is not part of the generated interface.
func (s *SQLiteStore) tx(tx *sql.Tx) dialectQueries {
	return s.queries.WithTx(tx)
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// DeleteRecipient overrides queryAdapter's to cascade; see
// deleteRecipientCascade for why both deletes have to commit together.
func (s *SQLiteStore) DeleteRecipient(ctx context.Context, id string) error {
	return deleteRecipientCascade(ctx, s, id)
}

// DeleteSession overrides queryAdapter's to cascade; see deleteSessionCascade
// for why both deletes have to commit together.
func (s *SQLiteStore) DeleteSession(ctx context.Context, id string) error {
	return deleteSessionCascade(ctx, s, id)
}

// DeleteDestination and SetDefaultDestination override queryAdapter's to run
// their statements in one transaction, which is what keeps exactly one default.
func (s *SQLiteStore) DeleteDestination(ctx context.Context, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		return tx.DeleteDestination(ctx, id)
	})
}

func (s *SQLiteStore) SetDefaultDestination(ctx context.Context, id string) error {
	return s.WithTx(ctx, func(ctx context.Context, tx Store) error {
		return tx.SetDefaultDestination(ctx, id)
	})
}

// Ping is a round trip to the database rather than a look at a connection
// struct: a file that has been deleted or a disk that has gone read-only shows
// up here and nowhere else.
func (s *SQLiteStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// WithTx runs fn against a store bound to one transaction, committing when it
// returns nil and rolling back otherwise. It is what lets an import that turns
// out to be invalid half way through leave the configuration as it found it.
func (s *SQLiteStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// A panic must not leave the transaction open, and it must keep travelling.
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := fn(ctx, &sqliteTx{queryAdapter{q: s.tx(tx)}}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// WithSerializableTx is WithTx on SQLite. There is one writer, so a
// transaction already sees a world nobody else is changing -- the distinction
// exists for a backend where that is not free.
func (s *SQLiteStore) WithSerializableTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
	return s.WithTx(ctx, fn)
}
