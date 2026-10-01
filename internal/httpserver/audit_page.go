package httpserver

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// auditPageSize is one screen of the trail; the API may ask for up to the store's cap.
const auditPageSize = 50

// auditFilter reads the trail's filter and cursor from the query string.
func auditFilter(r *http.Request, limit int) (views.AuditFilter, models.AuditFilter) {
	query := r.URL.Query()
	shown := views.AuditFilter{
		Actor:        query.Get("actor"),
		ResourceType: query.Get("type"),
		ResourceID:   query.Get("id"),
		Action:       query.Get("action"),
	}
	filter := models.AuditFilter{
		Actor:        shown.Actor,
		ResourceType: shown.ResourceType,
		ResourceID:   shown.ResourceID,
		Action:       shown.Action,
		Limit:        limit,
	}
	if at, err := time.Parse(time.RFC3339Nano, query.Get("at")); err == nil && query.Get("cursor") != "" {
		filter.CursorID, filter.CursorAt = query.Get("cursor"), at
	}
	for key, dst := range map[string]*time.Time{"since": &filter.Since, "until": &filter.Until} {
		if at, err := time.Parse(time.RFC3339Nano, query.Get(key)); err == nil {
			*dst = at
		}
	}
	return shown, filter
}

func (s *Server) handleAuditPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	shown, filter := auditFilter(r, auditPageSize)
	page := views.Audit{Viewer: s.viewerFor(r), Filter: shown}

	events, err := s.store.ListAuditEvents(ctx, filter)
	if err != nil {
		page.Error = failureText(ctx, "list audit events", err)
	}
	for _, e := range events {
		page.Events = append(page.Events, views.AuditEntry{Event: e, Changes: auditChanges(e.Before, e.After)})
	}
	if len(events) == filter.Limit {
		page.Older = views.AuditOlderLink(shown, events[len(events)-1])
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.AuditPage(page).Render(ctx, w); err != nil {
		logError(ctx, "render audit page", err)
	}
}

// auditResponse pages like the UI: pass next back as cursor and at.
type auditResponse struct {
	Events []models.AuditEvent `json:"events"`
	Next   *auditCursor        `json:"next,omitempty"`
}

type auditCursor struct {
	Cursor string    `json:"cursor"`
	At     time.Time `json:"at"`
}

func (s *Server) handleAuditAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	limit := auditPageSize
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = n
	}
	_, filter := auditFilter(r, limit)
	events, err := s.store.ListAuditEvents(r.Context(), filter)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, err)
		return
	}
	response := auditResponse{Events: events}
	if response.Events == nil {
		response.Events = []models.AuditEvent{}
	}
	if len(events) > 0 && len(events) == limit {
		last := events[len(events)-1]
		response.Next = &auditCursor{Cursor: last.ID, At: last.OccurredAt}
	}
	writeJSON(w, http.StatusOK, response)
}

// auditChanges lists the top-level fields that differ between two snapshots.
func auditChanges(before, after json.RawMessage) []views.AuditChange {
	if before == nil && after == nil {
		return nil
	}
	var old, updated map[string]json.RawMessage
	if (before != nil && json.Unmarshal(before, &old) != nil) || (after != nil && json.Unmarshal(after, &updated) != nil) {
		// Not objects: show them whole.
		return []views.AuditChange{{Before: string(before), After: string(after)}}
	}

	fields := make([]string, 0, len(old)+len(updated))
	for field := range old {
		fields = append(fields, field)
	}
	for field := range updated {
		if _, ok := old[field]; !ok {
			fields = append(fields, field)
		}
	}
	slices.Sort(fields)

	changes := make([]views.AuditChange, 0, len(fields))
	for _, field := range fields {
		was, is := compactJSON(old[field]), compactJSON(updated[field])
		if was != is {
			changes = append(changes, views.AuditChange{Field: field, Before: was, After: is})
		}
	}
	return changes
}

func compactJSON(raw json.RawMessage) string {
	if raw == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return string(raw)
	}
	return buf.String()
}
