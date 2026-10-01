package cli

import (
	"log/slog"

	"github.com/pflege-de-labs/teamster/internal/audit"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// newRecorder opens the audit sinks the configuration names (ADR 0070). With
// none it is nil, which records nothing and leaves the store unwrapped.
func newRecorder(logger *slog.Logger, cfg config.AuditConfig, st store.Store, metrics audit.Metrics) (*audit.Recorder, error) {
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
	if primary == nil && len(async) == 0 {
		return nil, nil
	}
	return audit.NewRecorder(logger, metrics, cfg.QueueSize, primary, async...), nil
}
