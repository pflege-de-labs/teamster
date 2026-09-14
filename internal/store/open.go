package store

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"time"
)

// Options is what opening a store needs to know. It is not config.Config so
// that this package stays free of the configuration schema, and so its tests
// can ask for a store without building one.
type Options struct {
	Driver  string
	Migrate MigrateMode

	// SQLite
	Path string

	// Postgres
	DSN             string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnectTimeout  time.Duration
}

const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
)

// Open picks the backend by name and never by inference.
//
// Inferring it from which fields happen to be filled in would be the more
// convenient thing and the wrong one: the container image sets
// TEAMSTER_DATABASE_PATH unconditionally, so an operator who configured only a
// Postgres host would get a service that starts, works, and writes everything
// to a file that dies with the container.
func Open(ctx context.Context, opts Options) (Store, error) {
	switch opts.Driver {
	case DriverSQLite, "":
		return NewSQLiteStore(ctx, opts.Path, opts.Migrate)
	case DriverPostgres:
		return NewPostgresStore(ctx, PostgresOptions{
			DSN:             opts.DSN,
			MaxOpenConns:    opts.MaxOpenConns,
			MaxIdleConns:    opts.MaxIdleConns,
			ConnMaxLifetime: opts.ConnMaxLifetime,
			ConnectTimeout:  opts.ConnectTimeout,
		}, opts.Migrate)
	default:
		return nil, fmt.Errorf("unknown database driver %q, want %q or %q", opts.Driver, DriverSQLite, DriverPostgres)
	}
}

// Target is what to say in a log line about where the state lives. It exists
// because "which database is this instance actually on" is the first question
// of any incident, and because the image's baked-in path makes the answer
// non-obvious. It never includes the password.
func Target(opts Options) string {
	switch opts.Driver {
	case DriverPostgres:
		return "postgres " + redactDSN(opts.DSN)
	default:
		return "sqlite " + opts.Path
	}
}

func redactDSN(dsn string) string {
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Host == "" {
		return "(unparseable connection string)"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	return parsed.String()
}

// PostgresDSN assembles the connection string from the discrete settings, or
// passes through the URL when one is given.
func PostgresDSN(host string, port int, dbname, user, password, sslmode, sslrootcert, rawURL string) string {
	if rawURL != "" {
		return rawURL
	}

	dsn := url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		Path:   "/" + dbname,
	}
	if password != "" {
		dsn.User = url.UserPassword(user, password)
	} else {
		dsn.User = url.User(user)
	}

	query := url.Values{}
	if sslmode != "" {
		query.Set("sslmode", sslmode)
	}
	if sslrootcert != "" {
		query.Set("sslrootcert", sslrootcert)
	}
	// Guardrails per session rather than per statement: a query that has run
	// away, a lock that will not come, and a transaction somebody left open
	// should all end by themselves rather than hold a connection for the life
	// of the process.
	query.Set("statement_timeout", "5000")
	query.Set("lock_timeout", "2000")
	query.Set("idle_in_transaction_session_timeout", "10000")
	dsn.RawQuery = query.Encode()

	return dsn.String()
}
