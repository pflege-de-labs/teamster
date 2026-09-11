package httpserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func (s *Server) handleAlertmanager(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload models.AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	for _, alert := range payload.Alerts {
		model := models.Alert{
			Source:      "alertmanager",
			Status:      alert.Status,
			Labels:      alert.Labels,
			Annotations: alert.Annotations,
			StartsAt:    alert.StartsAt,
			EndsAt:      alert.EndsAt,
			Generator:   alert.GeneratorURL,
			Fingerprint: alert.Fingerprint,
		}
		if err := s.processAlert(model); err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUniversal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var payload models.UniversalWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	model := models.Alert{
		Source:      "universal",
		Status:      payload.Status,
		Labels:      payload.Labels,
		Annotations: payload.Annotations,
		StartsAt:    payload.StartsAt,
		EndsAt:      payload.EndsAt,
		Generator:   payload.Generator,
		Fingerprint: payload.Fingerprint,
	}

	if err := s.processAlert(model); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) processAlert(alert models.Alert) error {
	if alert.Fingerprint == "" {
		alert.Fingerprint = hashFingerprint(alert)
	}

	route, err := s.router.SelectRoute(alert.Labels)
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	template, err := s.store.GetTemplate(route.TemplateID)
	if err != nil {
		return fmt.Errorf("template: %w", err)
	}
	destination, err := s.store.GetDestination(route.DestinationID)
	if err != nil {
		return fmt.Errorf("destination: %w", err)
	}

	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Alert: alert,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("render: %w", err)
	}

	// The summary line is the template's to decide now; templates.RenderMessage
	// falls back to the one this service used to hardcode.
	msg := graph.Message{Title: rendered.Title, Text: rendered.Text, Card: rendered.Card}

	switch alert.Status {
	case "firing":
		active, err := s.store.GetActiveAlert(alert.Fingerprint)
		if err == nil {
			if err := s.graph.UpdateMessage(active.TeamID, active.ChannelID, active.MessageID, msg); err != nil {
				return fmt.Errorf("graph update: %w", err)
			}
			active.Status = alert.Status
			active.LastUpdate = time.Now().UTC()
			return s.store.UpsertActiveAlert(active)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("active alert lookup: %w", err)
		}

		messageID, err := s.graph.PostMessage(destination.TeamID, destination.ChannelID, msg)
		if err != nil {
			return fmt.Errorf("graph post: %w", err)
		}

		return s.store.UpsertActiveAlert(models.ActiveAlert{
			Fingerprint: alert.Fingerprint,
			Status:      alert.Status,
			TeamID:      destination.TeamID,
			ChannelID:   destination.ChannelID,
			MessageID:   messageID,
			LastUpdate:  time.Now().UTC(),
		})
	case "resolved":
		active, err := s.store.GetActiveAlert(alert.Fingerprint)
		if err == nil {
			if err := s.graph.UpdateMessage(active.TeamID, active.ChannelID, active.MessageID, msg); err != nil {
				return fmt.Errorf("graph update: %w", err)
			}
			return s.store.DeleteActiveAlert(alert.Fingerprint)
		}
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("active alert lookup: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown status: %s", alert.Status)
	}
}

func hashFingerprint(alert models.Alert) string {
	h := sha256.New()
	_, _ = h.Write([]byte(alert.Source))
	_, _ = h.Write([]byte(alert.Generator))
	_, _ = h.Write([]byte(alert.StartsAt.Format(time.RFC3339)))
	keys := make([]string, 0, len(alert.Labels))
	for key := range alert.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := alert.Labels[key]
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte("="))
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}
