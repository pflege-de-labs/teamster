package store_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func auditEvent(id string, at time.Time, actor, action, typ, resourceID string) models.AuditEvent {
	return models.AuditEvent{
		ID:           id,
		OccurredAt:   at,
		Actor:        models.Actor{Subject: actor, Name: actor + " name", Via: models.ViaSession},
		Action:       action,
		ResourceType: typ,
		ResourceID:   resourceID,
		RequestID:    "req-" + id,
	}
}

func auditIDs(events []models.AuditEvent) []string {
	ids := make([]string, 0, len(events))
	for _, e := range events {
		ids = append(ids, e.ID)
	}
	return ids
}

// seedAudit writes five events an hour apart, except b and c which share an
// instant, so the id tie-break is exercised.
func seedAudit(t *testing.T, st store.Store, t0 time.Time) {
	t.Helper()
	events := []models.AuditEvent{
		auditEvent("a", t0, "alice", "template.create", "Template", "t1"),
		auditEvent("b", t0.Add(time.Hour), "bob", "template.update", "Template", "t1"),
		auditEvent("c", t0.Add(time.Hour), "alice", "destination.create", "Destination", "d1"),
		auditEvent("d", t0.Add(2*time.Hour), "alice", "route.delete", "Route", "r1"),
		auditEvent("e", t0.Add(3*time.Hour), "bob", "template.delete", "Template", "t1"),
	}
	events[1].Before = json.RawMessage(`{"name":"old"}`)
	events[1].After = json.RawMessage(`{"name":"new"}`)
	for _, e := range events {
		if err := st.InsertAuditEvent(t.Context(), e); err != nil {
			t.Fatalf("InsertAuditEvent(%s): %v", e.ID, err)
		}
	}
}

func TestConformanceAuditEventsList(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		filter models.AuditFilter
		want   []string
	}{
		{"everything, newest first", models.AuditFilter{}, []string{"e", "d", "c", "b", "a"}},
		{"by actor", models.AuditFilter{Actor: "bob"}, []string{"e", "b"}},
		{"by resource", models.AuditFilter{ResourceType: "Template", ResourceID: "t1"}, []string{"e", "b", "a"}},
		{"by action", models.AuditFilter{Action: "route.delete"}, []string{"d"}},
		{"since is inclusive", models.AuditFilter{Since: t0.Add(2 * time.Hour)}, []string{"e", "d"}},
		{"until is exclusive", models.AuditFilter{Until: t0.Add(time.Hour)}, []string{"a"}},
		{"limit", models.AuditFilter{Limit: 2}, []string{"e", "d"}},
		{"cursor resumes inside a shared instant", models.AuditFilter{CursorID: "c", CursorAt: t0.Add(time.Hour)}, []string{"b", "a"}},
		{"cursor and filter", models.AuditFilter{Actor: "alice", CursorID: "d", CursorAt: t0.Add(2 * time.Hour)}, []string{"c", "a"}},
	}

	eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
		st := open(t)
		seedAudit(t, st, t0)

		for _, tt := range tests {
			got, err := st.ListAuditEvents(t.Context(), tt.filter)
			if err != nil {
				t.Fatalf("%s: ListAuditEvents: %v", tt.name, err)
			}
			if ids := auditIDs(got); !slices.Equal(ids, tt.want) {
				t.Errorf("%s: got %v, want %v", tt.name, ids, tt.want)
			}
		}

		got, err := st.ListAuditEvents(t.Context(), models.AuditFilter{Action: "template.update"})
		if err != nil || len(got) != 1 {
			t.Fatalf("ListAuditEvents(template.update) = %v, %v", got, err)
		}
		event := got[0]
		if event.Actor != (models.Actor{Subject: "bob", Name: "bob name", Via: models.ViaSession}) {
			t.Errorf("actor = %+v", event.Actor)
		}
		if string(event.Before) != `{"name":"old"}` || string(event.After) != `{"name":"new"}` {
			t.Errorf("before/after = %s / %s", event.Before, event.After)
		}
		if !event.OccurredAt.Equal(t0.Add(time.Hour)) || event.RequestID != "req-b" {
			t.Errorf("occurred_at, request_id = %v, %q", event.OccurredAt, event.RequestID)
		}

		created, err := st.ListAuditEvents(t.Context(), models.AuditFilter{Action: "template.create"})
		if err != nil || len(created) != 1 || created[0].Before != nil || created[0].After != nil {
			t.Errorf("an event without snapshots reads back as %+v, %v", created, err)
		}
	})
}

func TestConformanceAuditEventsPrune(t *testing.T) {
	t.Parallel()

	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		cutoff  time.Time
		keep    int
		removed int64
		want    []string
	}{
		{"nothing configured", time.Time{}, 0, 0, []string{"e", "d", "c", "b", "a"}},
		{"by age", t0.Add(time.Hour), 0, 1, []string{"e", "d", "c", "b"}},
		{"by count keeps the newest", time.Time{}, 3, 2, []string{"e", "d", "c"}},
		{"by count larger than the trail", time.Time{}, 10, 0, []string{"e", "d", "c", "b", "a"}},
		{"both, the stricter wins", t0.Add(2 * time.Hour), 4, 3, []string{"e", "d"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			eachBackend(t, func(t *testing.T, open func(t *testing.T) store.Store) {
				st := open(t)
				seedAudit(t, st, t0)

				removed, err := st.PruneAuditEvents(t.Context(), tt.cutoff, tt.keep)
				if err != nil {
					t.Fatalf("PruneAuditEvents: %v", err)
				}
				if removed != tt.removed {
					t.Errorf("removed %d, want %d", removed, tt.removed)
				}
				got, err := st.ListAuditEvents(t.Context(), models.AuditFilter{})
				if err != nil {
					t.Fatalf("ListAuditEvents: %v", err)
				}
				if ids := auditIDs(got); !slices.Equal(ids, tt.want) {
					t.Errorf("left %v, want %v", ids, tt.want)
				}
			})
		})
	}
}
