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
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// ServeCmd runs the HTTP server until ctx is cancelled.
type ServeCmd struct{}

// sessionSweeper is the one method the sweep needs. Taking the interface
// rather than the store keeps the sweeper testable and independent of which
// backend is open, in the spirit of ADR 0002.
type sessionSweeper interface {
	DeleteExpiredSessions(ctx context.Context) error
}

// sweepSessions clears expired sessions and abandoned login flows. Neither is
// honoured once expired, so this only keeps the tables from growing.
func sweepSessions(ctx context.Context, store sessionSweeper) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		if err := store.DeleteExpiredSessions(ctx); err != nil {
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

	sqlStore, err := store.NewSQLiteStore(ctx, cfg.Database.Path, store.MigrateMode(cfg.Database.Migrate))
	if err != nil {
		// A signal that arrives while the store is still opening is a
		// shutdown, not a failure to start: there is nothing to report and
		// nothing left to close.
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = sqlStore.Close() }()

	// After the store, so that the deferred shutdown below — and the last
	// collection it triggers — runs while the database is still open.
	telemetry, err := metrics.New(cfg.Metrics)
	if err != nil {
		return fmt.Errorf("metrics: %w", err)
	}
	defer func() {
		// A deadline of its own: by the time this runs, ctx is the cancelled
		// one that started the shutdown, and a cancelled context abandons the
		// final export — the window worth having after a crash.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Metrics.ShutdownTimeout)
		defer cancel()
		if err := telemetry.Shutdown(shutdownCtx); err != nil {
			log.Printf("metrics shutdown: %v", err)
		}
	}()

	// The collection's own context, not the process one: the last collection
	// is the one shutdown forces, by which time the process context is already
	// cancelled and reading the gauge through it would fail.
	if err := telemetry.ObserveActiveAlerts(func(ctx context.Context) (int64, error) {
		return sqlStore.CountActiveAlerts(ctx)
	}); err != nil {
		return fmt.Errorf("active alerts gauge: %w", err)
	}

	// Binding here rather than in the goroutine, for the same reason the main
	// listener does: an address already in use is an error to return, not a log
	// line to lose.
	metricsAddr, stopMetrics, err := telemetry.Start()
	if err != nil {
		return fmt.Errorf("metrics listener: %w", err)
	}
	if metricsAddr != "" {
		log.Printf("metrics on http://%s%s", metricsAddr, cfg.Metrics.Path)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Metrics.ShutdownTimeout)
		defer cancel()
		if err := stopMetrics(shutdownCtx); err != nil {
			log.Printf("metrics listener shutdown: %v", err)
		}
	}()

	graphClient, err := graph.NewClient(cfg.Graph, telemetry)
	if err != nil {
		return fmt.Errorf("graph client: %w", err)
	}

	srv, err := httpserver.NewServer(*cfg, sqlStore, graphClient, telemetry)
	if err != nil {
		return fmt.Errorf("http server: %w", err)
	}

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
