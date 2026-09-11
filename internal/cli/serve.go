package cli

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/httpserver"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// ServeCmd runs the HTTP server until ctx is cancelled.
type ServeCmd struct{}

// sweepSessions clears expired sessions and abandoned login flows. Neither is
// honoured once expired, so this only keeps the tables from growing.
func sweepSessions(ctx context.Context, store *store.SQLiteStore) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		if err := store.DeleteExpiredSessions(); err != nil {
			log.Printf("sweep sessions: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *ServeCmd) Run(ctx context.Context, cfg *config.Config) error {
	if err := config.Validate(*cfg); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	sqlStore, err := store.NewSQLiteStore(cfg.Database.Path)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = sqlStore.Close() }()

	graphClient, err := graph.NewClient(cfg.Graph)
	if err != nil {
		return fmt.Errorf("graph client: %w", err)
	}

	srv := httpserver.NewServer(*cfg, sqlStore, graphClient)

	// Listening before serving surfaces a bind failure as an error instead of
	// leaving it to the goroutine below.
	listener, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Server.Addr, err)
	}

	url := url.URL{Scheme: "http", Host: listener.Addr().String()}
	log.Printf("listening on %s", url.String())

	go sweepSessions(ctx, sqlStore)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(listener) }()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	log.Printf("shutting down, draining for up to %s", cfg.Server.ShutdownTimeout)

	// The shutdown deadline must outlive the cancelled ctx it is derived from.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
