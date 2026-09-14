package cli

import (
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// storeOptions is the one translation from configuration to store options, so
// the three commands that open a database cannot disagree about what the
// settings mean.
func storeOptions(cfg *config.Config, migrate store.MigrateMode) store.Options {
	pg := cfg.Database.Postgres
	return store.Options{
		Driver:  cfg.Database.Driver,
		Migrate: migrate,
		Path:    cfg.Database.Path,
		DSN: store.PostgresDSN(pg.Host, pg.Port, pg.DBName, pg.User, pg.Password,
			pg.SSLMode, pg.SSLRootCert, pg.URL),
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
	}
}
