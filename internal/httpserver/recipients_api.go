package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// recipientAPI is what /api/recipients exposes: enough to identify a person
// and see whether delivery to them is healthy, not the Bot Framework
// conversation reference (ConversationID, ServiceURL, AADObjectID, TenantID).
// Those four are operational plumbing this API's one consumer -- an admin
// deciding whether to unlink someone -- has no use for; omitting them is one
// fewer thing a leak of this response has to matter about.
type recipientAPI struct {
	ID            string    `json:"id"`
	Subject       string    `json:"subject"`
	Name          string    `json:"name"`
	BlockedAt     time.Time `json:"blocked_at,omitempty"`
	BlockedReason string    `json:"blocked_reason,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func recipientAPIOf(r models.Recipient) recipientAPI {
	return recipientAPI{
		ID:            r.ID,
		Subject:       r.Subject,
		Name:          r.Name,
		BlockedAt:     r.BlockedAt,
		BlockedReason: r.BlockedReason,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

// handleRecipients answers the same list the admin page renders. There is no
// POST here: a recipient is created only by redeeming a link code (see
// recipients_link.go and bot_messages.go), never by an admin typing one in.
func (s *Server) handleRecipients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	items, err := s.store.ListRecipients(ctx)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]recipientAPI, 0, len(items))
	for _, item := range items {
		out = append(out, recipientAPIOf(item))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRecipientByID unlinks a person: the only write this endpoint offers.
// There is no GET or PUT here on purpose -- a recipient's fields all come from
// the conversation reference Teams handed the bot, and nothing an admin edits
// through this API would mean anything Teams did not already say.
func (s *Server) handleRecipientByID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := strings.TrimPrefix(r.URL.Path, "/api/recipients/")
	if id == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if r.Method != http.MethodDelete {
		w.Header().Set("Allow", http.MethodDelete)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if err := s.store.DeleteRecipient(ctx, id); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
