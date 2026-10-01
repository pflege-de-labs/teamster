package store_test

import (
	"slices"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func TestConformanceAuditEventsAfter(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		cursor models.AuditCursor
		until  time.Time
		limit  int
		want   []string
	}{
		{"from the start, oldest first", models.AuditCursor{}, t0.Add(24 * time.Hour), 0, []string{"a", "b", "c", "d", "e"}},
		{"after a cursor inside a shared instant", models.AuditCursor{At: t0.Add(time.Hour), ID: "b"}, t0.Add(24 * time.Hour), 0, []string{"c", "d", "e"}},
		{"until holds back the young", models.AuditCursor{}, t0.Add(2 * time.Hour), 0, []string{"a", "b", "c"}},
		{"a page", models.AuditCursor{}, t0.Add(24 * time.Hour), 2, []string{"a", "b"}},
		{"past the end", models.AuditCursor{At: t0.Add(3 * time.Hour), ID: "e"}, t0.Add(24 * time.Hour), 0, []string{}},
	}
	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		seedAudit(t, st, t0)
		for _, tt := range tests {
			got, err := st.ListAuditEventsAfter(t.Context(), tt.cursor, tt.until, tt.limit)
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if ids := auditIDs(got); !slices.Equal(ids, tt.want) {
				t.Errorf("%s: got %v, want %v", tt.name, ids, tt.want)
			}
		}
	})
}

func TestConformanceAuditCursor(t *testing.T) {
	t.Parallel()

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		ctx := t.Context()
		first := models.AuditCursor{At: time.Date(2026, 9, 1, 12, 0, 0, 123456789, time.UTC), ID: "a b"}
		second := models.AuditCursor{At: first.At.Add(time.Second), ID: "c"}

		if _, ok, err := st.AuditCursor(ctx, "nats"); ok || err != nil {
			t.Fatalf("a new relay has a cursor: %v, %v", ok, err)
		}
		steps := []struct {
			name     string
			from, to models.AuditCursor
			want     bool
		}{
			{"create", models.AuditCursor{}, first, true},
			{"create again loses", models.AuditCursor{}, second, false},
			{"advance from the current one", first, second, true},
			{"advance from a stale one loses", first, second, false},
		}
		for _, s := range steps {
			ok, err := st.AdvanceAuditCursor(ctx, "nats", s.from, s.to)
			if err != nil || ok != s.want {
				t.Errorf("%s = %v, %v; want %v", s.name, ok, err, s.want)
			}
		}
		got, ok, err := st.AuditCursor(ctx, "nats")
		if err != nil || !ok || !got.At.Equal(second.At) || got.ID != second.ID {
			t.Errorf("cursor = %+v, %v, %v; want %+v", got, ok, err, second)
		}
		if _, ok, _ := st.AuditCursor(ctx, "other"); ok {
			t.Error("cursors are not kept apart by name")
		}
	})
}
