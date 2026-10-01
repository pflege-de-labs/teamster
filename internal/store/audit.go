package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store/sqlitedb"
)

// auditPageMax bounds one page, so a filter that forgets its limit cannot
// read the whole trail into memory.
const auditPageMax = 500

// openEnd stands in for an open Until: later than any event, and still a
// timestamp both dialects compare without special-casing NULL.
var openEnd = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

func (s queryAdapter) InsertAuditEvent(ctx context.Context, e models.AuditEvent) error {
	err := s.q.InsertAuditEvent(ctx, sqlitedb.InsertAuditEventParams{
		ID:           newID(e.ID),
		OccurredAt:   e.OccurredAt.UTC(),
		ActorSubject: e.Actor.Subject,
		ActorName:    e.Actor.Name,
		ActorVia:     e.Actor.Via,
		ActorTokenID: e.Actor.TokenID,
		Action:       e.Action,
		ResourceType: e.ResourceType,
		ResourceID:   e.ResourceID,
		RequestID:    e.RequestID,
		Before:       nullJSON(e.Before),
		After:        nullJSON(e.After),
	})
	if err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}
	return nil
}

func (s queryAdapter) ListAuditEvents(ctx context.Context, filter models.AuditFilter) ([]models.AuditEvent, error) {
	limit := filter.Limit
	if limit <= 0 || limit > auditPageMax {
		limit = auditPageMax
	}
	until := filter.Until
	if until.IsZero() {
		until = openEnd
	}
	rows, err := s.q.ListAuditEvents(ctx, sqlitedb.ListAuditEventsParams{
		Actor:        filter.Actor,
		ResourceType: filter.ResourceType,
		ResourceID:   filter.ResourceID,
		Action:       filter.Action,
		Since:        filter.Since.UTC(),
		Until:        until.UTC(),
		CursorID:     filter.CursorID,
		CursorAt:     filter.CursorAt.UTC(),
		MaxRows:      int64(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list audit events: %w", err)
	}
	out := make([]models.AuditEvent, 0, len(rows))
	for _, row := range rows {
		out = append(out, models.AuditEvent{
			ID:         row.ID,
			OccurredAt: row.OccurredAt.UTC(),
			Actor: models.Actor{
				Subject: row.ActorSubject,
				Name:    row.ActorName,
				Via:     row.ActorVia,
				TokenID: row.ActorTokenID,
			},
			Action:       row.Action,
			ResourceType: row.ResourceType,
			ResourceID:   row.ResourceID,
			RequestID:    row.RequestID,
			Before:       rawJSON(row.Before),
			After:        rawJSON(row.After),
		})
	}
	return out, nil
}

func (s queryAdapter) PruneAuditEvents(ctx context.Context, cutoff time.Time, keep int) (int64, error) {
	var total int64
	if !cutoff.IsZero() {
		n, err := s.q.DeleteAuditEventsBefore(ctx, cutoff.UTC())
		if err != nil {
			return 0, fmt.Errorf("prune expired audit events: %w", err)
		}
		total += n
	}
	if keep > 0 {
		n, err := s.q.DeleteAuditEventsBeyond(ctx, int64(keep))
		if err != nil {
			return total, fmt.Errorf("prune excess audit events: %w", err)
		}
		total += n
	}
	return total, nil
}

func nullJSON(raw []byte) sql.NullString {
	return sql.NullString{String: string(raw), Valid: len(raw) > 0}
}

func rawJSON(value sql.NullString) []byte {
	if !value.Valid || value.String == "" {
		return nil
	}
	return []byte(value.String)
}
