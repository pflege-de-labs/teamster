package cli

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/pressly/goose/v3"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/store/migrations"
)

// MigrateCmd groups the schema commands. They exist so that applying a
// migration can be a step somebody runs and watches — before an upgrade, or
// from a deployment's own job — rather than something the service does on the
// way up. Set database.migrate to verify to make that the only way it happens.
type MigrateCmd struct {
	Up     MigrateUpCmd     `cmd:"" help:"Apply every pending migration."`
	Down   MigrateDownCmd   `cmd:"" help:"Roll back the most recent migration."`
	Status MigrateStatusCmd `cmd:"" help:"Show which migrations have been applied."`
}

type MigrateUpCmd struct{}

func (c *MigrateUpCmd) Run(ctx context.Context, cfg *config.Config) error {
	return withProvider(ctx, cfg, func(provider *goose.Provider) error {
		applied, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("apply migrations: %w", err)
		}
		if len(applied) == 0 {
			fmt.Println("already up to date")
			return nil
		}
		for _, result := range applied {
			fmt.Printf("applied %d %s in %s\n", result.Source.Version, sourceName(result.Source), result.Duration)
		}
		return nil
	})
}

// MigrateDownCmd rolls back one migration. It is deliberately one at a time:
// the down of a migration that dropped data cannot bring it back, and a command
// that unwinds the whole schema in one go is too easy to run by accident.
type MigrateDownCmd struct{}

func (c *MigrateDownCmd) Run(ctx context.Context, cfg *config.Config) error {
	return withProvider(ctx, cfg, func(provider *goose.Provider) error {
		result, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("roll back migration: %w", err)
		}
		fmt.Printf("rolled back %d %s in %s\n", result.Source.Version, sourceName(result.Source), result.Duration)
		return nil
	})
}

type MigrateStatusCmd struct{}

func (c *MigrateStatusCmd) Run(ctx context.Context, cfg *config.Config) error {
	return withProvider(ctx, cfg, func(provider *goose.Provider) error {
		status, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("read migration status: %w", err)
		}

		out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		// Writes to a tabwriter are buffered; Flush below reports the failure.
		_, _ = fmt.Fprintln(out, "VERSION\tSTATE\tAPPLIED\tSOURCE")
		for _, entry := range status {
			applied := "-"
			if !entry.AppliedAt.IsZero() {
				applied = entry.AppliedAt.Format("2006-01-02 15:04:05")
			}
			_, _ = fmt.Fprintf(out, "%d\t%s\t%s\t%s\n", entry.Source.Version, entry.State, applied, sourceName(entry.Source))
		}
		return out.Flush()
	})
}

// sourceName is what to call a migration in the listing. A Go migration is
// registered rather than read from a file, so it has no path of its own.
func sourceName(source *goose.Source) string {
	if source == nil {
		return "unknown"
	}
	if source.Path != "" {
		return source.Path
	}
	return fmt.Sprintf("%s migration, registered in code", source.Type)
}

// withProvider opens the database without migrating it — whatever the
// configured mode says, a command whose whole job is the schema must decide for
// itself when it changes.
func withProvider(ctx context.Context, cfg *config.Config, fn func(*goose.Provider) error) error {
	opts := storeOptions(cfg, store.MigrateOff)
	driver, dsn, dialect := "sqlite", migrations.SQLiteDSN(opts.Path), migrations.SQLite
	if cfg.Database.Driver == store.DriverPostgres {
		driver, dsn, dialect = "pgx", opts.DSN, migrations.Postgres
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return fmt.Errorf("open %s: %w", store.Target(opts), err)
	}
	defer func() { _ = db.Close() }()

	provider, err := migrations.New(dialect, db)
	if err != nil {
		return err
	}

	// SQLite opens lazily, so the first failure anyone sees comes from the
	// migration rather than from Open. Naming the file here means every one of
	// these commands says which database it could not touch.
	if err := fn(provider); err != nil {
		return fmt.Errorf("%s: %w", store.Target(opts), err)
	}
	return nil
}
