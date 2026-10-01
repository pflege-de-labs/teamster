package audit

import (
	"context"
	"log/slog"
	"time"
)

// A Pruner enforces the database trail's retention. Pruning is idempotent, so
// every replica may run one.
type Pruner struct {
	log      *slog.Logger
	store    Store
	maxAge   time.Duration
	maxCount int
	interval time.Duration
	now      func() time.Time
}

// NewPruner keeps events younger than maxAge and at most maxCount of them;
// zero switches either limit off.
func NewPruner(logger *slog.Logger, st Store, maxAge time.Duration, maxCount int, interval time.Duration) *Pruner {
	return &Pruner{
		log:      logger,
		store:    st,
		maxAge:   maxAge,
		maxCount: maxCount,
		interval: interval,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Prune runs once and reports how many events it removed.
func (p *Pruner) Prune(ctx context.Context) (int64, error) {
	var cutoff time.Time
	if p.maxAge > 0 {
		cutoff = p.now().Add(-p.maxAge)
	}
	return p.store.PruneAuditEvents(ctx, cutoff, p.maxCount)
}

// Run prunes at start and then every interval until ctx is cancelled.
func (p *Pruner) Run(ctx context.Context) {
	if p.maxAge <= 0 && p.maxCount <= 0 {
		return
	}
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		if removed, err := p.Prune(ctx); err != nil {
			if ctx.Err() == nil {
				p.log.Error("prune audit events", "err", err)
			}
		} else if removed > 0 {
			p.log.Debug("pruned audit events", "removed", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
