package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

const maxPreviewBytes = 1 << 20 // 1 MiB is far beyond any Adaptive Card template

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPreviewBytes)

	var req struct {
		Body   string `json:"body"`
		Sample string `json:"sample"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "template is too large to preview")
			return
		}

		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	card, err := templates.Render(req.Body, templates.RenderData{
		Alert: previewAlert(req.Sample),
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		// A broken template is the answer the preview was asked for, not a server fault.
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"card": card})
}

func previewSamples() []string {
	return []string{"firing", "resolved"}
}

func previewAlert(name string) models.Alert {
	alert := models.Alert{
		Source: "universal",
		Status: "firing",
		Labels: map[string]string{
			"alertname": "HighMemory",
			"severity":  "warning",
			"service":   "worker",
		},
		Annotations: map[string]string{
			"summary":     "Memory usage is above 80%",
			"description": "worker service memory is high",
		},
		StartsAt:    time.Date(2026, 2, 9, 9, 0, 0, 0, time.UTC),
		Generator:   "custom",
		Fingerprint: "2a6d3b7f1c",
	}

	switch name {
	case "resolved":
		alert.Status = "resolved"
		alert.EndsAt = time.Date(2026, 2, 9, 10, 30, 0, 0, time.UTC)
		return alert
	default:
		return alert
	}
}
