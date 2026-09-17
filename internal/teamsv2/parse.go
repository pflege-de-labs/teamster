package teamsv2

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/pflege-de-labs/teamster/internal/templates"
)

// Message is a parsed payload, in the three parts a Teams message has. It is
// deliberately the same shape as templates.Message: whichever way a message was
// produced, delivery sees one thing.
type Message struct {
	Title string
	Text  string
	Cards []json.RawMessage
}

// ErrEmpty is a body that parsed as JSON and said nothing -- no card, no text.
// It is separate because it is the one failure a sender can fix by looking at
// what it sent rather than at how it encoded it.
var ErrEmpty = errors.New("payload carries neither text nor an adaptive card")

// probe is every field the three shapes are told apart by, read in one pass.
type probe struct {
	AtType          string            `json:"@type"`
	ThemeColor      string            `json:"themeColor"`
	Sections        []json.RawMessage `json:"sections"`
	PotentialAction []json.RawMessage `json:"potentialAction"`
	Attachments     []Attachment      `json:"attachments"`
	Summary         string            `json:"summary"`
	Text            string            `json:"text"`
}

// Parse reads a Teams webhook body. The shape is recognised from the body
// itself rather than from a content type or a query parameter, because a sender
// being migrated sends what it always sent and is not going to start labelling
// it.
func Parse(data []byte) (Message, error) {
	var p probe
	if err := json.Unmarshal(data, &p); err != nil {
		return Message{}, fmt.Errorf("invalid JSON: %w", err)
	}

	// A MessageCard is claimed by @type, but plenty of senders in the wild omit
	// it, so the fields only that shape has count as well.
	if strings.EqualFold(p.AtType, "MessageCard") ||
		p.ThemeColor != "" || len(p.Sections) > 0 || len(p.PotentialAction) > 0 {
		var mc MessageCard
		if err := json.Unmarshal(data, &mc); err != nil {
			return Message{}, fmt.Errorf("invalid MessageCard: %w", err)
		}
		return fromMessageCard(mc)
	}

	if len(p.Attachments) > 0 {
		return fromEnvelope(p)
	}

	if strings.TrimSpace(p.Text) != "" {
		return textMessage(p.Summary, p.Text)
	}

	return Message{}, ErrEmpty
}

func fromEnvelope(p probe) (Message, error) {
	msg, err := textMessage(p.Summary, p.Text)
	if err != nil {
		return Message{}, err
	}

	for _, att := range p.Attachments {
		if !strings.EqualFold(att.ContentType, AdaptiveCardContentType) {
			continue
		}
		if !isJSONObject(att.Content) {
			return Message{}, errors.New("adaptive card attachment is not a JSON object")
		}
		msg.Cards = append(msg.Cards, att.Content)
	}

	if len(msg.Cards) == 0 {
		if msg.Text != "" {
			// Attachments this service cannot send, but text it can: sending
			// the text beats refusing the message.
			return msg, nil
		}
		return Message{}, fmt.Errorf("no attachment of type %s", AdaptiveCardContentType)
	}
	return msg, nil
}

// textMessage is the text half of every shape: the summary previews in the
// activity feed, and the text is sanitized here so that this path gives the
// same guarantee the template path does.
func textMessage(summary, text string) (Message, error) {
	msg := Message{Title: oneLine(summary)}
	if strings.TrimSpace(text) == "" {
		return msg, nil
	}
	safe, err := templates.Sanitize(text)
	if err != nil {
		return Message{}, fmt.Errorf("text: %w", err)
	}
	msg.Text = safe
	return msg, nil
}

// oneLine is what the activity feed shows: one line, however many the sender
// wrote.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimLeft(raw, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}
