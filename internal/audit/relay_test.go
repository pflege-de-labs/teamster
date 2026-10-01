package audit

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func insertAt(t *testing.T, st RelayStore, id string, at time.Time) {
	t.Helper()
	inserter, ok := st.(interface {
		InsertAuditEvent(ctx context.Context, e models.AuditEvent) error
	})
	if !ok {
		t.Fatal("the store cannot insert")
	}
	e := models.AuditEvent{ID: id, OccurredAt: at, Actor: models.Actor{Subject: "a", Via: models.ViaSystem}, Action: "template.update", ResourceType: TypeTemplate}
	if err := inserter.InsertAuditEvent(t.Context(), e); err != nil {
		t.Fatal(err)
	}
}

func ids(events []models.AuditEvent) []string {
	out := []string{}
	for _, e := range events {
		out = append(out, e.ID)
	}
	return out
}

func TestRelayStartsAtTheNewestAndCatchesUpAfterAnOutage(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	insertAt(t, st, "old-1", now.Add(-time.Hour))
	insertAt(t, st, "old-2", now.Add(-30*time.Minute))

	sink := &memorySink{name: "nats"}
	metrics := newCountingMetrics()
	r := NewRelay(discard(), st, sink, metrics, time.Second, 5*time.Second)
	r.now = func() time.Time { return now }

	// The first step only places the cursor: history is not replayed.
	if n, err := r.Step(t.Context()); n != 0 || err != nil {
		t.Fatalf("first step = %d, %v", n, err)
	}

	insertAt(t, st, "e1", now.Add(-20*time.Second))
	insertAt(t, st, "e2", now.Add(-10*time.Second))
	insertAt(t, st, "young", now.Add(-time.Second))

	// The sink is down: nothing is lost, the cursor stays put.
	sink.fail = errors.New("nats: no responders")
	if n, err := r.Step(t.Context()); n != 0 || err == nil {
		t.Fatalf("step during the outage = %d, %v", n, err)
	}
	if metrics.failed["nats"] != 1 {
		t.Errorf("failures counted = %d", metrics.failed["nats"])
	}

	// Back up: everything since the cursor arrives in order, the young one waits.
	sink.fail = nil
	if n, err := r.Step(t.Context()); n != 2 || err != nil {
		t.Fatalf("step after the outage = %d, %v", n, err)
	}
	if got := ids(sink.recorded()); !slices.Equal(got, []string{"e1", "e2"}) {
		t.Errorf("delivered %v", got)
	}

	// Once it has settled, the young one goes too, and only once.
	r.now = func() time.Time { return now.Add(time.Minute) }
	if n, _ := r.Step(t.Context()); n != 1 {
		t.Errorf("settled step delivered %d", n)
	}
	if n, _ := r.Step(t.Context()); n != 0 {
		t.Errorf("an idle step delivered %d", n)
	}
	if got := ids(sink.recorded()); !slices.Equal(got, []string{"e1", "e2", "young"}) {
		t.Errorf("delivered %v", got)
	}
}

func TestRelayDrainsALongBacklogInBatches(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	sink := &memorySink{name: "nats"}
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	r := NewRelay(discard(), st, sink, nil, time.Second, 0)
	r.now = func() time.Time { return now }
	if _, err := r.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := range relayBatch + 5 {
		insertAt(t, st, time.Duration(i).String(), now.Add(time.Duration(i)*time.Second))
	}
	r.now = func() time.Time { return now.Add(time.Hour) }
	if n, err := r.Step(t.Context()); n != relayBatch+5 || err != nil {
		t.Errorf("drained %d, %v; want all %d", n, err, relayBatch+5)
	}
}

// Two replicas relaying the same trail: each event is delivered, and a
// replica that lost the race to advance stops for this step.
func TestRelaysOnTwoReplicasShareTheCursor(t *testing.T) {
	t.Parallel()

	st := openStore(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a, b := &memorySink{name: "nats"}, &memorySink{name: "nats"}
	ra := NewRelay(discard(), st, a, nil, time.Second, 0)
	rb := NewRelay(discard(), st, b, nil, time.Second, 0)
	for _, r := range []*Relay{ra, rb} {
		r.now = func() time.Time { return now }
	}
	if _, err := ra.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	insertAt(t, st, "e1", now.Add(time.Second))
	insertAt(t, st, "e2", now.Add(2*time.Second))
	for _, r := range []*Relay{ra, rb} {
		r.now = func() time.Time { return now.Add(time.Minute) }
	}

	// b read the cursor before a moved it: it delivers e1 again and stops.
	cursor, _, _ := st.AuditCursor(t.Context(), "nats")
	if _, err := ra.Step(t.Context()); err != nil {
		t.Fatal(err)
	}
	if moved, _ := st.AdvanceAuditCursor(t.Context(), "nats", cursor, cursor); moved {
		t.Fatal("a stale cursor advanced")
	}
	if n, _ := rb.Step(t.Context()); n != 0 {
		t.Errorf("b delivered %d after a caught up", n)
	}
	if got := ids(a.recorded()); !slices.Equal(got, []string{"e1", "e2"}) {
		t.Errorf("a delivered %v", got)
	}
}

func TestRelayRunClosesItsSink(t *testing.T) {
	t.Parallel()

	sink := &memorySink{name: "nats"}
	r := NewRelay(discard(), openStore(t), sink, nil, time.Millisecond, 0)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Run(ctx)
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	<-done
	if !sink.closed {
		t.Error("the sink was not closed")
	}
}
