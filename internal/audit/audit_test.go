package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type memorySink struct {
	name   string
	mu     sync.Mutex
	events []models.AuditEvent
	fail   error
	block  chan struct{}
	closed bool
}

func (s *memorySink) Name() string { return s.name }

func (s *memorySink) Write(_ context.Context, e models.AuditEvent) error {
	if s.block != nil {
		<-s.block
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.events = append(s.events, e)
	return nil
}

func (s *memorySink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *memorySink) recorded() []models.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]models.AuditEvent(nil), s.events...)
}

type countingMetrics struct {
	mu      sync.Mutex
	failed  map[string]int
	dropped map[string]int
}

func newCountingMetrics() *countingMetrics {
	return &countingMetrics{failed: map[string]int{}, dropped: map[string]int{}}
}

func (m *countingMetrics) AuditFailed(_ context.Context, sink string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failed[sink]++
}

func (m *countingMetrics) AuditDropped(_ context.Context, sink string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropped[sink]++
}

func TestStampFillsFromContext(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	alice := models.Actor{Subject: "alice", Name: "Alice", Via: models.ViaSession}
	tests := []struct {
		name      string
		ctx       context.Context
		in        models.AuditEvent
		wantActor models.Actor
		wantReq   string
		wantAt    time.Time
		wantID    string
	}{
		{
			name:      "no actor is teamster itself",
			ctx:       context.Background(),
			wantActor: models.Actor{Subject: "teamster", Via: models.ViaSystem},
			wantAt:    at,
		},
		{
			name:      "actor and request from the context",
			ctx:       WithRequestID(WithActor(context.Background(), alice), "req-1"),
			wantActor: alice,
			wantReq:   "req-1",
			wantAt:    at,
		},
		{
			name:      "what the caller set is kept",
			ctx:       WithRequestID(WithActor(context.Background(), alice), "req-1"),
			in:        models.AuditEvent{ID: "fixed", OccurredAt: at.Add(time.Hour), RequestID: "req-0", Actor: models.Actor{Subject: "bob", Via: models.ViaBasic}},
			wantActor: models.Actor{Subject: "bob", Via: models.ViaBasic},
			wantReq:   "req-0",
			wantAt:    at.Add(time.Hour),
			wantID:    "fixed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := NewRecorder(discard(), nil, 1, nil)
			r.now = func() time.Time { return at }

			got := r.Stamp(tt.ctx, tt.in)
			if got.Actor != tt.wantActor || got.RequestID != tt.wantReq || !got.OccurredAt.Equal(tt.wantAt) {
				t.Errorf("Stamp() = %+v", got)
			}
			if tt.wantID != "" && got.ID != tt.wantID {
				t.Errorf("ID = %q, want %q", got.ID, tt.wantID)
			}
			if got.ID == "" {
				t.Error("Stamp left the id empty")
			}
		})
	}
}

func TestRecorderDelivers(t *testing.T) {
	t.Parallel()

	primary := &memorySink{name: "database"}
	file := &memorySink{name: "file"}
	stream := &memorySink{name: "stream"}
	r := NewRecorder(discard(), nil, 8, primary, file, stream)

	r.Record(t.Context(), models.AuditEvent{Action: "template.create", ResourceType: TypeTemplate, ResourceID: "t1"})
	if got := primary.recorded(); len(got) != 1 {
		t.Fatalf("primary has %d events before Record returned, want 1", len(got))
	}
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	tests := []struct {
		name string
		sink *memorySink
	}{
		{"primary", primary},
		{"first async", file},
		{"second async", stream},
	}
	id := primary.recorded()[0].ID
	for _, tt := range tests {
		got := tt.sink.recorded()
		if len(got) != 1 || got[0].ID != id || got[0].Action != "template.create" {
			t.Errorf("%s: got %+v, want the one event %s", tt.name, got, id)
		}
		if !tt.sink.closed {
			t.Errorf("%s: not closed", tt.name)
		}
	}
}

func TestRecorderFailuresNeverReachTheCaller(t *testing.T) {
	t.Parallel()

	metrics := newCountingMetrics()
	primary := &memorySink{name: "database", fail: errors.New("disk full")}
	async := &memorySink{name: "file", fail: errors.New("gone")}
	r := NewRecorder(discard(), metrics, 8, primary, async)

	r.Record(t.Context(), models.AuditEvent{Action: "route.delete"})
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if metrics.failed["database"] != 1 || metrics.failed["file"] != 1 {
		t.Errorf("failed = %v, want one per sink", metrics.failed)
	}
}

func TestRecorderDropsWhenAQueueIsFull(t *testing.T) {
	t.Parallel()

	metrics := newCountingMetrics()
	slow := &memorySink{name: "stream", block: make(chan struct{})}
	r := NewRecorder(discard(), metrics, 1, nil, slow)

	// One is taken by the blocked writer, one fills the queue, the rest drop.
	for range 4 {
		r.Record(t.Context(), models.AuditEvent{Action: "template.update"})
	}
	close(slow.block)
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	delivered := len(slow.recorded())
	if delivered+metrics.dropped["stream"] != 4 || metrics.dropped["stream"] < 2 {
		t.Errorf("delivered %d, dropped %d, want 4 in total and at least 2 dropped", delivered, metrics.dropped["stream"])
	}
}

func TestRecorderAfterClose(t *testing.T) {
	t.Parallel()

	async := &memorySink{name: "file"}
	r := NewRecorder(discard(), nil, 1, nil, async)
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	r.Record(t.Context(), models.AuditEvent{Action: "late"})
	if err := r.Close(t.Context()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := async.recorded(); len(got) != 0 {
		t.Errorf("recorded %v after Close", got)
	}

	var none *Recorder
	none.Record(t.Context(), models.AuditEvent{Action: "ignored"})
	if err := none.Close(t.Context()); err != nil {
		t.Errorf("nil Close: %v", err)
	}
}

func TestRecorderCloseGivesUpAtTheDeadline(t *testing.T) {
	t.Parallel()

	stuck := &memorySink{name: "stream", block: make(chan struct{})}
	defer close(stuck.block)
	r := NewRecorder(discard(), nil, 4, nil, stuck)
	r.Record(t.Context(), models.AuditEvent{Action: "template.update"})

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Close() = %v, want the deadline", err)
	}
}

func TestFileSinkWritesJSONLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	// Appends rather than truncating, so a restart keeps what was there.
	if err := os.WriteFile(path, []byte(`{"id":"earlier"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sink, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	events := []models.AuditEvent{
		{ID: "1", Action: "template.create", After: json.RawMessage(`{"name":"x"}`)},
		{ID: "2", Action: "template.delete", Before: json.RawMessage(`{"name":"x"}`)},
	}
	for _, e := range events {
		if err := sink.Write(t.Context(), e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := sink.Write(t.Context(), events[0]); !errors.Is(err, os.ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var ids []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var e models.AuditEvent
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			t.Fatalf("line %q is not JSON: %v", scanner.Text(), err)
		}
		ids = append(ids, e.ID)
	}
	if len(ids) != 3 || ids[0] != "earlier" || ids[1] != "1" || ids[2] != "2" {
		t.Errorf("ids = %v", ids)
	}

	stdout, err := OpenFile("-")
	if err != nil || stdout.Name() != "file" {
		t.Fatalf("OpenFile(-) = %v, %v", stdout, err)
	}
	if err := stdout.Close(); err != nil {
		t.Errorf("closing stdout sink: %v", err)
	}
	if _, err := OpenFile(filepath.Join(t.TempDir(), "missing", "audit.jsonl")); err == nil {
		t.Error("OpenFile in a missing directory succeeded")
	}
}

type pruneStore struct {
	mu      sync.Mutex
	inserts []models.AuditEvent
	cutoffs []time.Time
	keeps   []int
	fail    error
}

func (s *pruneStore) InsertAuditEvent(_ context.Context, e models.AuditEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inserts = append(s.inserts, e)
	return s.fail
}

func (s *pruneStore) PruneAuditEvents(_ context.Context, cutoff time.Time, keep int) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cutoffs = append(s.cutoffs, cutoff)
	s.keeps = append(s.keeps, keep)
	return 1, s.fail
}

func TestPrunerLimits(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		age        time.Duration
		count      int
		wantCutoff time.Time
		wantKeep   int
	}{
		{"age only", 24 * time.Hour, 0, now.Add(-24 * time.Hour), 0},
		{"count only", 0, 100, time.Time{}, 100},
		{"both", time.Hour, 5, now.Add(-time.Hour), 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := &pruneStore{}
			p := NewPruner(discard(), st, tt.age, tt.count, time.Hour)
			p.now = func() time.Time { return now }
			if _, err := p.Prune(t.Context()); err != nil {
				t.Fatalf("Prune: %v", err)
			}
			if !st.cutoffs[0].Equal(tt.wantCutoff) || st.keeps[0] != tt.wantKeep {
				t.Errorf("pruned with cutoff %v keep %d, want %v and %d", st.cutoffs[0], st.keeps[0], tt.wantCutoff, tt.wantKeep)
			}
		})
	}
}

func TestPrunerRun(t *testing.T) {
	t.Parallel()

	t.Run("no limits returns at once", func(t *testing.T) {
		t.Parallel()
		st := &pruneStore{}
		NewPruner(discard(), st, 0, 0, time.Hour).Run(t.Context())
		if len(st.cutoffs) != 0 {
			t.Errorf("pruned %d times with no limit", len(st.cutoffs))
		}
	})
	t.Run("prunes until cancelled", func(t *testing.T) {
		t.Parallel()
		st := &pruneStore{fail: errors.New("locked")}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			NewPruner(discard(), st, time.Hour, 0, time.Millisecond).Run(ctx)
		}()
		deadline := time.After(5 * time.Second)
		for {
			st.mu.Lock()
			n := len(st.cutoffs)
			st.mu.Unlock()
			if n >= 2 {
				break
			}
			select {
			case <-deadline:
				t.Fatal("pruner never ran twice")
			case <-time.After(time.Millisecond):
			}
		}
		cancel()
		<-done
	})
}

func TestDBSink(t *testing.T) {
	t.Parallel()

	st := &pruneStore{}
	sink := NewDBSink(st)
	if err := sink.Write(t.Context(), models.AuditEvent{ID: "x"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if sink.Name() != "database" || sink.Close() != nil || len(st.inserts) != 1 {
		t.Errorf("sink %q wrote %d events", sink.Name(), len(st.inserts))
	}
}
