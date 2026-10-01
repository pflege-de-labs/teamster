package cli

import (
	"context"
	"log/slog"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// newRecorder opens the audit sinks the configuration names (ADR 0070, ADR 0071).
// With none it is nil, which records nothing and leaves the store unwrapped.
func newRecorder(ctx context.Context, logger *slog.Logger, cfg config.AuditConfig, st store.Store, metrics audit.Metrics) (*audit.Recorder, error) {
	var primary audit.Sink
	if cfg.Database {
		primary = audit.NewDBSink(st)
	}
	var async []audit.Sink
	if cfg.File != "" {
		file, err := audit.OpenFile(cfg.File)
		if err != nil {
			return nil, err
		}
		async = append(async, file)
	}
	if cfg.NATS.URL != "" {
		stream, err := audit.OpenNATS(ctx, logger, audit.NATSOptions{
			URL:           cfg.NATS.URL,
			SubjectPrefix: cfg.NATS.SubjectPrefix,
			Stream:        cfg.NATS.Stream,
			CreateStream:  cfg.NATS.CreateStream,
			CredsFile:     cfg.NATS.CredsFile,
			Timeout:       cfg.NATS.Timeout,
		})
		if err != nil {
			for _, sink := range async {
				_ = sink.Close()
			}
			return nil, err
		}
		async = append(async, stream)
	}
	if primary == nil && len(async) == 0 {
		return nil, nil
	}
	return audit.NewRecorder(logger, metrics, cfg.QueueSize, primary, async...), nil
}
