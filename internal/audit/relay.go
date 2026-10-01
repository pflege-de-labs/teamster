package audit

import (
	"context"
	"log/slog"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// RelayStore is what a relay reads the trail and keeps its place in.
type RelayStore interface {
	ListAuditEvents(ctx context.Context, filter models.AuditFilter) ([]models.AuditEvent, error)
	ListAuditEventsAfter(ctx context.Context, cursor models.AuditCursor, until time.Time, limit int) ([]models.AuditEvent, error)
	AuditCursor(ctx context.Context, name string) (models.AuditCursor, bool, error)
	AdvanceAuditCursor(ctx context.Context, name string, from, to models.AuditCursor) (bool, error)
}

// relayBatch bounds one catch-up step, so a long outage drains in pieces.
const relayBatch = 100

// A Relay feeds a sink from the database trail rather than from a queue, so
// the trail is its buffer: an outage delays events, and loses none that
// retention keeps (ADR 0078).
type Relay struct {
	log      *slog.Logger
	store    RelayStore
	sink     Sink
	metrics  Metrics
	interval time.Duration
	// settle holds back events this young, so one stamped earlier but
	// committed later is not passed by the cursor.
	settle time.Duration
	now    func() time.Time
}

// NewRelay builds a relay for sink, which it closes when Run returns.
func NewRelay(logger *slog.Logger, st RelayStore, sink Sink, metrics Metrics, interval, settle time.Duration) *Relay {
	if metrics == nil {
		metrics = noMetrics{}
	}
	return &Relay{
		log: logger, store: st, sink: sink, metrics: metrics,
		interval: interval, settle: settle,
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Run delivers until ctx is cancelled, then closes the sink.
func (r *Relay) Run(ctx context.Context) {
	defer func() { _ = r.sink.Close() }()
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		if _, err := r.Step(ctx); err != nil && ctx.Err() == nil {
			r.log.Warn("audit relay behind", "sink", r.sink.Name(), "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Step delivers what is due and reports how many events it delivered. It
// stops at the first event the sink refuses, which is retried next time.
func (r *Relay) Step(ctx context.Context) (int, error) {
	name := r.sink.Name()
	cursor, ok, err := r.store.AuditCursor(ctx, name)
	if err != nil {
		return 0, err
	}
	if !ok {
		// A relay starts where the trail is now, not with its whole history.
		start, err := r.newest(ctx)
		if err != nil {
			return 0, err
		}
		if _, err := r.store.AdvanceAuditCursor(ctx, name, models.AuditCursor{}, start); err != nil {
			return 0, err
		}
		return 0, nil
	}

	delivered := 0
	for {
		events, err := r.store.ListAuditEventsAfter(ctx, cursor, r.now().Add(-r.settle), relayBatch)
		if err != nil || len(events) == 0 {
			return delivered, err
		}
		for _, e := range events {
			if err := r.sink.Write(ctx, e); err != nil {
				r.metrics.AuditFailed(ctx, name)
				return delivered, err
			}
			next := models.AuditCursor{At: e.OccurredAt, ID: e.ID}
			moved, err := r.store.AdvanceAuditCursor(ctx, name, cursor, next)
			if err != nil {
				return delivered, err
			}
			delivered++
			if !moved {
				// Another replica got further; it carries on from there, and the
				// sink drops the duplicate by event id.
				return delivered, nil
			}
			cursor = next
		}
		if len(events) < relayBatch {
			return delivered, nil
		}
	}
}

// newest is the cursor of the newest event in the trail, or now for an empty
// one: never the zero cursor, which AdvanceAuditCursor reads as "create".
func (r *Relay) newest(ctx context.Context) (models.AuditCursor, error) {
	events, err := r.store.ListAuditEvents(ctx, models.AuditFilter{Limit: 1})
	if err != nil {
		return models.AuditCursor{}, err
	}
	if len(events) == 0 {
		return models.AuditCursor{At: r.now()}, nil
	}
	return models.AuditCursor{At: events[0].OccurredAt, ID: events[0].ID}, nil
}

// Close releases the sink of a relay that will not run, as in a one-off command.
func (r *Relay) Close() error { return r.sink.Close() }
