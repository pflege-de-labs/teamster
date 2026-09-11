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
	"github.com/pflege-de-labs/teamster/internal/routing"
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
	if alert.Status != "firing" && alert.Status != "resolved" {
		return fmt.Errorf("unknown status: %s", alert.Status)
	}

	result, err := s.router.Plan(alert.Labels)
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	switch result.Reason {
	case routing.ReasonNoRoutes:
		return errors.New("no routes configured")
	case routing.ReasonNone:
		return errors.New("no matching route and no default route")
	}

	if alert.Status == "resolved" {
		return s.resolveAlert(alert, result.Deliveries)
	}

	// One channel refusing the message must not cost the others theirs, so every
	// delivery is attempted and the failures are reported together.
	var failures []error
	for _, delivery := range result.Deliveries {
		if err := s.deliver(alert, delivery); err != nil {
			failures = append(failures, fmt.Errorf("route %s: %w", delivery.RouteName, err))
		}
	}
	return errors.Join(failures...)
}

func (s *Server) deliver(alert models.Alert, delivery routing.Delivery) error {
	destination, msg, err := s.render(alert, delivery)
	if err != nil {
		return err
	}

	active, err := s.store.GetActiveAlert(alert.Fingerprint, destination.TeamID, destination.ChannelID)
	switch {
	case err == nil:
		// The card for this channel already exists, so the alert is an update to
		// it rather than a second card.
		if err := s.graph.UpdateMessage(active.TeamID, active.ChannelID, active.MessageID, msg); err != nil {
			return fmt.Errorf("graph update: %w", err)
		}
		active.Status = alert.Status
		active.LastUpdate = time.Now().UTC()
		return s.store.UpsertActiveAlert(active)
	case !errors.Is(err, store.ErrNotFound):
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
}

// resolveAlert walks the cards that were posted rather than the plan, because
// the routes may have changed since: a card in a channel the plan no longer
// names still has to stop saying the alert is firing.
func (s *Server) resolveAlert(alert models.Alert, plan []routing.Delivery) error {
	active, err := s.store.ListActiveAlerts(alert.Fingerprint)
	if err != nil {
		return fmt.Errorf("active alert lookup: %w", err)
	}

	var failures []error
	for _, card := range active {
		delivery, err := s.deliveryFor(card, plan)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		_, msg, err := s.render(alert, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		// The row outlives a failed update on purpose: it is the only record that
		// this channel still holds a card claiming the alert fires, and the
		// sender's retry is what puts that right.
		if err := s.graph.UpdateMessage(card.TeamID, card.ChannelID, card.MessageID, msg); err != nil {
			failures = append(failures, fmt.Errorf("graph update: %w", err))
			continue
		}
		if err := s.store.DeleteActiveAlert(card.Fingerprint, card.TeamID, card.ChannelID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// The delivery that owns a card is the one pointing at its channel; a card the
// plan no longer covers is rendered with the first template the plan names,
// which is better than leaving it claiming the alert still fires.
func (s *Server) deliveryFor(card models.ActiveAlert, plan []routing.Delivery) (routing.Delivery, error) {
	for _, delivery := range plan {
		destination, err := s.store.GetDestination(delivery.DestinationID)
		if err != nil {
			continue
		}
		if destination.TeamID == card.TeamID && destination.ChannelID == card.ChannelID {
			return delivery, nil
		}
	}
	if len(plan) == 0 {
		return routing.Delivery{}, fmt.Errorf("no route renders the card in channel %s", card.ChannelID)
	}
	return plan[0], nil
}

func (s *Server) render(alert models.Alert, delivery routing.Delivery) (models.Destination, graph.Message, error) {
	template, err := s.store.GetTemplate(delivery.TemplateID)
	if err != nil {
		return models.Destination{}, graph.Message{}, fmt.Errorf("template: %w", err)
	}
	destination, err := s.store.GetDestination(delivery.DestinationID)
	if err != nil {
		return models.Destination{}, graph.Message{}, fmt.Errorf("destination: %w", err)
	}

	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Alert: alert,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return models.Destination{}, graph.Message{}, fmt.Errorf("render: %w", err)
	}

	// The summary line is the template's to decide now; templates.RenderMessage
	// falls back to the one this service used to hardcode.
	return destination, graph.Message{Title: rendered.Title, Text: rendered.Text, Card: rendered.Card}, nil
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
