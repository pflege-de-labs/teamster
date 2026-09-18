package httpserver

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/metrics"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/routing"
	"github.com/pflege-de-labs/teamster/internal/store"
	"github.com/pflege-de-labs/teamster/internal/templates"
)

func (s *Server) handleAlertmanager(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		// Counted here because a refused token is otherwise a 401 nobody is
		// watching, and "the sender's secret is wrong" looks exactly like "the
		// sender stopped sending".
		s.metrics.WebhookReceived(ctx, "alertmanager", "refused")
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
		s.metrics.WebhookReceived(ctx, model.Source, model.Status)
		if err := s.processAlert(ctx, model); err != nil {
			writeJSONError(w, http.StatusBadGateway, err.Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleUniversal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.webhookAuth(r) {
		s.metrics.WebhookReceived(ctx, "universal", "refused")
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

	s.metrics.WebhookReceived(ctx, model.Source, model.Status)
	if err := s.processAlert(ctx, model); err != nil {
		writeJSONError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) processAlert(ctx context.Context, alert models.Alert) error {
	if alert.Fingerprint == "" {
		alert.Fingerprint = hashFingerprint(alert)
	}
	if alert.Status != "firing" && alert.Status != "resolved" {
		return fmt.Errorf("unknown status: %s", alert.Status)
	}

	result, err := s.router.Plan(ctx, alert.Labels)
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
		return s.resolveAlert(ctx, alert, result.Deliveries)
	}

	// One channel refusing the message must not cost the others theirs, so every
	// delivery is attempted and the failures are reported together.
	var failures []error
	for _, delivery := range result.Deliveries {
		if err := s.deliver(ctx, alert, delivery); err != nil {
			failures = append(failures, fmt.Errorf("route %s: %w", delivery.RouteName, err))
		}
	}
	return errors.Join(failures...)
}

// errCardInFlight says another instance is inside its Graph call for this very
// card. It is an error rather than a silent success because the payload that
// lost the race may carry something the winner's did not: a 502 asks the sender
// to retry, and the retry updates the card the winner created.
var errCardInFlight = errors.New("another instance is posting this card")

// claimTTL is how long a claim may go uncompleted before another instance may
// take it over. It has to outlast a Graph call comfortably: too short and two
// instances post the same card, which is the thing claiming exists to prevent.
func (s *Server) claimTTL() time.Duration {
	graphTimeout := time.Duration(s.cfg.Graph.TimeoutSec) * time.Second
	if ttl := 3 * graphTimeout; ttl > 30*time.Second {
		return ttl
	}
	return 30 * time.Second
}

// deliver posts or updates the one message this delivery names, in whichever
// transport its kind says. A route may name a channel, a person or both, and
// each target is its own Delivery with its own claim -- so one failing does not
// cost the other its message.
//
// deliverToChannel below is the original path, unchanged in substance.
//
// The card is claimed before the Graph call and the claim completed after it,
// with no transaction spanning the two: a transaction held across a network
// call pins a connection for as long as Microsoft takes to answer, and tells us
// nothing if the process dies holding it. The claim row does — it is the only
// record that a post was in flight, which is what lets the next attempt take
// over rather than wait forever or post a second card.
func (s *Server) deliver(ctx context.Context, alert models.Alert, delivery routing.Delivery) error {
	if delivery.Kind == routing.DeliveryRecipient {
		return s.deliverToRecipient(ctx, alert, delivery)
	}
	return s.deliverToChannel(ctx, alert, delivery)
}

func (s *Server) deliverToChannel(ctx context.Context, alert models.Alert, delivery routing.Delivery) error {
	rendered, err := s.renderMessage(ctx, alert, delivery)
	if err != nil {
		return err
	}
	destination, err := s.channelTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg := channelMessage(rendered)

	now := s.now()
	claim := models.AlertClaim{
		Fingerprint: alert.Fingerprint,
		TeamID:      destination.TeamID,
		ChannelID:   destination.ChannelID,
		Status:      alert.Status,
		Owner:       uuid.NewString(),
		At:          now,
		StaleBefore: now.Add(-s.claimTTL()),
	}

	card, outcome, err := s.store.ClaimActiveAlert(ctx, claim)
	if err != nil {
		return fmt.Errorf("claim active alert: %w", err)
	}

	switch outcome {
	case store.ClaimPosted:
		// The card for this channel already exists, so the alert is an update
		// to it rather than a second card.
		if err := s.graph.UpdateMessage(card.TeamID, card.ChannelID, card.MessageID, msg); err != nil {
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			return fmt.Errorf("graph update: %w", err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		return s.store.TouchActiveAlert(ctx, card, alert.Status, s.now())
	case store.ClaimHeld:
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		return errCardInFlight
	}

	messageID, err := s.graph.PostMessage(destination.TeamID, destination.ChannelID, msg)
	if err != nil {
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		// The claim is ours and nothing was posted under it, so it goes back
		// now rather than making the next attempt wait out the cutoff.
		if release := s.store.ReleaseActiveAlertClaim(ctx, claim); release != nil {
			logError("release alert claim", release)
		}
		return fmt.Errorf("graph post: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)

	if err := s.store.CompleteActiveAlertClaim(ctx, claim, messageID, s.now()); err != nil {
		if errors.Is(err, store.ErrClaimLost) {
			// The window this cannot close: the post succeeded, and by the time
			// it was recorded the row belonged to somebody else's card. Graph
			// has no idempotency key for a channel message, so the card just
			// posted cannot be adopted or withdrawn -- only reported.
			logError("orphaned card", fmt.Errorf("%s/%s message %s: %w",
				destination.TeamID, destination.ChannelID, messageID, err))
		}
		return err
	}
	return nil
}

// resolveAlert walks the messages that went out rather than the plan, because
// the routes may have changed since: a card in a channel the plan no longer
// names still has to stop saying the alert is firing. Both tables are walked
// for that reason -- a chat message is as stranded as a card is.
func (s *Server) resolveAlert(ctx context.Context, alert models.Alert, plan []routing.Delivery) error {
	failures := s.resolveChannelCards(ctx, alert, plan)
	failures = append(failures, s.resolveChatMessages(ctx, alert, plan)...)
	return errors.Join(failures...)
}

func (s *Server) resolveChannelCards(ctx context.Context, alert models.Alert, plan []routing.Delivery) []error {
	active, err := s.store.ListActiveAlerts(ctx, alert.Fingerprint)
	if err != nil {
		return []error{fmt.Errorf("active alert lookup: %w", err)}
	}

	var failures []error
	for _, card := range active {
		// A claim in flight has no card yet, so there is nothing to edit and
		// nothing to forget: deleting it would strand the message the other
		// instance is about to post, still saying the alert fires. Reporting
		// it retryable is the same reasoning as the failed update below --
		// the sender's retry is what puts it right, by which time the card
		// exists and this becomes an ordinary resolve.
		if !card.Posted() {
			failures = append(failures, fmt.Errorf("channel %s: %w", card.ChannelID, errCardInFlight))
			continue
		}

		delivery, err := s.channelDeliveryFor(ctx, card, plan)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		rendered, err := s.renderMessage(ctx, alert, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		msg := channelMessage(rendered)
		// The row outlives a failed update on purpose: it is the only record that
		// this channel still holds a card claiming the alert fires, and the
		// sender's retry is what puts that right.
		if err := s.graph.UpdateMessage(card.TeamID, card.ChannelID, card.MessageID, msg); err != nil {
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			failures = append(failures, fmt.Errorf("graph update: %w", err))
			continue
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		// Deleting by message id, so a resolve that raced a refire forgets the
		// card it just edited rather than the newer one that replaced it.
		if err := s.store.DeleteActiveAlertCard(ctx, card.Fingerprint, card.TeamID, card.ChannelID, card.MessageID); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// resolveChatMessages sends the resolution rather than editing the message the
// firing produced. ADR 0026: an edit in Teams shows an "Edited" marker and does
// not re-notify, so an in-place resolve would be silent -- and the one thing
// the person on call is waiting for is being told it cleared. Sending also
// means this path never needs the stored activity id.
func (s *Server) resolveChatMessages(ctx context.Context, alert models.Alert, plan []routing.Delivery) []error {
	active, err := s.store.ListActiveAlertRecipients(ctx, alert.Fingerprint)
	if err != nil {
		return []error{fmt.Errorf("active alert recipient lookup: %w", err)}
	}
	if len(active) > 0 && s.bot == nil {
		return []error{errNoBotConfigured}
	}

	var failures []error
	for _, card := range active {
		// A claim in flight has nothing sent under it yet; the same reasoning
		// as the channel path above.
		if !card.Posted() {
			failures = append(failures, fmt.Errorf("recipient %s: %w", card.RecipientID, errCardInFlight))
			continue
		}

		delivery, err := recipientDeliveryFor(card.RecipientID, plan)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		recipient, err := s.store.GetRecipient(ctx, card.RecipientID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				// The recipient is gone -- unlinked since this row was
				// claimed or posted, or a row stranded some other way before
				// unlinking cascaded (DeleteRecipient in internal/store). This
				// is not swallowing an error: the resolve itself has not
				// failed, only the row it was about to act on no longer names
				// anybody to notify or anything to keep, and forgetting it is
				// what lets the alert resolve instead of failing against the
				// same 404 on every retry.
				if forget := s.store.DeleteActiveAlertRecipientCard(ctx, card.Fingerprint, card.RecipientID, card.MessageID); forget != nil {
					logError("forget orphaned recipient card", forget)
				}
				continue
			}
			failures = append(failures, fmt.Errorf("recipient %s: %w", card.RecipientID, err))
			continue
		}

		rendered, err := s.renderMessage(ctx, alert, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		msg, err := chatMessage(rendered)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		if _, err := s.bot.SendMessage(ctx, conversationRef(recipient), msg); err != nil {
			if recipientBlocked(err) {
				// Nothing will reach this person until somebody acts, so the
				// row goes rather than being retried into the same error on
				// every later alert. The flag records why for an admin --
				// informational only, per ADR 0026: it never stops the next
				// alert from trying again.
				s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeBlocked)
				s.markRecipientBlocked(ctx, card.RecipientID, err)
				if forget := s.store.DeleteActiveAlertRecipientCard(ctx, card.Fingerprint, card.RecipientID, card.MessageID); forget != nil {
					logError("forget blocked recipient card", forget)
				}
				failures = append(failures, fmt.Errorf("bot send: %w", err))
				continue
			}
			// The row outlives a failed send for the channel path's reason: it
			// is the only record that this person was told the alert fires.
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			failures = append(failures, fmt.Errorf("bot send: %w", err))
			continue
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
		s.clearRecipientBlocked(ctx, card.RecipientID)

		// Deleting by message id, so a resolve that raced a refire forgets the
		// row it just resolved rather than the newer one that replaced it.
		if err := s.store.DeleteActiveAlertRecipientCard(ctx, card.Fingerprint, card.RecipientID, card.MessageID); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// The delivery that owns a card is the one pointing at its channel; a card the
// plan no longer covers is rendered with the first template the plan names,
// which is better than leaving it claiming the alert still fires.
//
// The fallback stays inside the kind. A plan that fans out to a channel and a
// person has deliveries of both, and handing a channel card the recipient one
// would render it against a template chosen for a chat -- picked, at that, by
// nothing better than position in the slice.
func (s *Server) channelDeliveryFor(ctx context.Context, card models.ActiveAlert, plan []routing.Delivery) (routing.Delivery, error) {
	channels := deliveriesOfKind(plan, routing.DeliveryChannel)
	for _, delivery := range channels {
		destination, err := s.store.GetDestination(ctx, delivery.DestinationID)
		if err != nil {
			continue
		}
		if destination.TeamID == card.TeamID && destination.ChannelID == card.ChannelID {
			return delivery, nil
		}
	}
	if len(channels) == 0 {
		return routing.Delivery{}, fmt.Errorf("no route renders the card in channel %s", card.ChannelID)
	}
	return channels[0], nil
}

// recipientDeliveryFor is the same for a chat message, and needs no store
// lookup: a delivery names the recipient the row is keyed by directly.
func recipientDeliveryFor(recipientID string, plan []routing.Delivery) (routing.Delivery, error) {
	recipients := deliveriesOfKind(plan, routing.DeliveryRecipient)
	for _, delivery := range recipients {
		if delivery.RecipientID == recipientID {
			return delivery, nil
		}
	}
	if len(recipients) == 0 {
		return routing.Delivery{}, fmt.Errorf("no route renders the message to recipient %s", recipientID)
	}
	return recipients[0], nil
}

func deliveriesOfKind(plan []routing.Delivery, kind routing.DeliveryKind) []routing.Delivery {
	out := make([]routing.Delivery, 0, len(plan))
	for _, delivery := range plan {
		if delivery.Kind == kind {
			out = append(out, delivery)
		}
	}
	return out
}

// renderMessage renders the template a delivery names. It is separate from
// resolving the target because the two no longer go together: a channel
// delivery resolves a destination, a chat delivery a recipient, and both render
// the same way.
//
// The summary line is the template's to decide; templates.RenderMessage falls
// back to the one this service used to hardcode.
func (s *Server) renderMessage(ctx context.Context, alert models.Alert, delivery routing.Delivery) (templates.Message, error) {
	template, err := s.store.GetTemplate(ctx, delivery.TemplateID)
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageTemplate)
		return templates.Message{}, fmt.Errorf("template: %w", err)
	}

	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Alert: alert,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageRender)
		return templates.Message{}, fmt.Errorf("render: %w", err)
	}
	return rendered, nil
}

func (s *Server) channelTarget(ctx context.Context, delivery routing.Delivery) (models.Destination, error) {
	destination, err := s.store.GetDestination(ctx, delivery.DestinationID)
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageDestination)
		return models.Destination{}, fmt.Errorf("destination: %w", err)
	}
	return destination, nil
}

func (s *Server) recipientTarget(ctx context.Context, delivery routing.Delivery) (models.Recipient, error) {
	recipient, err := s.store.GetRecipient(ctx, delivery.RecipientID)
	if err != nil {
		s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageRecipient)
		return models.Recipient{}, fmt.Errorf("recipient: %w", err)
	}
	return recipient, nil
}

// chatMessage is the rendered template on its way to a chat rather than a
// channel. Text is converted from the sanitized HTML rather than re-rendered
// from the template, so the one sanitizer covers both transports -- see
// templates.ToMarkdown.
// channelMessage is chatMessage's counterpart for a channel. graph.Message
// carries a slice because a Teams V2 payload may bring several cards; a
// template renders exactly one.
func channelMessage(rendered templates.Message) graph.Message {
	msg := graph.Message{Title: rendered.Title, Text: rendered.Text}
	if len(rendered.Card) > 0 {
		msg.Cards = []json.RawMessage{rendered.Card}
	}
	return msg
}

func chatMessage(rendered templates.Message) (bot.Message, error) {
	text, err := templates.ToMarkdown(rendered.Text)
	if err != nil {
		return bot.Message{}, fmt.Errorf("markdown: %w", err)
	}
	return bot.Message{Title: rendered.Title, Text: text, Card: rendered.Card}, nil
}

func conversationRef(recipient models.Recipient) bot.ConversationReference {
	return bot.ConversationReference{
		ServiceURL:     recipient.ServiceURL,
		ConversationID: recipient.ConversationID,
		BotChannelID:   recipient.BotChannelID,
		TenantID:       recipient.TenantID,
		AADObjectID:    recipient.AADObjectID,
	}
}

// errNoBotConfigured is what a route pointing at a person means in a deployment
// that never configured the bot. It is an error rather than a skip: the alert
// was meant to reach somebody and did not.
var errNoBotConfigured = errors.New("this route delivers to a person, but no bot is configured")

// recipientBlocked distinguishes the one failure that will not come right on
// its own. The Bot Connector says so in two places -- MessageWritesBlocked at
// the top level, ConversationBlockedByUser one level in -- and both mean the
// person uninstalled or blocked the bot, so re-attempting within this alert
// only produces the same error again.
//
// Everything else, including a 429 and any 5xx, is transient and fails this
// delivery exactly as a channel failure does.
func recipientBlocked(err error) bool {
	var apiErr *bot.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	return apiErr.Code == "MessageWritesBlocked" || apiErr.InnerCode == "ConversationBlockedByUser"
}

// blockedReason is what MarkRecipientBlocked records. The Bot Connector's own
// code names the failure precisely -- "MessageWritesBlocked" tells an admin
// more than a bare "blocked" would -- so it is read off the same error
// recipientBlocked already classified, preferring the top-level Code and
// falling back to InnerCode for the case recipientBlocked itself treats as
// equivalent.
func blockedReason(err error) string {
	var apiErr *bot.APIError
	if !errors.As(err, &apiErr) {
		return "blocked"
	}
	if apiErr.Code != "" {
		return apiErr.Code
	}
	return apiErr.InnerCode
}

// markRecipientBlocked and clearRecipientBlocked are best effort, exactly like
// the DeleteActiveAlertRecipientCard calls beside them: the flag is
// informational (ADR 0026), so a failure to write it must not fail the
// delivery it describes, nor undo a send or update that already succeeded.
func (s *Server) markRecipientBlocked(ctx context.Context, recipientID string, cause error) {
	if err := s.store.MarkRecipientBlocked(ctx, recipientID, s.now(), blockedReason(cause)); err != nil {
		logError("mark recipient blocked", err)
	}
}

// clearRecipientBlocked runs after every successful send or update, not only
// after one that follows a recovery: the flag must always describe now, and
// clearing a recipient that was never blocked is a no-op in the store, not a
// reason to check first.
func (s *Server) clearRecipientBlocked(ctx context.Context, recipientID string) {
	if err := s.store.ClearRecipientBlocked(ctx, recipientID); err != nil {
		logError("clear recipient blocked", err)
	}
}

// deliverToRecipient is deliverToChannel's counterpart for a person's chat, and
// claims the same way for the same reason -- see deliver above.
//
// Two things differ, both from ADR 0026. A permanent failure drops the claim
// row and is counted under its own outcome, so it stops re-attempting within
// this alert and is visible as something other than noise. And an activity id
// of "" is a documented success, not a claim in flight: the message was
// delivered but cannot be edited later, so the next firing touches the row
// rather than sending a second copy.
func (s *Server) deliverToRecipient(ctx context.Context, alert models.Alert, delivery routing.Delivery) error {
	if s.bot == nil {
		return errNoBotConfigured
	}

	rendered, err := s.renderMessage(ctx, alert, delivery)
	if err != nil {
		return err
	}
	recipient, err := s.recipientTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg, err := chatMessage(rendered)
	if err != nil {
		return err
	}
	ref := conversationRef(recipient)

	now := s.now()
	claim := models.RecipientClaim{
		Fingerprint: alert.Fingerprint,
		RecipientID: recipient.ID,
		Status:      alert.Status,
		Owner:       uuid.NewString(),
		At:          now,
		StaleBefore: now.Add(-s.claimTTL()),
	}

	card, outcome, err := s.store.ClaimActiveAlertRecipient(ctx, claim)
	if err != nil {
		return fmt.Errorf("claim active alert recipient: %w", err)
	}

	switch outcome {
	case store.ClaimPosted:
		// A re-fire edits the message in place. Teams marks it edited and does
		// not re-notify, which is what a repeated firing should do.
		if card.MessageID == "" {
			// Delivered, but the Connector named nothing to edit. Sending again
			// would be a second copy of a message the person already has, so
			// the row is only restamped.
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
			s.clearRecipientBlocked(ctx, card.RecipientID)
			return s.store.TouchActiveAlertRecipient(ctx, card, alert.Status, s.now())
		}
		if err := s.bot.UpdateMessage(ctx, ref, card.MessageID, msg); err != nil {
			if recipientBlocked(err) {
				s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeBlocked)
				s.markRecipientBlocked(ctx, card.RecipientID, err)
				if forget := s.store.DeleteActiveAlertRecipientCard(ctx, card.Fingerprint, card.RecipientID, card.MessageID); forget != nil {
					logError("forget blocked recipient card", forget)
				}
				return fmt.Errorf("bot update: %w", err)
			}
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			return fmt.Errorf("bot update: %w", err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		s.clearRecipientBlocked(ctx, card.RecipientID)
		return s.store.TouchActiveAlertRecipient(ctx, card, alert.Status, s.now())
	case store.ClaimHeld:
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		return errCardInFlight
	}

	activityID, err := s.bot.SendMessage(ctx, ref, msg)
	if err != nil {
		failure := metrics.OutcomeFailed
		if recipientBlocked(err) {
			failure = metrics.OutcomeBlocked
			s.markRecipientBlocked(ctx, recipient.ID, err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), failure)
		// The claim is ours and nothing was sent under it, so it goes back now
		// rather than making the next attempt wait out the cutoff. That is the
		// same row a permanent failure has to drop, so one release covers both.
		if release := s.store.ReleaseActiveAlertRecipientClaim(ctx, claim); release != nil {
			logError("release recipient claim", release)
		}
		return fmt.Errorf("bot send: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
	s.clearRecipientBlocked(ctx, recipient.ID)

	if err := s.store.CompleteActiveAlertRecipientClaim(ctx, claim, activityID, s.now()); err != nil {
		if errors.Is(err, store.ErrClaimLost) {
			// The same window deliverToChannel cannot close: the message was
			// sent, and by the time it was recorded the row was somebody
			// else's. It can only be reported.
			logError("orphaned chat message", fmt.Errorf("recipient %s activity %s: %w",
				recipient.ID, activityID, err))
		}
		return err
	}
	return nil
}

// routeLabel is what a delivery is counted under. The name is what an operator
// recognises, but nothing requires a route to have one, and an empty attribute
// is a row in a dashboard that says nothing.
func routeLabel(delivery routing.Delivery) string {
	if delivery.RouteName != "" {
		return delivery.RouteName
	}
	return delivery.RouteID
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
