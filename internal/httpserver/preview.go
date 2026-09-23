package httpserver

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/teamsv2"
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
		Title  string `json:"title"`
		Text   string `json:"text"`
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

	// The whole message, not the card alone: the feed line is the point of
	// having a title at all, so the preview has to show it.
	msg, err := templates.RenderMessage(models.Template{
		Title: req.Title,
		Text:  req.Text,
		Body:  req.Body,
	}, previewData(req.Sample))
	if err != nil {
		// A broken template is the answer the preview was asked for, not a server fault.
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}

	// templates.Message omits the parts a template does not have, so the browser
	// can tell "no card" from "an empty card".
	writeJSON(w, http.StatusOK, msg)
}

func previewSamples() []string {
	return []string{"firing", "resolved", "message", teamsV2Source}
}

// previewTeamsV2Body is what a Teams V2 sender posts, for previewing a
// template meant for an endpoint (ADR 0040).
const previewTeamsV2Body = `{"@type":"MessageCard","themeColor":"FF0000","summary":"Build failed",` +
	`"title":"Build failed","text":"Pipeline **main** failed at step *test*.",` +
	`"sections":[{"facts":[{"name":"Branch","value":"main"},{"name":"Commit","value":"4f2a91c"}]}]}`

// previewData is what a sample renders against: an alert, and for a Teams V2
// sample the payload a template reaches through .Payload.
func previewData(name string) templates.RenderData {
	now := time.Now().UTC().Format(time.RFC3339)
	if name != teamsV2Source {
		return templates.RenderData{Alert: previewAlert(name), Now: now}
	}
	// A constant, and TestPreviewRendersTheTeamsV2Sample proves it parses.
	msg, _ := teamsv2.Parse([]byte(previewTeamsV2Body))
	var payload any
	_ = json.Unmarshal([]byte(previewTeamsV2Body), &payload)
	alert := models.Alert{Source: teamsV2Source, Title: msg.Title, Text: msg.Text}
	if len(msg.Cards) > 0 {
		alert.Card = msg.Cards[0]
	}
	return templates.RenderData{Alert: alert, Now: now, Payload: payload}
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
	case "message":
		// A general message has no lifecycle: no status, no start time, no
		// fingerprint -- exactly the fields a one-shot delivery never uses.
		// Labels and annotations stay, since those are what routing and the
		// template still see regardless.
		alert.Status = ""
		alert.StartsAt = time.Time{}
		alert.Fingerprint = ""
		return alert
	default:
		return alert
	}
}
