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
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/graph"
	"github.com/pflege-de-labs/teamster/internal/logging"
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
	if !s.authorizeWebhook(w, r, models.SourceAlertmanager) {
		return
	}

	var payload models.AlertmanagerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	for _, alert := range payload.Alerts {
		ev := models.Event{
			Source: models.SourceAlertmanager,
			Key:    alert.Fingerprint,
			State:  alertmanagerState(alert.Status),
			Labels: alert.Labels,
			Alertmanager: &models.AlertmanagerEvent{
				Annotations:       alert.Annotations,
				StartsAt:          alert.StartsAt,
				EndsAt:            alert.EndsAt,
				GeneratorURL:      alert.GeneratorURL,
				Receiver:          payload.Receiver,
				GroupKey:          payload.GroupKey,
				GroupLabels:       payload.GroupLabels,
				CommonLabels:      payload.CommonLabels,
				CommonAnnotations: payload.CommonAnnotations,
				ExternalURL:       payload.ExternalURL,
			},
		}
		s.metrics.WebhookReceived(ctx, ev.Source, string(ev.State))
		if err := s.processEvent(ctx, ev); err != nil {
			writeError(w, r, http.StatusBadGateway, err)
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
	if !s.authorizeWebhook(w, r, models.SourceUniversal) {
		return
	}

	var payload models.UniversalWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	// Refused rather than delivered once, so a sender still speaking the old
	// firing/resolved vocabulary finds out instead of losing its updates.
	if !payload.State.Valid() {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown state %q; want open, closed or none", payload.State))
		return
	}

	ev := models.Event{
		Source: models.SourceUniversal,
		Key:    payload.Key,
		State:  payload.State,
		Labels: payload.Labels,
		Title:  payload.Title,
		Text:   payload.Text,
		Card:   payload.Card,
		Universal: &models.UniversalEvent{
			Attributes: payload.Attributes,
			Time:       payload.Time,
			URL:        payload.URL,
		},
	}

	s.metrics.WebhookReceived(ctx, ev.Source, string(ev.State))
	if err := s.processEvent(ctx, ev); err != nil {
		writeError(w, r, http.StatusBadGateway, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// alertmanagerState maps Alertmanager's vocabulary onto the event lifecycle.
// Anything else it might send is delivered once rather than refused: the
// payload is Alertmanager's, not something its sender can correct.
func alertmanagerState(status string) models.EventState {
	switch status {
	case "firing":
		return models.StateOpen
	case "resolved":
		return models.StateClosed
	}
	return models.StateNone
}

// processEvent routes and delivers one event. State decides the lifecycle, not
// whether the event is accepted: open and closed opt into the claim protocol
// below, because that is what lets a repeat edit a card instead of posting a
// second one and a close clear it. No state -- the normal case for a sender
// with no lifecycle -- is delivered once and tracked nowhere (ADR 0035).
func (s *Server) processEvent(ctx context.Context, ev models.Event) error {
	if ev.Key == "" {
		ev.Key = deriveKey(ev)
	}
	// After the key, so a card posted before the label existed is still the
	// one a later update or close finds.
	ev.Labels = models.WithSourceLabel(ev.Labels, ev.Source)
	// Before routing, so an event no route matches yet still teaches the
	// editor the labels a route for it would select on.
	if s.samples != nil {
		s.samples.Observe(ev.Labels, models.AttributesOf(ev))
	}

	result, err := s.router.Plan(ctx, ev.Labels)
	if err != nil {
		return fmt.Errorf("route: %w", err)
	}
	switch result.Reason {
	case routing.ReasonNoRoutes:
		return errors.New("no routes configured")
	case routing.ReasonNone:
		return errors.New("no matching route and no default route")
	}

	switch ev.State {
	case models.StateClosed:
		return s.closeEvent(ctx, ev, result.Deliveries)
	case models.StateOpen:
		return s.deliverAll(ctx, ev, result.Deliveries, s.deliver)
	default:
		return s.deliverAll(ctx, ev, result.Deliveries, s.deliverOnce)
	}
}

// deliverAll attempts every delivery and reports failures together, so one
// channel refusing the message does not cost the others theirs.
func (s *Server) deliverAll(ctx context.Context, ev models.Event, deliveries []routing.Delivery,
	deliverFn func(context.Context, models.Event, routing.Delivery) error,
) error {
	var failures []error
	for _, delivery := range deliveries {
		if err := deliverFn(ctx, ev, delivery); err != nil {
			failures = append(failures, fmt.Errorf("route %s: %w", delivery.RouteName, err))
		}
	}
	return errors.Join(failures...)
}

// errCardInFlight says another instance is inside its Bot Connector call for this very
// card. It is an error rather than a silent success because the payload that
// lost the race may carry something the winner's did not: a 502 asks the sender
// to retry, and the retry updates the card the winner created.
var errCardInFlight = errors.New("another instance is posting this card")

// claimTTL is how long a claim may go uncompleted before another instance may
// take it over. It has to outlast a Bot Connector call comfortably: too short
// and two instances post the same card, which is the thing claiming exists to
// prevent.
func (s *Server) claimTTL() time.Duration {
	botTimeout := time.Duration(s.cfg.Bot.TimeoutSec) * time.Second
	if ttl := 3 * botTimeout; ttl > 30*time.Second {
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
func (s *Server) deliver(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	if delivery.Kind == routing.DeliveryRecipient {
		return s.deliverToRecipient(ctx, ev, delivery)
	}
	return s.deliverToChannel(ctx, ev, delivery)
}

// deliverOnce is deliver's counterpart for a message with no tracked
// lifecycle: nothing identifies a later post as the same event, so there is
// nothing to claim and nothing to update -- it is rendered and sent exactly
// once, the same fire-and-forget contract /teamsv2/... already has, just
// routed and templated first.
func (s *Server) deliverOnce(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	if delivery.Kind == routing.DeliveryRecipient {
		return s.deliverToRecipientOnce(ctx, ev, delivery)
	}
	return s.deliverToChannelOnce(ctx, ev, delivery)
}

func (s *Server) deliverToChannel(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	rendered, err := s.renderMessage(ctx, ev, delivery)
	if err != nil {
		return err
	}
	destination, err := s.channelTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg := s.channelMessage(rendered)

	now := s.now()
	claim := models.EventClaim{
		Key:         ev.Key,
		TeamID:      destination.TeamID,
		ChannelID:   destination.ChannelID,
		State:       ev.State,
		Owner:       uuid.NewString(),
		At:          now,
		StaleBefore: now.Add(-s.claimTTL()),
	}

	card, outcome, err := s.store.ClaimActiveEvent(ctx, claim)
	if err != nil {
		return fmt.Errorf("claim active event: %w", err)
	}

	switch outcome {
	case store.ClaimPosted:
		if card.ConversationID == "" {
			return s.replaceUneditableCard(ctx, ev, delivery, card)
		}
		// The card for this channel already exists, so the event is an update
		// to it rather than a second card.
		if err := s.channels.UpdateInChannel(ctx, card.TeamID, card.ChannelID, cardOfRow(card), msg); err != nil {
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), channelFailure(err))
			return fmt.Errorf("channel update: %w", err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		return s.store.TouchActiveEvent(ctx, card, ev.State, s.now())
	case store.ClaimHeld:
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		return errCardInFlight
	}

	posted, err := s.channels.PostToChannel(ctx, destination.TeamID, destination.ChannelID, msg)
	if err != nil {
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), channelFailure(err))
		// The claim is ours and nothing was posted under it, so it goes back
		// now rather than making the next attempt wait out the cutoff.
		if release := s.store.ReleaseActiveEventClaim(ctx, claim); release != nil {
			logError(ctx, "release event claim", release)
		}
		return fmt.Errorf("channel post: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)

	if err := s.store.CompleteActiveEventClaim(ctx, claim, posted.MessageID, posted.ConversationID, s.now()); err != nil {
		if errors.Is(err, store.ErrClaimLost) {
			// The window this cannot close: the post succeeded, and by the time
			// it was recorded the row belonged to somebody else's card. The Bot
			// Connector has no idempotency key for a channel post, so the card
			// just posted cannot be adopted or withdrawn -- only reported.
			logError(ctx, "orphaned card", fmt.Errorf("%s/%s message %s: %w",
				destination.TeamID, destination.ChannelID, posted.MessageID, err))
		}
		return err
	}
	return nil
}

// replaceUneditableCard forgets a card nothing can edit -- one the previous
// release posted through Graph -- and posts its successor (ADR 0045).
func (s *Server) replaceUneditableCard(ctx context.Context, ev models.Event, delivery routing.Delivery, card models.ActiveEvent) error {
	if err := s.store.DeleteActiveEventCard(ctx, card.Key, card.TeamID, card.ChannelID, card.MessageID); err != nil {
		return fmt.Errorf("forget uneditable card: %w", err)
	}
	return s.deliverToChannel(ctx, ev, delivery)
}

func cardOfRow(card models.ActiveEvent) channelCard {
	return channelCard{ConversationID: card.ConversationID, MessageID: card.MessageID}
}

// deliverToChannelOnce is deliverToChannel without the claim: render, resolve
// the destination, post. Nothing is written to active_events, so a second
// message with the same key posts a second card rather than editing
// this one -- the point of the untracked path, not an oversight of it.
func (s *Server) deliverToChannelOnce(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	rendered, err := s.renderMessage(ctx, ev, delivery)
	if err != nil {
		return err
	}
	destination, err := s.channelTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg := s.channelMessage(rendered)

	if _, err := s.channels.PostToChannel(ctx, destination.TeamID, destination.ChannelID, msg); err != nil {
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), channelFailure(err))
		return fmt.Errorf("channel post: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
	return nil
}

// closeEvent walks the messages that went out rather than the plan, because
// the routes may have changed since: a card in a channel the plan no longer
// names still has to stop saying the event is open. Both tables are walked
// for that reason -- a chat message is as stranded as a card is.
func (s *Server) closeEvent(ctx context.Context, ev models.Event, plan []routing.Delivery) error {
	failures := s.closeChannelCards(ctx, ev, plan)
	failures = append(failures, s.closeChatMessages(ctx, ev, plan)...)
	return errors.Join(failures...)
}

func (s *Server) closeChannelCards(ctx context.Context, ev models.Event, plan []routing.Delivery) []error {
	active, err := s.store.ListActiveEvents(ctx, ev.Key)
	if err != nil {
		return []error{fmt.Errorf("active event lookup: %w", err)}
	}

	var failures []error
	for _, card := range active {
		// A claim in flight has no card yet, so there is nothing to edit and
		// nothing to forget: deleting it would strand the message the other
		// instance is about to post, still saying the event is open. Reporting
		// it retryable is the same reasoning as the failed update below --
		// the sender's retry is what puts it right, by which time the card
		// exists and this becomes an ordinary close.
		if !card.Posted() {
			failures = append(failures, fmt.Errorf("channel %s: %w", card.ChannelID, errCardInFlight))
			continue
		}

		delivery, err := s.channelDeliveryFor(ctx, card, plan)
		if err != nil {
			failures = append(failures, err)
			continue
		}

		rendered, err := s.renderMessage(ctx, ev, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		msg := s.channelMessage(rendered)
		// The row outlives a failed update on purpose: it is the only record that
		// this channel still holds a card claiming the event is open, and the
		// sender's retry is what puts that right. A card nothing can edit is
		// only forgotten.
		if card.ConversationID != "" {
			if err := s.channels.UpdateInChannel(ctx, card.TeamID, card.ChannelID, cardOfRow(card), msg); err != nil {
				s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), channelFailure(err))
				failures = append(failures, fmt.Errorf("channel update: %w", err))
				continue
			}
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		}
		// Deleting by message id, so a close that raced a reopen forgets the
		// card it just edited rather than the newer one that replaced it.
		if err := s.store.DeleteActiveEventCard(ctx, card.Key, card.TeamID, card.ChannelID, card.MessageID); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// closeChatMessages sends the resolution rather than editing the message the
// open event produced. ADR 0026: an edit in Teams shows an "Edited" marker and does
// not re-notify, so an in-place close would be silent -- and the one thing
// the person on call is waiting for is being told it cleared. Sending also
// means this path never needs the stored activity id.
func (s *Server) closeChatMessages(ctx context.Context, ev models.Event, plan []routing.Delivery) []error {
	active, err := s.store.ListActiveEventRecipients(ctx, ev.Key)
	if err != nil {
		return []error{fmt.Errorf("active event recipient lookup: %w", err)}
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
				// is not swallowing an error: the close itself has not
				// failed, only the row it was about to act on no longer names
				// anybody to notify or anything to keep, and forgetting it is
				// what lets the event close instead of failing against the
				// same 404 on every retry.
				if forget := s.store.DeleteActiveEventRecipientCard(ctx, card.Key, card.RecipientID, card.MessageID); forget != nil {
					logError(ctx, "forget orphaned recipient card", forget)
				}
				continue
			}
			failures = append(failures, fmt.Errorf("recipient %s: %w", card.RecipientID, err))
			continue
		}

		rendered, err := s.renderMessage(ctx, ev, delivery)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		msg, err := s.chatMessage(rendered)
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
				if forget := s.store.DeleteActiveEventRecipientCard(ctx, card.Key, card.RecipientID, card.MessageID); forget != nil {
					logError(ctx, "forget blocked recipient card", forget)
				}
				failures = append(failures, fmt.Errorf("bot send: %w", err))
				continue
			}
			// The row outlives a failed send for the channel path's reason: it
			// is the only record that this person was told the event is open.
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			failures = append(failures, fmt.Errorf("bot send: %w", err))
			continue
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
		s.clearRecipientBlocked(ctx, card.RecipientID)

		// Deleting by message id, so a close that raced a reopen forgets the
		// row it just closed rather than the newer one that replaced it.
		if err := s.store.DeleteActiveEventRecipientCard(ctx, card.Key, card.RecipientID, card.MessageID); err != nil {
			failures = append(failures, err)
		}
	}
	return failures
}

// The delivery that owns a card is the one pointing at its channel; a card the
// plan no longer covers is rendered with the first template the plan names,
// which is better than leaving it claiming the event is still open.
//
// The fallback stays inside the kind. A plan that fans out to a channel and a
// person has deliveries of both, and handing a channel card the recipient one
// would render it against a template chosen for a chat -- picked, at that, by
// nothing better than position in the slice.
func (s *Server) channelDeliveryFor(ctx context.Context, card models.ActiveEvent, plan []routing.Delivery) (routing.Delivery, error) {
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
// renderMessage produces the message one delivery sends. A route's own
// Template wins whenever it has one, exactly as before; a route with none
// falls back to whatever the payload itself supplied directly (ADR 0036) --
// the fallback for a template-less route, never an override of a route that
// has one, so a client accidentally sending a blank Title cannot silently
// blank out a working template. A payload with nothing direct to send gets
// the built-in default, and every untemplated message carries the hint card
// (ADR 0039). Between the two sits the source's default template (ADR 0055).
func (s *Server) renderMessage(ctx context.Context, ev models.Event, delivery routing.Delivery) (templates.Message, error) {
	template, found, err := s.deliveryTemplate(ctx, ev, delivery)
	if err != nil {
		return templates.Message{}, err
	}
	if !found {
		msg, err := untemplatedMessage(ev)
		// A route whose template names other sources has one; saying there is
		// none would send somebody to create a second.
		if err == nil && delivery.TemplateID == "" {
			msg.Notice, err = templates.HintCard(s.cfg.Server.ExternalURL, templatesPanelPath)
		}
		if err != nil {
			s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageRender)
			return templates.Message{}, err
		}
		return msg, nil
	}

	rendered, err := templates.RenderMessage(template, templates.RenderData{
		Event: ev,
		Now:   time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		s.metrics.RenderFailed(ctx, template.ID, metrics.StageRender)
		return templates.Message{}, fmt.Errorf("render: %w", err)
	}
	return rendered, nil
}

// deliveryTemplate is the template a delivery renders with: the route's own
// when it handles the event's source, otherwise that source's default (ADR
// 0055). found is false when neither exists.
func (s *Server) deliveryTemplate(ctx context.Context, ev models.Event, delivery routing.Delivery) (models.Template, bool, error) {
	if delivery.TemplateID != "" {
		template, err := s.store.GetTemplate(ctx, delivery.TemplateID)
		if err != nil {
			s.metrics.RenderFailed(ctx, delivery.TemplateID, metrics.StageTemplate)
			return models.Template{}, false, fmt.Errorf("template: %w", err)
		}
		if template.Handles(ev.Source) {
			return template, true, nil
		}
		// A template written for another webhook's payload would render it
		// wrong or not at all (ADR 0053).
		logging.FromContext(ctx).Warn("template does not handle this source; using the source's default",
			"template", template.Name, "source", ev.Source, "route", delivery.RouteName)
	}
	return s.sourceDefaultTemplate(ctx, ev.Source)
}

// sourceDefaultTemplate is the template chosen for every message from source
// that nothing more specific renders. A default that has since stopped
// handling its source is passed over rather than trusted.
func (s *Server) sourceDefaultTemplate(ctx context.Context, source string) (models.Template, bool, error) {
	id, err := s.store.GetSourceDefaultTemplate(ctx, source)
	if err != nil {
		s.metrics.RenderFailed(ctx, "", metrics.StageTemplate)
		return models.Template{}, false, fmt.Errorf("source default template: %w", err)
	}
	if id == "" {
		return models.Template{}, false, nil
	}
	template, err := s.store.GetTemplate(ctx, id)
	if err != nil {
		s.metrics.RenderFailed(ctx, id, metrics.StageTemplate)
		return models.Template{}, false, fmt.Errorf("source default template: %w", err)
	}
	if !template.Handles(source) {
		logging.FromContext(ctx).Warn("source default template no longer handles its source", "template", template.Name, "source", source)
		return models.Template{}, false, nil
	}
	return template, true, nil
}

// templatesPanelPath is where the hint card sends someone to create a template.
const templatesPanelPath = "/admin#templates"

// untemplatedMessage is the payload's own content when it has any, and the
// built-in default when it has none.
func untemplatedMessage(ev models.Event) (templates.Message, error) {
	if ev.Title == "" && ev.Text == "" && len(ev.Card) == 0 {
		return templates.Default(ev)
	}
	return directMessage(ev)
}

// directMessage builds a Message straight from what the sender supplied,
// skipping template rendering entirely. Text still goes through
// templates.RenderText -- the same Markdown-to-sanitized-HTML pipeline a
// stored template's Text goes through -- because that sanitizer is a trust
// boundary regardless of who authored the text, not something a sender gets
// to skip by not using a template. The card is passed through as given, the
// same way a template's rendered card is: valid JSON is required, the
// contents are not otherwise validated.
func directMessage(ev models.Event) (templates.Message, error) {
	msg := templates.Message{Title: ev.Title}
	if ev.Text != "" {
		safe, err := templates.RenderText(ev.Text)
		if err != nil {
			return templates.Message{}, fmt.Errorf("text: %w", err)
		}
		msg.Text = safe
	}
	if len(ev.Card) > 0 {
		var probe any
		if err := json.Unmarshal(ev.Card, &probe); err != nil {
			return templates.Message{}, fmt.Errorf("card: not valid JSON: %w", err)
		}
		msg.Card = ev.Card
	}
	return msg, nil
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
// The notice is a card only beside a card of the message's own; next to plain
// text it is a line of text, because a channel post is one Teams message and
// the text would otherwise have to move into a card that renders it worse.
func (s *Server) channelMessage(rendered templates.Message) graph.Message {
	msg := graph.Message{Title: rendered.Title, Text: rendered.Text}
	if len(rendered.Card) > 0 {
		msg.Cards = []json.RawMessage{rendered.Card}
	}
	if len(rendered.Notice) == 0 {
		return msg
	}
	if len(msg.Cards) == 0 && msg.Text != "" {
		if hint, err := templates.RenderText(templates.HintText(s.cfg.Server.ExternalURL, templatesPanelPath)); err == nil {
			msg.Text += hint
			return msg
		}
	}
	msg.Cards = append(msg.Cards, rendered.Notice)
	return msg
}

// A chat message carries one card, so the notice takes the card's place only
// when there is none, and is otherwise said in a line of text.
func (s *Server) chatMessage(rendered templates.Message) (bot.Message, error) {
	text, err := templates.ToMarkdown(rendered.Text)
	if err != nil {
		return bot.Message{}, fmt.Errorf("markdown: %w", err)
	}
	msg := bot.Message{Title: rendered.Title, Text: text, Card: rendered.Card, Summary: templates.Summary(rendered.Title, rendered.Text)}
	if len(rendered.Notice) > 0 {
		if len(msg.Card) == 0 {
			msg.Card = rendered.Notice
		} else {
			msg.Text = strings.TrimSpace(msg.Text + "\n\n" + templates.HintText(s.cfg.Server.ExternalURL, templatesPanelPath))
		}
	}
	return msg, nil
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
// the DeleteActiveEventRecipientCard calls beside them: the flag is
// informational (ADR 0026), so a failure to write it must not fail the
// delivery it describes, nor undo a send or update that already succeeded.
func (s *Server) markRecipientBlocked(ctx context.Context, recipientID string, cause error) {
	if err := s.store.MarkRecipientBlocked(ctx, recipientID, s.now(), blockedReason(cause)); err != nil {
		logError(ctx, "mark recipient blocked", err)
	}
}

// clearRecipientBlocked runs after every successful send or update, not only
// after one that follows a recovery: the flag must always describe now, and
// clearing a recipient that was never blocked is a no-op in the store, not a
// reason to check first.
func (s *Server) clearRecipientBlocked(ctx context.Context, recipientID string) {
	if err := s.store.ClearRecipientBlocked(ctx, recipientID); err != nil {
		logError(ctx, "clear recipient blocked", err)
	}
}

// deliverToRecipient is deliverToChannel's counterpart for a person's chat, and
// claims the same way for the same reason -- see deliver above.
//
// Two things differ, both from ADR 0026. A permanent failure drops the claim
// row and is counted under its own outcome, so it stops re-attempting within
// this alert and is visible as something other than noise. And an activity id
// of "" is a documented success, not a claim in flight: the message was
// delivered but cannot be edited later, so the next update touches the row
// rather than sending a second copy.
func (s *Server) deliverToRecipient(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	if s.bot == nil {
		return errNoBotConfigured
	}

	rendered, err := s.renderMessage(ctx, ev, delivery)
	if err != nil {
		return err
	}
	recipient, err := s.recipientTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg, err := s.chatMessage(rendered)
	if err != nil {
		return err
	}
	ref := conversationRef(recipient)

	now := s.now()
	claim := models.RecipientClaim{
		Key:         ev.Key,
		RecipientID: recipient.ID,
		State:       ev.State,
		Owner:       uuid.NewString(),
		At:          now,
		StaleBefore: now.Add(-s.claimTTL()),
	}

	card, outcome, err := s.store.ClaimActiveEventRecipient(ctx, claim)
	if err != nil {
		return fmt.Errorf("claim active event recipient: %w", err)
	}

	switch outcome {
	case store.ClaimPosted:
		// A re-fire edits the message in place. Teams marks it edited and does
		// not re-notify, which is what a repeated open event should do.
		if card.MessageID == "" {
			// Delivered, but the Connector named nothing to edit. Sending again
			// would be a second copy of a message the person already has, so
			// the row is only restamped.
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
			s.clearRecipientBlocked(ctx, card.RecipientID)
			return s.store.TouchActiveEventRecipient(ctx, card, ev.State, s.now())
		}
		if err := s.bot.UpdateMessage(ctx, ref, card.MessageID, msg); err != nil {
			if recipientBlocked(err) {
				s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeBlocked)
				s.markRecipientBlocked(ctx, card.RecipientID, err)
				if forget := s.store.DeleteActiveEventRecipientCard(ctx, card.Key, card.RecipientID, card.MessageID); forget != nil {
					logError(ctx, "forget blocked recipient card", forget)
				}
				return fmt.Errorf("bot update: %w", err)
			}
			s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
			return fmt.Errorf("bot update: %w", err)
		}
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeUpdated)
		s.clearRecipientBlocked(ctx, card.RecipientID)
		return s.store.TouchActiveEventRecipient(ctx, card, ev.State, s.now())
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
		if release := s.store.ReleaseActiveEventRecipientClaim(ctx, claim); release != nil {
			logError(ctx, "release recipient claim", release)
		}
		return fmt.Errorf("bot send: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
	s.clearRecipientBlocked(ctx, recipient.ID)

	if err := s.store.CompleteActiveEventRecipientClaim(ctx, claim, activityID, s.now()); err != nil {
		if errors.Is(err, store.ErrClaimLost) {
			// The same window deliverToChannel cannot close: the message was
			// sent, and by the time it was recorded the row was somebody
			// else's. It can only be reported.
			logError(ctx, "orphaned chat message", fmt.Errorf("recipient %s activity %s: %w",
				recipient.ID, activityID, err))
		}
		return err
	}
	return nil
}

// deliverToRecipientOnce is deliverToRecipient without the claim: no
// RecipientClaim, no ActiveEventRecipient row, no blocked-flag bookkeeping --
// that machinery exists to stop a *repeated* delivery failure from reading as
// noise, and a message that is never repeated has no repeats to distinguish.
// A failed send here is recorded and returned exactly like any other failure.
func (s *Server) deliverToRecipientOnce(ctx context.Context, ev models.Event, delivery routing.Delivery) error {
	if s.bot == nil {
		return errNoBotConfigured
	}

	rendered, err := s.renderMessage(ctx, ev, delivery)
	if err != nil {
		return err
	}
	recipient, err := s.recipientTarget(ctx, delivery)
	if err != nil {
		return err
	}
	msg, err := s.chatMessage(rendered)
	if err != nil {
		return err
	}
	ref := conversationRef(recipient)

	if _, err := s.bot.SendMessage(ctx, ref, msg); err != nil {
		s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomeFailed)
		return fmt.Errorf("bot send: %w", err)
	}
	s.metrics.DeliveryRecorded(ctx, routeLabel(delivery), metrics.OutcomePosted)
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

// deriveKey identifies an event whose sender gave it no key: the same source,
// origin, start and labels are the same event.
func deriveKey(ev models.Event) string {
	var url string
	var at time.Time
	switch {
	case ev.Alertmanager != nil:
		url, at = ev.Alertmanager.GeneratorURL, ev.Alertmanager.StartsAt
	case ev.Universal != nil:
		url, at = ev.Universal.URL, ev.Universal.Time
	}
	h := sha256.New()
	_, _ = h.Write([]byte(ev.Source))
	_, _ = h.Write([]byte(url))
	_, _ = h.Write([]byte(at.Format(time.RFC3339)))
	keys := make([]string, 0, len(ev.Labels))
	for key := range ev.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := ev.Labels[key]
		_, _ = h.Write([]byte(key))
		_, _ = h.Write([]byte("="))
		_, _ = h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}
