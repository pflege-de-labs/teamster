package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/bot"
	"github.com/pflege-de-labs/teamster/internal/config"
	"github.com/pflege-de-labs/teamster/internal/models"
	"github.com/pflege-de-labs/teamster/internal/store"
)

// botActivity is the slice of the Bot Framework Activity schema this service
// reads. Fields it never acts on are left out rather than modelled and
// ignored, so there is nothing here a reviewer has to check is unused on
// purpose.
type botActivity struct {
	Type         string          `json:"type"`
	ServiceURL   string          `json:"serviceUrl"`
	ChannelID    string          `json:"channelId"`
	Text         string          `json:"text"`
	Conversation botConversation `json:"conversation"`
	From         botAccount      `json:"from"`
	Recipient    botAccount      `json:"recipient"`
	MembersAdded []botAccount    `json:"membersAdded,omitempty"`
	Entities     []botEntity     `json:"entities,omitempty"`
	ChannelData  botChannelData  `json:"channelData,omitempty"`
}

type botConversation struct {
	ID               string `json:"id"`
	ConversationType string `json:"conversationType"`
}

// botAccount covers both the sender (From), the bot's own identity in this
// conversation (Recipient), and an entry of MembersAdded. Teams ids are
// "28:<app-id>" for a bot and "29:..." for a user, which is why Recipient.ID
// -- not the configured App ID -- is what tells a bot-added event apart from
// a person joining.
type botAccount struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AADObjectID string `json:"aadObjectId,omitempty"`
}

// botEntity is a mention, when present. Teams sends the display text that was
// inserted for the mention (e.g. "<at>Team Alerts</at>") in Text, matched
// verbatim in Entities.Text.
type botEntity struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// botChannelData is a Teams-specific envelope go-oidc and the base Activity
// schema know nothing about, so its fields are read defensively: an activity
// missing it (or missing Tenant) decodes to a zero value rather than an error.
type botChannelData struct {
	Tenant struct {
		ID string `json:"id"`
	} `json:"tenant"`
}

// activityShapeRefusal is the cheap, non-secret gate applied before any bearer
// token is even looked at: see the ordering comment on handleBotMessages.
// Returning "" means the activity is worth authenticating.
func activityShapeRefusal(activity botActivity, bot config.BotConfig) string {
	if activity.ChannelID != "msteams" {
		return "unsupported-channel"
	}
	switch activity.Type {
	case "message", "conversationUpdate":
	default:
		return "unsupported-type"
	}
	if activity.Conversation.ConversationType != "personal" {
		return "unsupported-conversation"
	}
	// A single-tenant registration has exactly one tenant it should ever hear
	// from; an absent tenant (Teams omits channelData for some activity
	// shapes) is not evidence of anything and passes through.
	if tenant := activity.ChannelData.Tenant.ID; bot.TenantType != "multi" && tenant != "" && tenant != bot.TenantID {
		return "tenant-mismatch"
	}
	return ""
}

// handleBotMessages is the one endpoint Microsoft's signature authenticates
// rather than any of teamster's own credentials, so every check below is load
// bearing. Order matters: the shape gate runs before the bearer token is even
// looked at, because the token's kid is attacker-controlled and acting on it
// -- verifying a signature, and on an unrecognised kid fetching this
// process's own JWKS cache from the network -- is not free the way parsing
// JSON and comparing strings is.
func (s *Server) handleBotMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, botBodyLimit)

	var activity botActivity
	if err := json.NewDecoder(r.Body).Decode(&activity); err != nil {
		status := "invalid-json"
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			status = "body-too-large"
		}
		s.metrics.WebhookReceived(ctx, "bot", status)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if refusal := activityShapeRefusal(activity, s.cfg.Bot); refusal != "" {
		s.metrics.WebhookReceived(ctx, "bot", refusal)
		// Answering anything but 2xx to a request Microsoft signed and
		// delivered correctly reads, to its tooling, as "this bot's auth is
		// broken" -- this is merely an activity this bot does not act on, the
		// same reasoning dispatchBotActivity's own "always 200" already uses.
		w.WriteHeader(http.StatusOK)
		return
	}

	token, bearer := parseBearer(r.Header.Get("Authorization"))
	if bearer != bearerOK {
		status := "no-bearer"
		if bearer == bearerMalformed {
			status = "malformed-bearer"
		}
		s.metrics.WebhookReceived(ctx, "bot", status)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	status, ok, forbidden := s.verifyBotToken(ctx, token, activity.ServiceURL, activity.ChannelID)
	// Counted before the status is written, the same order handleAlertmanager
	// uses for its own refusal.
	s.metrics.WebhookReceived(ctx, "bot", status)
	switch {
	case forbidden:
		w.WriteHeader(http.StatusForbidden)
		return
	case !ok:
		// A failure to reach our own metadata or JWKS cache is not the
		// caller's fault and is worth a retry, unlike a bad signature.
		if status == "metadata-unreachable" || status == "endorsements-unreachable" {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	s.dispatchBotActivity(ctx, activity)
	w.WriteHeader(http.StatusOK)
}

// dispatchBotActivity runs only once every check above has passed, so
// everything here may read the activity's content and act on it. It always
// leaves the caller to answer 200 -- see handleBotMessages -- because the Bot
// Connector retries anything else, and none of these branches fail in a way a
// retry would fix. activityShapeRefusal admits only "message" and
// "conversationUpdate", so those are the only two cases; every activity that
// falls through -- message included, which is what actually arrives for a
// personal-scope bot -- gets the service-url refresh below.
func (s *Server) dispatchBotActivity(ctx context.Context, activity botActivity) {
	switch activity.Type {
	case "conversationUpdate":
		if botWasAdded(activity) {
			s.replyText(ctx, activity, installInstructions)
			return
		}
	case "message":
		s.handleBotMessage(ctx, activity)
	}
	s.refreshRecipientServiceURL(ctx, activity)
}

// botWasAdded reports whether this activity's membersAdded includes the bot
// itself. Recipient.ID is the bot's id *in this conversation* -- "28:<app-id>"
// for Teams -- not the configured App ID, so comparing against Recipient.ID is
// what tells the bot's own install apart from a person joining the chat.
func botWasAdded(activity botActivity) bool {
	for _, member := range activity.MembersAdded {
		if member.ID == activity.Recipient.ID {
			return true
		}
	}
	return false
}

const (
	installInstructions = "Thanks for adding me! Ask an admin for a link code in the " +
		"teamster admin UI, then send it to me here to connect your alerts to this chat."
	linkNeutralReply = "That does not match an active link code. Ask an admin for a new " +
		"one, or check the one you have for typos."
	linkConfirmedReply = "You're linked. Alerts will start arriving in this chat."
)

// mentionMarkup strips <at>...</at> mention tags Teams inserts around the
// bot's own display name at the front of a message. It is a fallback for a
// client that does not also list the mention in Entities.
var mentionMarkup = regexp.MustCompile(`(?is)<at[^>]*>.*?</at>`)

// linkCodeRun matches one grouped code, dashes between groups optional,
// wherever it appears in a longer message. It is built from linkCodeAlphabet
// itself rather than a hand-written character range, so it cannot silently
// drift from the alphabet the way linkCodeShape's test regex once did.
var linkCodeRun = regexp.MustCompile(buildLinkCodeRunPattern())

func buildLinkCodeRunPattern() string {
	group := "[" + regexp.QuoteMeta(linkCodeAlphabet) + "]{" + strconv.Itoa(linkCodeGroupLen) + "}"
	return group + strings.Repeat("-?"+group, linkCodeGroups-1)
}

// normalizeLinkCode turns whatever a person actually typed or tapped into the
// bare code it might be. A raw == against the stored code misses all of this:
// Teams wraps an @-mention in <at> tags and often lists it again in Entities,
// &nbsp; shows up routinely in place of an ordinary space, a code is usually
// typed or read back with its grouping dashes, and a code is often only part
// of a longer message ("here you go ABCD-EFGH-JKMN"). Finding the first
// code-shaped run handles all of that; stripping every whitespace character
// from the whole message first, as this used to, does not -- it turns "here
// you go ABCD-EFGH-JKMN" into "hereyougoABCDEFGHJKMN".
func normalizeLinkCode(text string, entities []botEntity) string {
	for _, entity := range entities {
		if entity.Type == "mention" && entity.Text != "" {
			text = strings.ReplaceAll(text, entity.Text, "")
		}
	}
	text = mentionMarkup.ReplaceAllString(text, "")
	text = html.UnescapeString(text)
	text = strings.ToUpper(text)
	return strings.ReplaceAll(linkCodeRun.FindString(text), "-", "")
}

func (s *Server) handleBotMessage(ctx context.Context, activity botActivity) {
	code := normalizeLinkCode(activity.Text, activity.Entities)
	if code == "" {
		s.replyText(ctx, activity, linkNeutralReply)
		return
	}

	if _, err := s.redeemLinkCode(ctx, activity, code); err != nil {
		if !errors.Is(err, store.ErrNotFound) {
			logError("redeem link code", err)
		}
		// A code that does not exist and a code that expired look the same
		// from here on purpose: telling them apart would tell a guesser
		// whether they are close.
		s.replyText(ctx, activity, linkNeutralReply)
		return
	}
	s.replyText(ctx, activity, linkConfirmedReply)
}

// redeemLinkCode spends code, then looks up any existing recipient for its
// subject and creates or updates.
//
// TakeLinkFlow runs first, on the plain store rather than inside the
// transaction below, so it commits on its own: nested inside a transaction
// that later rolled back -- on an expired code, or on a failure in the
// create/update after it -- the code's deletion would roll back with it,
// letting the same code be retried. Spending it unconditionally here is what
// TakeLinkFlow's own doc comment promises ("cannot be retried until it is
// guessed right"); doing so from inside a rollback-able transaction broke
// that promise for exactly the expired-code case.
//
// The create-or-update still runs inside one serializable transaction of its
// own: two live codes for the same subject redeemed concurrently would both
// miss the GetRecipientBySubject lookup and both try to insert, which only
// the unique index on subject catches, after the fact.
//
// Subject comes from the redeemed LinkFlow alone, never from the activity: the
// activity is whoever is holding the phone, and the flow is who an
// already-authenticated admin-UI session issued the code to.
func (s *Server) redeemLinkCode(ctx context.Context, activity botActivity, code string) (models.Recipient, error) {
	flow, err := s.store.TakeLinkFlow(ctx, code)
	if err != nil {
		return models.Recipient{}, err
	}

	var recipient models.Recipient
	var displaced *models.Recipient
	err = s.store.WithSerializableTx(ctx, func(ctx context.Context, tx store.Store) error {
		conversation := models.Recipient{
			Subject:        flow.Subject,
			Name:           activity.From.Name,
			AADObjectID:    activity.From.AADObjectID,
			ConversationID: activity.Conversation.ID,
			ServiceURL:     activity.ServiceURL,
			BotChannelID:   activity.ChannelID,
			TenantID:       activity.ChannelData.Tenant.ID,
		}

		existing, err := tx.GetRecipientBySubject(ctx, flow.Subject)
		switch {
		case err == nil:
			conversation.ID = existing.ID
			// Teams omits channelData for some activity shapes; an absent
			// tenant here must not blank out a previously-known-good one.
			if conversation.TenantID == "" {
				conversation.TenantID = existing.TenantID
			}
			if existing.ConversationID != conversation.ConversationID {
				previous := existing
				displaced = &previous
			}
			recipient, err = tx.UpdateRecipient(ctx, conversation)
			return err
		case errors.Is(err, store.ErrNotFound):
			recipient, err = tx.CreateRecipient(ctx, conversation)
			return err
		default:
			return err
		}
	})
	if err != nil {
		return models.Recipient{}, err
	}

	if displaced != nil {
		// A leaked code must not silently steal someone's alert stream: tell
		// the conversation it just lost before moving on.
		s.notifyConversationDisplaced(ctx, *displaced)
	}
	return recipient, nil
}

// linkDisplacedReply tells whoever was previously receiving alerts in a
// conversation that a new redemption just took over their subject's link.
const linkDisplacedReply = "This chat has been unlinked: a link code for the same account was just " +
	"redeemed elsewhere. If that wasn't expected, ask an admin for a new code."

// notifyConversationDisplaced is best-effort, like replyText: the redemption
// that displaced this conversation has already succeeded, so a failure to
// notify here is logged rather than allowed to undo it.
func (s *Server) notifyConversationDisplaced(ctx context.Context, previous models.Recipient) {
	if s.bot == nil {
		return
	}
	ref := bot.ConversationReference{
		ServiceURL:     previous.ServiceURL,
		ConversationID: previous.ConversationID,
		BotChannelID:   previous.BotChannelID,
		TenantID:       previous.TenantID,
		AADObjectID:    previous.AADObjectID,
	}
	if _, err := s.bot.SendMessage(ctx, ref, bot.Message{Text: linkDisplacedReply}); err != nil {
		logError("bot displaced notice", fmt.Errorf("conversation %s: %w", previous.ConversationID, err))
	}
}

// refreshRecipientServiceURL is the only signal this service ever gets that
// Teams moved a conversation to a different regional endpoint: nothing pushes
// that change, and a stale URL otherwise fails silently months later when a
// delivery to it starts erroring. Recipients are scanned rather than looked up
// by conversation id because the store exposes no such index; the list is
// bounded by how many people asked for alerts in a chat, which is not a scale
// where that matters.
func (s *Server) refreshRecipientServiceURL(ctx context.Context, activity botActivity) {
	if activity.Conversation.ID == "" {
		return
	}

	recipients, err := s.store.ListRecipients(ctx)
	if err != nil {
		logError("list recipients for service url refresh", err)
		return
	}

	// Deliberately the first match, not every match: a "personal" conversation
	// is 1:1 by construction (activityShapeRefusal admits no other
	// conversationType), so at most one recipient should ever carry a given
	// ConversationID.
	for _, recipient := range recipients {
		if recipient.ConversationID != activity.Conversation.ID {
			continue
		}
		if recipient.ServiceURL == activity.ServiceURL {
			return
		}
		recipient.ServiceURL = activity.ServiceURL
		if _, err := s.store.UpdateRecipient(ctx, recipient); err != nil {
			logError("refresh recipient service url", err)
		}
		return
	}
}

// replyText sends a plain-text activity back into the same conversation. It is
// a best-effort reply: the inbound activity has already been accepted with a
// 200, so a failure here is logged rather than turned into a retry that would
// re-run whatever the message just did.
func (s *Server) replyText(ctx context.Context, activity botActivity, text string) {
	if s.bot == nil {
		return
	}
	ref := bot.ConversationReference{
		ServiceURL:     activity.ServiceURL,
		ConversationID: activity.Conversation.ID,
		BotChannelID:   activity.ChannelID,
		TenantID:       activity.ChannelData.Tenant.ID,
		AADObjectID:    activity.From.AADObjectID,
	}
	if _, err := s.bot.SendMessage(ctx, ref, bot.Message{Text: text}); err != nil {
		logError("bot reply", fmt.Errorf("conversation %s: %w", activity.Conversation.ID, err))
	}
}
