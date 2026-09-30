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
	return []string{models.SourceAlertmanager, "open", "closed", "message", teamsV2Source}
}

// previewTeamsV2Body is what a Teams V2 sender posts, for previewing a
// template meant for an endpoint (ADR 0040).
const previewTeamsV2Body = `{"@type":"MessageCard","themeColor":"FF0000","summary":"Build failed",` +
	`"title":"Build failed","text":"Pipeline **main** failed at step *test*.",` +
	`"sections":[{"facts":[{"name":"Branch","value":"main"},{"name":"Commit","value":"4f2a91c"}]}]}`

// previewData is what a sample renders against: an event, and for a Teams V2
// sample the payload a template reaches through .Payload.
func previewData(name string) templates.RenderData {
	now := time.Now().UTC().Format(time.RFC3339)
	if name != teamsV2Source {
		return templates.RenderData{Event: previewEvent(name), Now: now}
	}
	// A constant, and TestPreviewRendersTheTeamsV2Sample proves it parses.
	msg, _ := teamsv2.Parse([]byte(previewTeamsV2Body))
	var payload any
	_ = json.Unmarshal([]byte(previewTeamsV2Body), &payload)
	ev := models.Event{Source: teamsV2Source, Labels: models.WithSourceLabel(nil, teamsV2Source), Title: msg.Title, Text: msg.Text}
	if len(msg.Cards) > 0 {
		ev.Card = msg.Cards[0]
	}
	return templates.RenderData{Event: ev, Now: now, Payload: payload}
}

func previewEvent(name string) models.Event {
	if name == models.SourceAlertmanager {
		// What processEvent makes of one entry of an Alertmanager group.
		return models.Event{
			Source: models.SourceAlertmanager,
			Key:    "9f9f5f4a1f",
			State:  models.StateOpen,
			Labels: map[string]string{
				"alertname":        "HighCPU",
				"severity":         "critical",
				"service":          "api",
				models.SourceLabel: models.SourceAlertmanager,
			},
			Alertmanager: &models.AlertmanagerEvent{
				Annotations: map[string]string{
					"summary":     "CPU usage is above 90%",
					"description": "api service is spiking CPU",
				},
				StartsAt:     time.Date(2026, 2, 9, 9, 0, 0, 0, time.UTC),
				GeneratorURL: "http://prometheus.example/rule",
				Receiver:     "teamster",
				GroupKey:     `{}:{alertname="HighCPU"}`,
				GroupLabels:  map[string]string{"alertname": "HighCPU"},
				ExternalURL:  "http://alertmanager.example",
			},
		}
	}

	ev := models.Event{
		Source: models.SourceUniversal,
		Key:    "2a6d3b7f1c",
		State:  models.StateOpen,
		Labels: map[string]string{
			"alertname":        "HighMemory",
			"severity":         "warning",
			"service":          "worker",
			models.SourceLabel: models.SourceUniversal,
		},
		Universal: &models.UniversalEvent{
			Attributes: map[string]string{
				"summary":     "Memory usage is above 80%",
				"description": "worker service memory is high",
			},
			Time: time.Date(2026, 2, 9, 9, 0, 0, 0, time.UTC),
			URL:  "https://grafana.example/d/worker",
		},
	}
	switch name {
	case "closed":
		ev.State = models.StateClosed
	case "message":
		// A general message has no lifecycle: no state, no time, no key --
		// exactly the fields a one-shot delivery never uses.
		ev.State = models.StateNone
		ev.Key = ""
		ev.Universal.Time = time.Time{}
	}
	return ev
}
