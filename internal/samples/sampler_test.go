package samples

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
)

type fakeStore struct {
	mu      sync.Mutex
	batches [][]models.AlertSample
	prunes  []time.Time
	keep    int
	fail    error
}

func (f *fakeStore) RecordAlertSamples(_ context.Context, samples []models.AlertSample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, append([]models.AlertSample(nil), samples...))
	return f.fail
}

func (f *fakeStore) PruneAlertSamples(_ context.Context, cutoff time.Time, keep int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prunes = append(f.prunes, cutoff)
	f.keep = keep
	return 0, f.fail
}

// counts sums every batch by tuple, which is what the upsert does.
func (f *fakeStore) counts() map[string]int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]int64{}
	for _, batch := range f.batches {
		for _, s := range batch {
			out[string(s.Kind)+"/"+s.Key+"="+s.Value] += s.SeenCount
		}
	}
	return out
}

func enabled() config.SamplesConfig {
	return config.SamplesConfig{
		Enabled: true, Retention: 24 * time.Hour, MaxValuesPerKey: 10,
		MaxValueLength: 8, LRUSize: 16, FlushInterval: time.Minute,
	}
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func newSampler(t *testing.T, st Store, cfg config.SamplesConfig) (*Sampler, *clock) {
	t.Helper()
	s, err := New(st, cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := &clock{t: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)}
	s.now = c.now
	return s, c
}

func drain(s *Sampler) [][]tuple {
	var out [][]tuple
	for {
		select {
		case obs := <-s.queue:
			out = append(out, obs.tuples)
		default:
			return out
		}
	}
}

func TestObserveKeepsOnlyWhatMayBeStored(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         config.SamplesConfig
		labels      map[string]string
		annotations map[string]string
		want        []tuple
	}{
		{
			name:        "labels with values, annotations by key only",
			cfg:         enabled(),
			labels:      map[string]string{"env": "prod"},
			annotations: map[string]string{"summary": "disk on db-1 holds customer 4711"},
			want: []tuple{
				{kind: models.SampleLabel, key: "env", value: "prod"},
				{kind: models.SampleAnnotation, key: "summary"},
			},
		},
		{
			name:   "an overlong label value is skipped",
			cfg:    enabled(),
			labels: map[string]string{"pod": "worker-7d9f8c-abcde", "": "x"},
			want:   nil,
		},
		{
			name:   "disabled observes nothing",
			cfg:    config.SamplesConfig{},
			labels: map[string]string{"env": "prod"},
			want:   nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, _ := newSampler(t, &fakeStore{}, tt.cfg)
			s.Observe(tt.labels, tt.annotations)

			var got []tuple
			for _, batch := range drain(s) {
				got = append(got, batch...)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("queued %+v, want %+v", got, tt.want)
			}
			seen := map[tuple]bool{}
			for _, g := range got {
				seen[g] = true
			}
			for _, w := range tt.want {
				if !seen[w] {
					t.Errorf("queued %+v, want %+v among them", got, w)
				}
			}
		})
	}
}

func TestObserveOnANilSamplerIsANoOp(t *testing.T) {
	t.Parallel()

	var s *Sampler
	s.Observe(map[string]string{"a": "b"}, nil)
}

func TestNewRefusesAnUnusableCache(t *testing.T) {
	t.Parallel()

	cfg := enabled()
	cfg.LRUSize = 0
	if _, err := New(&fakeStore{}, cfg); err == nil {
		t.Error("New with lru-size 0 succeeded, want an error")
	}
}

func TestRecordCoalescesWritesWithinTheFlushInterval(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	s, c := newSampler(t, st, enabled())
	env := []tuple{{kind: models.SampleLabel, key: "env", value: "prod"}}

	s.record(t.Context(), observation{at: c.t, tuples: env})
	s.record(t.Context(), observation{at: c.t, tuples: env})
	s.record(t.Context(), observation{at: c.t, tuples: env})
	if got := len(st.batches); got != 1 {
		t.Fatalf("%d writes within the flush interval, want 1", got)
	}

	c.t = c.t.Add(time.Minute)
	s.record(t.Context(), observation{at: c.t, tuples: env})
	if got := len(st.batches); got != 2 {
		t.Fatalf("%d writes after the flush interval, want 2", got)
	}
	if got := st.counts()["label/env=prod"]; got != 4 {
		t.Errorf("stored count = %d, want 4, one per observation", got)
	}
}

func TestAnEvictedTupleKeepsItsCount(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	cfg := enabled()
	cfg.LRUSize = 1
	s, c := newSampler(t, st, cfg)
	a := tuple{kind: models.SampleLabel, key: "a", value: "1"}
	b := tuple{kind: models.SampleLabel, key: "b", value: "1"}

	s.record(t.Context(), observation{at: c.t, tuples: []tuple{a}})
	s.record(t.Context(), observation{at: c.t, tuples: []tuple{a}})
	s.record(t.Context(), observation{at: c.t, tuples: []tuple{b}})

	counts := st.counts()
	if counts["label/a=1"] != 2 || counts["label/b=1"] != 1 {
		t.Errorf("counts = %v, want a=2 and b=1", counts)
	}
}

func TestAFailedWriteIsRetriedWithItsCount(t *testing.T) {
	t.Parallel()

	st := &fakeStore{fail: errors.New("database gone")}
	s, c := newSampler(t, st, enabled())
	env := []tuple{{kind: models.SampleLabel, key: "env", value: "prod"}}

	s.record(t.Context(), observation{at: c.t, tuples: env})
	s.record(t.Context(), observation{at: c.t, tuples: env})
	if got := len(st.batches); got != 1 {
		t.Fatalf("%d attempts, want 1: a failure must not retry on every alert", got)
	}

	st.mu.Lock()
	st.fail = nil
	st.batches = nil
	st.mu.Unlock()
	c.t = c.t.Add(time.Minute)
	s.record(t.Context(), observation{at: c.t, tuples: env})
	if got := st.counts()["label/env=prod"]; got != 3 {
		t.Errorf("count after recovery = %d, want 3", got)
	}
}

func TestObserveDropsRatherThanBlocks(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	s, _ := newSampler(t, st, enabled())
	for range queueSize + 5 {
		s.Observe(map[string]string{"env": "prod"}, nil)
	}
	if got := s.dropped.Load(); got != 5 {
		t.Errorf("dropped = %d, want 5", got)
	}

	// The next write reports the drops and resets the counter.
	s.record(t.Context(), <-s.queue)
	if got := s.dropped.Load(); got != 0 {
		t.Errorf("dropped after record = %d, want 0", got)
	}
}

func TestRunWritesPrunesAndFlushesOnShutdown(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	s, c := newSampler(t, st, enabled())
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		s.Run(ctx)
		close(done)
	}()

	s.Observe(map[string]string{"env": "prod"}, map[string]string{"summary": "text"})
	s.Observe(map[string]string{"env": "prod"}, nil)
	deadline := time.After(5 * time.Second)
	for st.counts()["label/env=prod"] == 0 {
		select {
		case <-deadline:
			t.Fatal("the worker wrote nothing")
		case <-time.After(5 * time.Millisecond):
		}
	}
	// The second observation is counted whether or not the worker took it.
	cancel()
	<-done

	counts := st.counts()
	if counts["label/env=prod"] != 2 || counts["annotation/summary="] != 1 {
		t.Errorf("counts = %v, want env=prod twice and summary once", counts)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.prunes) == 0 {
		t.Fatal("Run did not prune on start")
	}
	if want := c.t.Add(-24 * time.Hour); !st.prunes[0].Equal(want) || st.keep != 10 {
		t.Errorf("prune(%s, %d), want (%s, 10)", st.prunes[0], st.keep, want)
	}
	for _, batch := range st.batches {
		for _, sample := range batch {
			if sample.Kind == models.SampleAnnotation && sample.Value != "" {
				t.Errorf("an annotation value reached the store: %+v", sample)
			}
			if strings.Contains(sample.Value, "text") {
				t.Errorf("annotation text reached the store: %+v", sample)
			}
		}
	}
}

func TestRunReturnsAtOnceWhenDisabled(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	s, _ := newSampler(t, st, config.SamplesConfig{})
	s.Run(t.Context())
	if len(st.prunes) != 0 || len(st.batches) != 0 {
		t.Errorf("a disabled sampler touched the store: %+v", st)
	}
}

// A tuple flushed on shutdown keeps the time it was last seen, not the time
// the process stopped, so a restart does not reorder recency.
func TestAFlushKeepsWhenATupleWasSeen(t *testing.T) {
	t.Parallel()

	st := &fakeStore{}
	s, c := newSampler(t, st, enabled())
	env := []tuple{{kind: models.SampleLabel, key: "env", value: "prod"}}
	seen := c.t

	s.record(t.Context(), observation{at: seen, tuples: env})
	s.record(t.Context(), observation{at: seen.Add(time.Second), tuples: env})
	c.t = seen.Add(time.Hour)
	s.flushPending(t.Context())

	last := st.batches[len(st.batches)-1][0]
	if !last.LastSeen.Equal(seen.Add(time.Second)) || !last.FirstSeen.Equal(seen.Add(time.Second)) || last.SeenCount != 1 {
		t.Errorf("flushed %+v, want one observation seen at %s", last, seen.Add(time.Second))
	}
}
