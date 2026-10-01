// Package audit records who changed the configuration, and how. See ADR 0070.
package audit

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// A Sink is somewhere audit events go. Write is never called concurrently.
type Sink interface {
	Name() string
	Write(ctx context.Context, e models.AuditEvent) error
	Close() error
}

// Metrics counts what the recorder could not deliver, by sink.
type Metrics interface {
	AuditFailed(ctx context.Context, sink string)
	AuditDropped(ctx context.Context, sink string)
}

type noMetrics struct{}

func (noMetrics) AuditFailed(context.Context, string)  {}
func (noMetrics) AuditDropped(context.Context, string) {}

// writeTimeout keeps a hanging sink from holding a request or the shutdown.
const writeTimeout = 5 * time.Second

type actorKey struct{}
type requestKey struct{}

// WithActor says who is acting for everything recorded under ctx.
func WithActor(ctx context.Context, actor models.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// WithRequestID ties what is recorded under ctx to the request's log lines.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestKey{}, id)
}

// ActorFrom falls back to Teamster itself, for changes no request asked for.
func ActorFrom(ctx context.Context) models.Actor {
	if actor, ok := ctx.Value(actorKey{}).(models.Actor); ok && actor.Subject != "" {
		return actor
	}
	return models.Actor{Subject: "teamster", Via: models.ViaSystem}
}

// A Recorder fans events out to the sinks. A nil Recorder records nothing.
type Recorder struct {
	log     *slog.Logger
	metrics Metrics
	now     func() time.Time
	primary Sink
	queues  []*queue
	wg      sync.WaitGroup

	// mu keeps Record from sending on a queue Close has closed.
	mu      sync.RWMutex
	stopped bool
}

type queue struct {
	sink   Sink
	events chan models.AuditEvent
}

// NewRecorder writes primary, when not nil, before Record returns, so the UI
// shows a change at once; async sinks get a queue each and can only lag.
func NewRecorder(logger *slog.Logger, metrics Metrics, queueSize int, primary Sink, async ...Sink) *Recorder {
	if metrics == nil {
		metrics = noMetrics{}
	}
	r := &Recorder{
		log:     logger,
		metrics: metrics,
		now:     func() time.Time { return time.Now().UTC() },
		primary: primary,
	}
	for _, sink := range async {
		q := &queue{sink: sink, events: make(chan models.AuditEvent, queueSize)}
		r.queues = append(r.queues, q)
		r.wg.Add(1)
		go r.drain(q)
	}
	return r
}

// Stamp fills in the id, the time, and the actor and request from ctx.
func (r *Recorder) Stamp(ctx context.Context, e models.AuditEvent) models.AuditEvent {
	if e.ID == "" {
		// Version 7 sorts by time, the order the trail is read in.
		if id, err := uuid.NewV7(); err == nil {
			e.ID = id.String()
		} else {
			e.ID = uuid.NewString()
		}
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = r.now()
	}
	if e.Actor.Subject == "" {
		e.Actor = ActorFrom(ctx)
	}
	if e.RequestID == "" {
		e.RequestID, _ = ctx.Value(requestKey{}).(string)
	}
	return e
}

// Record never fails the caller: the change has already been made, so a sink
// that refuses it is logged and counted instead.
func (r *Recorder) Record(ctx context.Context, e models.AuditEvent) {
	if r == nil {
		return
	}
	r.deliver(ctx, r.Stamp(ctx, e))
}

func (r *Recorder) deliver(ctx context.Context, e models.AuditEvent) {
	if r.primary != nil {
		// Detached, so a client hanging up does not take the record with it.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), writeTimeout)
		if err := r.primary.Write(writeCtx, e); err != nil {
			r.failed(writeCtx, r.primary, e, err)
		}
		cancel()
	}

	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.stopped {
		return
	}
	for _, q := range r.queues {
		select {
		case q.events <- e:
		default:
			r.metrics.AuditDropped(ctx, q.sink.Name())
			r.log.Warn("audit queue full, event dropped", "sink", q.sink.Name(), "audit_id", e.ID, "action", e.Action)
		}
	}
}

func (r *Recorder) drain(q *queue) {
	defer r.wg.Done()
	for e := range q.events {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		if err := q.sink.Write(ctx, e); err != nil {
			r.failed(ctx, q.sink, e, err)
		}
		cancel()
	}
}

func (r *Recorder) failed(ctx context.Context, sink Sink, e models.AuditEvent, err error) {
	r.metrics.AuditFailed(ctx, sink.Name())
	r.log.Error("audit write failed", "sink", sink.Name(), "audit_id", e.ID, "action", e.Action, "err", err)
}

// Close drains the queues until ctx is done, then closes every sink.
func (r *Recorder) Close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return nil
	}
	r.stopped = true
	for _, q := range r.queues {
		close(q.events)
	}
	r.mu.Unlock()

	drained := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(drained)
	}()
	var err error
	select {
	case <-drained:
	case <-ctx.Done():
		err = ctx.Err()
	}

	sinks := make([]Sink, 0, len(r.queues)+1)
	for _, q := range r.queues {
		sinks = append(sinks, q.sink)
	}
	if r.primary != nil {
		sinks = append(sinks, r.primary)
	}
	for _, sink := range sinks {
		if closeErr := sink.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}
