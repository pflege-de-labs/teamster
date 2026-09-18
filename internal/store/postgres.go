package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/pflege-de-labs/teamster/internal/store/migrations"
	"github.com/pflege-de-labs/teamster/internal/store/pgdb"
)

// PostgresStore is the backend for a deployment that runs more than one
// instance. Everything above it is the shared adapter; what is here is the
// three things a networked, genuinely concurrent database needs and a local
// file does not: a bounded pool, an isolation level asked for by name, and a
// retry for the conflicts that isolation reports instead of preventing.
type PostgresStore struct {
	queryAdapter
	queries *pgdb.Queries
	db      *sql.DB
}

type postgresTx struct {
	queryAdapter
}

func (postgresTx) Close() error               { return errInTransaction }
func (postgresTx) Ping(context.Context) error { return errInTransaction }
func (postgresTx) WithTx(context.Context, func(context.Context, Store) error) error {
	return errAlreadyInTransaction
}

func (postgresTx) WithSerializableTx(context.Context, func(context.Context, Store) error) error {
	return errAlreadyInTransaction
}

// PostgresOptions is what the caller may set about the connection. Zero means
// "choose something sensible", so a deployment that has not thought about pool
// sizes gets one that works rather than one connection or none.
type PostgresOptions struct {
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
	ConnectTimeout  time.Duration
}

const (
	defaultMaxOpenConns    = 10
	defaultMaxIdleConns    = 5
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

func NewPostgresStore(ctx context.Context, opts PostgresOptions, migrate MigrateMode) (*PostgresStore, error) {
	db, err := sql.Open("pgx", opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	// Bounded, because the limit that matters is the server's max_connections
	// divided by the number of instances, and an unbounded pool discovers that
	// during an incident. A lifetime so a connection pinned to a demoted
	// primary or a rotated pooler backend eventually dies rather than serving
	// errors forever.
	db.SetMaxOpenConns(orDefault(opts.MaxOpenConns, defaultMaxOpenConns))
	db.SetMaxIdleConns(orDefault(opts.MaxIdleConns, defaultMaxIdleConns))
	db.SetConnMaxLifetime(orDefaultDuration(opts.ConnMaxLifetime, defaultConnMaxLifetime))
	db.SetConnMaxIdleTime(orDefaultDuration(opts.ConnMaxIdleTime, defaultConnMaxIdleTime))

	// A connection is not attempted until the first query, so an unreachable
	// database would otherwise be discovered by the first alert rather than at
	// startup.
	connectCtx, cancel := context.WithTimeout(ctx, orDefaultDuration(opts.ConnectTimeout, 10*time.Second))
	defer cancel()
	if err := db.PingContext(connectCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to postgres: %w", err)
	}

	if err := applyMigrations(ctx, migrations.Postgres, db, migrate); err != nil {
		_ = db.Close()
		return nil, err
	}

	queries := pgdb.New(db)
	return &PostgresStore{
		queryAdapter: queryAdapter{q: pgQueries{q: queries}},
		queries:      queries,
		db:           db,
	}, nil
}

func orDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func orDefaultDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func (s *PostgresStore) Close() error {
	return s.db.Close()
}

func (s *PostgresStore) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	return nil
}

// DeleteRecipient overrides queryAdapter's to cascade; see
// deleteRecipientCascade for why both deletes have to commit together.
func (s *PostgresStore) DeleteRecipient(ctx context.Context, id string) error {
	return deleteRecipientCascade(ctx, s, id)
}

func (s *PostgresStore) WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
	return s.runTx(ctx, sql.LevelReadCommitted, fn)
}

func (s *PostgresStore) WithSerializableTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error {
	return s.runTx(ctx, sql.LevelSerializable, fn)
}

// maxTxAttempts bounds the retry. A conflict that survives three attempts is
// not contention any more, and answering slowly forever is worse than saying so.
const maxTxAttempts = 3

// runTx retries what the isolation level reports rather than prevents.
// Serializable transactions fail with a serialization error instead of
// blocking, deadlocks are detected and one side is chosen to die, and a
// concurrent insert can surface as a unique violation even behind ON CONFLICT.
// All three mean "run it again", and none of them should reach a handler as a
// database error code.
func (s *PostgresStore) runTx(ctx context.Context, level sql.IsolationLevel, fn func(ctx context.Context, tx Store) error) error {
	var err error
	for attempt := range maxTxAttempts {
		err = s.attemptTx(ctx, level, fn)
		if err == nil || !retryable(err) {
			return err
		}
		if ctx.Err() != nil {
			return err
		}
		// Jittered, so two instances that collided do not collide again in step.
		backoff := time.Duration(1<<attempt) * 10 * time.Millisecond
		select {
		case <-time.After(backoff + rand.N(backoff)):
		case <-ctx.Done():
			return err
		}
	}
	return fmt.Errorf("after %d attempts: %w", maxTxAttempts, err)
}

func (s *PostgresStore) attemptTx(ctx context.Context, level sql.IsolationLevel, fn func(ctx context.Context, tx Store) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: level})
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

	if err := fn(ctx, &postgresTx{queryAdapter{q: pgQueries{q: s.queries.WithTx(tx)}}}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	committed = true
	return nil
}

// retryable reports the three SQLSTATEs that mean "the database gave up on this
// attempt, not on the work": serialization failure, deadlock, and the unique
// violation a concurrent insert can produce.
func retryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "40001", "40P01", "23505":
		return true
	default:
		return false
	}
}
