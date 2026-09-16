package httpserver

import (
	"net/http"
	"sort"

	"github.com/pflege-de-labs/teamster/internal/authz"
	"github.com/pflege-de-labs/teamster/internal/httpserver/views"
	"github.com/pflege-de-labs/teamster/internal/models"
)

// handleRecipientsPage lists every person who has linked a chat, with the
// routes that target them and whether their conversation is known broken. It
// is a page of its own, like /admin/permissions, rather than a panel on
// /admin: an admin about to unlink somebody needs the full picture of what
// stops being delivered, not a row squeezed beside templates and routes.
func (s *Server) handleRecipientsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	_, roles := principalOf(r)
	page := views.Recipients{
		Notice:  r.URL.Query().Get("notice"),
		Error:   r.URL.Query().Get("error"),
		Viewer:  s.viewerFor(r),
		CanEdit: s.authz.Allow(principalSubject(r), roles, authz.ActionEdit, authz.Resource{Type: "Recipient"}),
	}

	recipients, err := s.store.ListRecipients(ctx)
	if err != nil && page.Error == "" {
		page.Error = err.Error()
	}
	routes, err := s.store.ListRoutes(ctx)
	if err != nil && page.Error == "" {
		page.Error = err.Error()
	}

	page.Rows = recipientRows(recipients, routes)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := views.RecipientsPage(page).Render(ctx, w); err != nil {
		logError("render recipients page", err)
	}
}

// recipientRows pairs each recipient with the names of the routes that target
// them, so the page can show what stops being delivered before an admin
// unlinks somebody. A route without a name falls back to its id, the same
// convention routeLabel uses for a delivery outcome.
func recipientRows(recipients []models.Recipient, routes []models.Route) []views.RecipientRow {
	names := map[string][]string{}
	for _, route := range routes {
		if route.RecipientID == "" {
			continue
		}
		label := route.Name
		if label == "" {
			label = route.ID
		}
		names[route.RecipientID] = append(names[route.RecipientID], label)
	}
	for _, list := range names {
		sort.Strings(list)
	}

	rows := make([]views.RecipientRow, 0, len(recipients))
	for _, recipient := range recipients {
		rows = append(rows, views.RecipientRow{Recipient: recipient, RouteNames: names[recipient.ID]})
	}
	return rows
}

// deleteRecipientForm unlinks a person. Deleting is allowed even when routes
// still reference it, matching how destinations already behave: the routing
// graph already draws a route pointing at something deleted as a "missing"
// node, and the page an admin just came from is what made the consequence an
// informed choice rather than a surprise.
func (s *Server) deleteRecipientForm(r *http.Request) (string, error) {
	ctx := r.Context()
	if err := s.store.DeleteRecipient(ctx, r.PostFormValue("id")); err != nil {
		return "", err
	}
	return "Recipient unlinked.", nil
}
