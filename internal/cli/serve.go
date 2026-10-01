package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/cards"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/httpserver"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/people"
	"github.com/pflege-de-labs/teamster/internal/samples"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// ServeCmd runs the HTTP server until ctx is cancelled.
type ServeCmd struct {
	// ready lets tests learn the bound addresses instead of guessing a free port.
	ready func(server, metrics string)
}

// sessionSweeper is the one method the sweep needs. Taking the interface
// rather than the store keeps the sweeper testable and independent of which
// backend is open, in the spirit of ADR 0002.
type sessionSweeper interface {
	DeleteExpiredSessions(ctx context.Context) error
}

// sweepSessions clears expired sessions and abandoned login flows. Neither is
// honoured once expired, so this only keeps the tables from growing.
func sweepSessions(ctx context.Context, logger *slog.Logger, store sessionSweeper) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		if err := store.DeleteExpiredSessions(ctx); err != nil {
			logger.Error("sweep sessions", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// newInstaller opens chats with people, and installs the app for them when
// bot.global-install allows.
func newInstaller(cfg *config.Config, st store.Store, dir *graph.Client, chats *bot.Client, rec people.Recorder) *people.Installer {
	return people.NewInstaller(st, dir, chats, rec, people.InstallerConfig{
		BotID:        cfg.Bot.ClientID,
		ServiceURL:   cfg.Bot.ServiceURL,
		AppID:        cfg.Bot.AppID,
		CatalogAppID: cfg.Bot.CatalogAppID,
		Global:       cfg.Bot.GlobalInstall,
	})
}

// newReconciler wires the installer for every member of the tenant. Its owner
// name tells replicas apart in the run table.
func newReconciler(logger *slog.Logger, cfg *config.Config, st store.Store, dir *graph.Client, inst *people.Installer, rec people.Recorder) *people.Reconciler {
	host, _ := os.Hostname()
	owner := fmt.Sprintf("%s-%d", host, os.Getpid())
	return people.NewReconciler(logger, st, dir, inst, rec, people.ReconcilerConfig{
		TenantID:    cfg.Graph.TenantID,
		Interval:    cfg.Bot.ReconcileInterval,
		Reverify:    cfg.Bot.ReverifyInterval,
		Concurrency: cfg.Bot.InstallConcurrency,
		Owner:       owner,
	})
}

func (c *ServeCmd) Run(ctx context.Context, cfg *config.Config) error {
	if err := config.Validate(*cfg); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	logger, err := logging.New(cfg.Log, os.Stderr)
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}
	// Routes the standard library's own log output, and any left in dependencies, through the same handler.
	slog.SetDefault(logger)

	opts := storeOptions(cfg, store.MigrateMode(cfg.Database.Migrate))
	sqlStore, err := store.Open(ctx, opts)
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
	// Which database this instance is on is the first question of any
	// incident, and the image's baked-in path makes it non-obvious.
	logger.Info("store opened", "target", store.Target(opts))
	if warning := webhookTokenWarning(ctx, cfg, sqlStore); warning != "" {
		logger.Warn(warning)
	}
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
			logger.Error("metrics shutdown", "err", err)
		}
	}()

	// The collection's own context, not the process one: the last collection
	// is the one shutdown forces, by which time the process context is already
	// cancelled and reading the gauge through it would fail.
	recorder, err := newRecorder(logger, cfg.Audit, sqlStore, telemetry)
	if err != nil {
		return fmt.Errorf("audit: %w", err)
	}
	defer func() {
		// Runs after the HTTP drain, so every change it let through is recorded.
		closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := recorder.Close(closeCtx); err != nil {
			logger.Error("audit shutdown", "err", err)
		}
	}()
	// Configuration writes go through this; reads and delivery bookkeeping pass straight on.
	auditedStore := audit.Wrap(sqlStore, recorder)

	if err := seedPresets(ctx, logger, auditedStore); err != nil {
		return err
	}

	if err := telemetry.ObserveActiveEvents(func(ctx context.Context) (int64, error) {
		return sqlStore.CountActiveEvents(ctx)
	}); err != nil {
		return fmt.Errorf("active events gauge: %w", err)
	}
	if err := telemetry.ObserveDestinationsWithoutApp(sqlStore.CountDestinationsWithoutBotTeam); err != nil {
		return fmt.Errorf("destinations without app gauge: %w", err)
	}
	if err := telemetry.ObserveDirectoryUsers(func(ctx context.Context) (map[string]int64, error) {
		counts, err := sqlStore.CountDirectoryUsersByState(ctx)
		out := make(map[string]int64, len(counts))
		for state, n := range counts {
			out[string(state)] = n
		}
		return out, err
	}); err != nil {
		return fmt.Errorf("directory users gauge: %w", err)
	}

	// Binding here rather than in the goroutine, for the same reason the main
	// listener does: an address already in use is an error to return, not a log
	// line to lose.
	metricsAddr, stopMetrics, err := telemetry.Start()
	if err != nil {
		return fmt.Errorf("metrics listener: %w", err)
	}
	if metricsAddr != "" {
		logger.Info("metrics listening", "url", "http://"+metricsAddr+cfg.Metrics.Path)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Metrics.ShutdownTimeout)
		defer cancel()
		if err := stopMetrics(shutdownCtx); err != nil {
			logger.Error("metrics listener shutdown", "err", err)
		}
	}()

	graphClient, err := graph.NewClient(cfg.Graph, telemetry)
	if err != nil {
		return fmt.Errorf("graph client: %w", err)
	}

	// Built only when the bot is configured -- unlike graphClient above,
	// which NewServer always needs. Constructing it regardless used to leave
	// httpserver's "if s.bot == nil" guards dead outside a test that deliberately
	// passes nil, and meant an unconfigured deployment's unlink button issued a
	// real client-credentials token request against three empty strings, and
	// logged a failure, every single time. botClient is left as a true nil
	// interface rather than a nil *bot.Client assigned to it: the latter would
	// still compare unequal to nil on the other side of NewServer's interface
	// parameter, defeating every one of those guards the same way. See
	// BotConfig's comment for why this credential is a second, separate Entra
	// registration from Graph's.
	var botClient interface {
		SendMessage(ctx context.Context, ref bot.ConversationReference, msg bot.Message) (string, error)
		UpdateMessage(ctx context.Context, ref bot.ConversationReference, activityID string, msg bot.Message) error
	}
	// Nil for the same reason; NewServer then refuses every channel delivery
	// with a reason, since Graph cannot post there (ADR 0045).
	var channels httpserver.ChannelTransport
	// Nil unless bot.global-install is on (ADR 0059).
	var reconciler *people.Reconciler
	var serverOpts []httpserver.Option
	if cfg.Bot.Configured() {
		client, err := bot.NewClient(cfg.Bot, telemetry)
		if err != nil {
			return fmt.Errorf("bot client: %w", err)
		}
		botClient = client
		channels = httpserver.NewBotChannels(client, sqlStore, cfg.Bot, cfg.Graph.TenantID)
		// Messages can name people whenever the bot is there; only installing
		// for them waits on global install (ADR 0063).
		inst := newInstaller(cfg, sqlStore, graphClient, client, telemetry)
		serverOpts = append(serverOpts, httpserver.WithPeople(people.Finder{
			Resolver:  people.NewResolver(sqlStore, graphClient, telemetry, cfg.Graph.TenantID, cfg.Bot.DirectoryTTL),
			Installer: inst,
		}))
		if cfg.Bot.GlobalInstall {
			reconciler = newReconciler(logger, cfg, sqlStore, graphClient, inst, telemetry)
		}
	} else {
		logger.Warn("bot not configured: every channel delivery will fail, see ADR 0045")
	}

	sampler, err := samples.New(logger, sqlStore, cfg.Samples)
	if err != nil {
		return fmt.Errorf("samples: %w", err)
	}

	srv, err := httpserver.NewServer(logger, *cfg, auditedStore, graphClient, botClient, channels, telemetry, sampler, serverOpts...)
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
	logger.Info("listening", "url", url.String())
	if c.ready != nil {
		c.ready(listener.Addr().String(), metricsAddr)
	}

	go sweepSessions(ctx, logger, sqlStore)
	if cfg.Audit.Database {
		go audit.NewPruner(logger, sqlStore, cfg.Audit.RetentionAge, cfg.Audit.RetentionCount, cfg.Audit.PruneInterval).Run(ctx)
	}

	// Stopped only once the drain below is over, so the alerts it delivers are
	// sampled too, and waited for before the store closes: its last act is a write.
	samplerCtx, stopSampler := context.WithCancel(context.WithoutCancel(ctx))
	samplerDone := make(chan struct{})
	go func() {
		defer close(samplerDone)
		sampler.Run(samplerCtx)
	}()
	defer func() {
		stopSampler()
		<-samplerDone
	}()

	if reconciler != nil {
		// Stopped with the process context: a run cut short is taken over or
		// requested again, so there is nothing to drain.
		reconcilerDone := make(chan struct{})
		go func() {
			defer close(reconcilerDone)
			reconciler.Run(ctx)
		}()
		defer func() { <-reconcilerDone }()
	}

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

	logger.Info("shutting down", "drain", cfg.Server.ShutdownTimeout.String())

	// The shutdown deadline must outlive the cancelled ctx it is derived from.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// webhookTokenWarning says why every webhook is about to be refused, which is
// otherwise a stream of 401s that looks like a sender misconfiguration.
func webhookTokenWarning(ctx context.Context, cfg *config.Config, st store.Store) string {
	if cfg.Webhook.Token != "" {
		return ""
	}
	tokens, err := st.ListAccessTokens(ctx)
	if err != nil {
		return fmt.Sprintf("list access tokens: %v", err)
	}
	if len(tokens) == 0 {
		return "no webhook token configured and none issued: /webhook/* refuses every sender until one is created at /admin/tokens"
	}
	return ""
}

// seedPresets gives a new installation one default template per source, so
// its first messages look like messages rather than JSON (ADR 0055).
func seedPresets(ctx context.Context, logger *slog.Logger, st store.Store) error {
	presets := cards.Presets()
	seed := make([]models.Template, 0, len(presets))
	for _, p := range presets {
		seed = append(seed, p.Template())
	}
	seeded, err := st.SeedTemplates(ctx, seed)
	if err != nil {
		return fmt.Errorf("seed default templates: %w", err)
	}
	if seeded {
		logger.Info("seeded default templates", "count", len(seed))
	}
	return nil
}
