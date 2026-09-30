package store_test

import (
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func label(key, value string, count int64, first, last time.Time) models.EventSample {
	return models.EventSample{Kind: models.SampleLabel, Key: key, Value: value, SeenCount: count, FirstSeen: first, LastSeen: last}
}

func sampleIndex(samples []models.EventSample) map[string]models.EventSample {
	out := map[string]models.EventSample{}
	for _, s := range samples {
		out[string(s.Kind)+"/"+s.Key+"="+s.Value] = s
	}
	return out
}

func TestConformanceEventSamplesAccumulate(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		first := []models.EventSample{
			label("severity", "critical", 2, t0, t0),
			{Kind: models.SampleAttribute, Key: "summary", SeenCount: 1, FirstSeen: t0, LastSeen: t0},
		}
		if err := st.RecordEventSamples(ctx, first); err != nil {
			t.Fatalf("RecordEventSamples: %v", err)
		}
		// An older last_seen, as a replica with a slow clock would send.
		second := []models.EventSample{
			label("severity", "critical", 3, t0.Add(time.Hour), t0.Add(time.Hour)),
			label("severity", "critical", 1, t0.Add(-time.Hour), t0.Add(-time.Hour)),
		}
		if err := st.RecordEventSamples(ctx, second); err != nil {
			t.Fatalf("RecordEventSamples again: %v", err)
		}
		if err := st.RecordEventSamples(ctx, nil); err != nil {
			t.Fatalf("RecordEventSamples(nil): %v", err)
		}

		got, err := st.ListEventSamples(ctx, 100)
		if err != nil {
			t.Fatalf("ListEventSamples: %v", err)
		}
		index := sampleIndex(got)
		if len(got) != 2 {
			t.Fatalf("ListEventSamples() = %+v, want two rows", got)
		}

		sev := index["label/severity=critical"]
		tests := []struct {
			name      string
			got, want any
		}{
			{"counts add up", sev.SeenCount, int64(6)},
			{"first_seen is the first write", sev.FirstSeen.UTC(), t0},
			{"last_seen never moves back", sev.LastSeen.UTC(), t0.Add(time.Hour)},
			{"attribute keeps no value", index["attribute/summary="].Value, ""},
			{"attribute count", index["attribute/summary="].SeenCount, int64(1)},
		}
		for _, tt := range tests {
			if tt.got != tt.want {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
			}
		}

		limited, err := st.ListEventSamples(ctx, 1)
		if err != nil {
			t.Fatalf("ListEventSamples(1): %v", err)
		}
		if len(limited) != 1 {
			t.Errorf("ListEventSamples(1) returned %d rows", len(limited))
		}
	})
}

func TestConformanceEventSamplesListNewestFirstWithinAKey(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		if err := st.RecordEventSamples(ctx, []models.EventSample{
			label("env", "dev", 1, t0, t0),
			label("env", "prod", 1, t0.Add(2*time.Minute), t0.Add(2*time.Minute)),
			label("env", "stage", 1, t0.Add(time.Minute), t0.Add(time.Minute)),
		}); err != nil {
			t.Fatalf("RecordEventSamples: %v", err)
		}

		got, err := st.ListEventSamples(ctx, 10)
		if err != nil {
			t.Fatalf("ListEventSamples: %v", err)
		}
		var values []string
		for _, s := range got {
			values = append(values, s.Value)
		}
		want := []string{"prod", "stage", "dev"}
		if len(values) != len(want) {
			t.Fatalf("values = %v, want %v", values, want)
		}
		for i := range want {
			if values[i] != want[i] {
				t.Errorf("values = %v, want %v", values, want)
				break
			}
		}
	})
}

func TestConformanceEventSamplesPrune(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

		samples := []models.EventSample{
			label("stale", "x", 1, t0.Add(-48*time.Hour), t0.Add(-48*time.Hour)),
			{Kind: models.SampleAttribute, Key: "old", SeenCount: 1, FirstSeen: t0.Add(-48 * time.Hour), LastSeen: t0.Add(-48 * time.Hour)},
			{Kind: models.SampleAttribute, Key: "fresh", SeenCount: 1, FirstSeen: t0, LastSeen: t0},
			label("pod", "a", 1, t0, t0.Add(1*time.Minute)),
			label("pod", "b", 1, t0, t0.Add(2*time.Minute)),
			// Three at the same instant: the tie-break on value keeps c and d.
			label("pod", "e", 1, t0, t0.Add(4*time.Minute)),
			label("pod", "d", 1, t0, t0.Add(4*time.Minute)),
			label("pod", "c", 1, t0, t0.Add(4*time.Minute)),
			label("env", "prod", 1, t0, t0),
		}
		if err := st.RecordEventSamples(ctx, samples); err != nil {
			t.Fatalf("RecordEventSamples: %v", err)
		}

		deleted, err := st.PruneEventSamples(ctx, t0.Add(-24*time.Hour), 2)
		if err != nil {
			t.Fatalf("PruneEventSamples: %v", err)
		}
		// Two expired, and pod loses a, b and e.
		if deleted != 5 {
			t.Errorf("PruneEventSamples deleted %d rows, want 5", deleted)
		}

		// A second replica running the same prune finds nothing left to do.
		again, err := st.PruneEventSamples(ctx, t0.Add(-24*time.Hour), 2)
		if err != nil {
			t.Fatalf("PruneEventSamples again: %v", err)
		}
		if again != 0 {
			t.Errorf("second PruneEventSamples deleted %d rows, want 0", again)
		}

		got, err := st.ListEventSamples(ctx, 100)
		if err != nil {
			t.Fatalf("ListEventSamples: %v", err)
		}
		index := sampleIndex(got)
		for _, want := range []string{"label/pod=c", "label/pod=d", "label/env=prod", "attribute/fresh="} {
			if _, ok := index[want]; !ok {
				t.Errorf("%s was pruned, want it kept; left %v", want, got)
			}
		}
		if len(got) != 4 {
			t.Errorf("%d rows remain, want 4: %+v", len(got), got)
		}
	})
}
