// Package samples remembers which label keys, label values and annotation keys
// incoming alerts carry, so the admin UI can offer them as completions while
// someone writes a template or a route. See ADR 0041.
//
// Observing an alert costs the delivery path a copy of its keys and a
// non-blocking channel send. Everything else -- the in-memory LRU that
// coalesces repeated tuples, the writes, the pruning -- happens on the single
// worker goroutine Run starts, so a slow or broken database delays nothing but
// the samples themselves.
package samples

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// Store is the slice of store.Store the sampler writes through.
type Store interface {
	RecordAlertSamples(ctx context.Context, samples []models.AlertSample) error
	PruneAlertSamples(ctx context.Context, cutoff time.Time, keepPerKey int) (int64, error)
}

const (
	// A burst larger than this is dropped rather than queued: the samples are
	// a convenience, and memory held for them is memory delivery cannot use.
	queueSize = 1024
	// Hourly, like the session sweep; pruning is idempotent, so every replica
	// running it on a shared database is harmless.
	pruneInterval = time.Hour
	// Pending counts are written on shutdown within this, so a stuck database
	// cannot hold the process past its drain deadline.
	flushTimeout = 5 * time.Second
)

// tuple is what one sample row is keyed on.
type tuple struct {
	kind  models.AlertSampleKind
	key   string
	value string
}

// entry is what the LRU remembers of a tuple between writes. first and last
// bound the pending observations, so a flush on shutdown does not make every
// tuple look as if it had just been seen.
type entry struct {
	written time.Time
	pending int64
	first   time.Time
	last    time.Time
}

func (e *entry) observe(at time.Time) {
	if e.pending == 0 {
		e.first = at
	}
	e.pending++
	e.last = at
}

// Sampler observes alerts and writes what they carried through Store. Its zero
// value is not usable; build one with New.
type Sampler struct {
	store   Store
	cfg     config.SamplesConfig
	now     func() time.Time
	queue   chan observation
	dropped atomic.Int64

	// cache and spill belong to the worker goroutine alone.
	cache *lru.Cache[tuple, *entry]
	spill []models.AlertSample
}

// New builds a sampler. A disabled one observes nothing and Run returns at
// once, so callers need not check the configuration themselves.
func New(st Store, cfg config.SamplesConfig) (*Sampler, error) {
	s := &Sampler{
		store: st,
		cfg:   cfg,
		now:   func() time.Time { return time.Now().UTC() },
		queue: make(chan observation, queueSize),
	}
	if !cfg.Enabled {
		return s, nil
	}
	// An evicted tuple still owes its count; the worker writes it with the
	// next batch rather than losing it.
	cache, err := lru.NewWithEvict(cfg.LRUSize, func(t tuple, e *entry) {
		if e.pending > 0 {
			s.spill = append(s.spill, sampleOf(t, e))
		}
	})
	if err != nil {
		return nil, err
	}
	s.cache = cache
	return s, nil
}

// observation is one alert's tuples and when it arrived.
type observation struct {
	at     time.Time
	tuples []tuple
}

// Observe queues one alert's labels and annotation keys. It never blocks: when
// the worker has fallen behind, the alert is not sampled.
func (s *Sampler) Observe(labels, annotations map[string]string) {
	if s == nil || !s.cfg.Enabled {
		return
	}
	tuples := make([]tuple, 0, len(labels)+len(annotations))
	for key, value := range labels {
		if key == "" || len(value) > s.cfg.MaxValueLength {
			continue
		}
		tuples = append(tuples, tuple{kind: models.SampleLabel, key: key, value: value})
	}
	for key := range annotations {
		if key == "" {
			continue
		}
		// The value is deliberately dropped here, before it can reach a queue.
		tuples = append(tuples, tuple{kind: models.SampleAnnotation, key: key})
	}
	if len(tuples) == 0 {
		return
	}
	select {
	case s.queue <- observation{at: s.now(), tuples: tuples}:
	default:
		s.dropped.Add(1)
	}
}

// Run writes observed samples and prunes old ones until ctx is cancelled, then
// writes whatever counts are still pending and returns.
func (s *Sampler) Run(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()

	s.prune(ctx)
	for {
		select {
		case <-ctx.Done():
			s.drainQueue()
			s.flushPending(ctx)
			return
		case obs := <-s.queue:
			s.record(ctx, obs)
		case <-ticker.C:
			s.prune(ctx)
		}
	}
}

// record counts one alert's tuples and writes those that are new, or whose
// last write is at least a flush interval old.
func (s *Sampler) record(ctx context.Context, obs observation) {
	if dropped := s.dropped.Swap(0); dropped > 0 {
		log.Printf("samples: dropped %d alerts while the writer was behind", dropped)
	}

	now := obs.at
	var due []*entry
	var batch []models.AlertSample
	for _, t := range obs.tuples {
		e := s.entryFor(t)
		e.observe(now)
		if now.Sub(e.written) < s.cfg.FlushInterval {
			continue
		}
		batch = append(batch, sampleOf(t, e))
		due = append(due, e)
	}
	batch = append(batch, s.spill...)
	s.spill = nil
	if len(batch) == 0 {
		return
	}

	// Marked written even on failure, so a broken database is retried once
	// per flush interval rather than on every alert.
	err := s.store.RecordAlertSamples(ctx, batch)
	for _, e := range due {
		e.written = now
		if err == nil {
			e.pending = 0
		}
	}
	if err != nil && ctx.Err() == nil {
		log.Printf("samples: %v", err)
	}
}

// drainQueue counts what was observed but not yet taken, so the final flush
// includes it.
func (s *Sampler) drainQueue() {
	for {
		select {
		case obs := <-s.queue:
			for _, t := range obs.tuples {
				s.entryFor(t).observe(obs.at)
			}
		default:
			return
		}
	}
}

func (s *Sampler) entryFor(t tuple) *entry {
	e, ok := s.cache.Get(t)
	if !ok {
		e = &entry{}
		s.cache.Add(t, e)
	}
	return e
}

// flushPending writes the counts accumulated since each tuple's last write, so
// a rolling restart does not reset every label's count to its last flush.
func (s *Sampler) flushPending(ctx context.Context) {
	batch := s.spill
	s.spill = nil
	for _, t := range s.cache.Keys() {
		if e, ok := s.cache.Peek(t); ok && e.pending > 0 {
			batch = append(batch, sampleOf(t, e))
		}
	}
	if len(batch) == 0 {
		return
	}
	flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flushTimeout)
	defer cancel()
	if err := s.store.RecordAlertSamples(flushCtx, batch); err != nil {
		log.Printf("samples: final flush: %v", err)
	}
}

func (s *Sampler) prune(ctx context.Context) {
	cutoff := s.now().Add(-s.cfg.Retention)
	if _, err := s.store.PruneAlertSamples(ctx, cutoff, s.cfg.MaxValuesPerKey); err != nil && ctx.Err() == nil {
		log.Printf("samples: prune: %v", err)
	}
}

func sampleOf(t tuple, e *entry) models.AlertSample {
	return models.AlertSample{
		Kind: t.kind, Key: t.key, Value: t.value,
		SeenCount: e.pending, FirstSeen: e.first, LastSeen: e.last,
	}
}
