package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/i18n"
	"github.com/pflege-de-labs/teamster/internal/logging"
	"github.com/pflege-de-labs/teamster/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

// writeError is the error boundary for JSON handlers. A 4xx carries err's own
// message, which must be written for the caller. A 5xx is logged and carries
// its status and the request id, so a store, Graph or Bot Framework failure
// stays in the log; only a missing bot, which the sender's operator has to fix
// here, is named.
func writeError(w http.ResponseWriter, r *http.Request, status int, err error) {
	logger := logging.FromContext(r.Context())
	if status < http.StatusInternalServerError {
		logger.Debug("client error", "status", status, "err", err)
		writeJSONError(w, status, err.Error())
		return
	}
	logger.Error("request failed", "status", status, "err", err)
	message := strings.ToLower(http.StatusText(status))
	if errors.Is(err, errNoChannelTransport) || errors.Is(err, errNoBotConfigured) {
		message = err.Error()
	}
	writeJSON(w, status, map[string]string{"error": message, "request_id": requestID(r.Context())})
}

// failureText logs err and returns what a page shows in its place: a generic
// sentence and the request id, never the underlying error.
func failureText(ctx context.Context, msg string, err error) string {
	logging.FromContext(ctx).Error(msg, "err", err)
	return i18n.T(ctx, "error.internal", requestID(ctx))
}

// logError keeps a failure that cannot change the response out of the caller's
// happy path without discarding it.
func logError(ctx context.Context, msg string, err error) {
	logging.FromContext(ctx).Error(msg, "err", err)
}

// userError marks an error whose message is written for the person who caused
// it, such as a form value that fails validation.
type userError struct{ err error }

func (e userError) Error() string { return e.err.Error() }
func (e userError) Unwrap() error { return e.err }

// userFacing reports whether err's message may be shown as it is: our own
// validation and the store's sentinels say what to fix and nothing about how
// the server is built.
func userFacing(err error) bool {
	switch {
	case errors.As(err, new(userError)), errors.As(err, new(invalidRoute)):
		return true
	}
	for _, known := range []error{
		store.ErrNotFound, store.ErrDefaultDestination, store.ErrConflict,
		errDeliveryRefused, errRecipientRefused, errTemplateRefused, errAddressedRefused, errUnknownCatchAllTemplate, errBadSourceDefault, errTokenName, errTokenNameTaken, errTokenWebhooks, errInvalidSlug, errUnknownTemplate,
	} {
		if errors.Is(err, known) {
			return true
		}
	}
	return false
}

// visibleError is what a page shows for err: its own message when that is
// written for the reader, otherwise failureText.
func visibleError(ctx context.Context, msg string, err error) string {
	if userFacing(err) {
		logging.FromContext(ctx).Debug(msg, "err", err)
		return err.Error()
	}
	return failureText(ctx, msg, err)
}
