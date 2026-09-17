package teamsv2

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestParseRecognisesEveryShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		body      string
		wantTitle string
		wantText  string
		wantCards int
	}{
		{
			name:      "v2 envelope with one card",
			body:      `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":{"type":"AdaptiveCard"}}]}`,
			wantCards: 1,
		},
		{
			name:      "v2 envelope with several cards",
			body:      `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":{"a":1}},{"contentType":"application/vnd.microsoft.card.adaptive","content":{"b":2}}]}`,
			wantCards: 2,
		},
		{
			name:      "v2 envelope carrying text beside the card",
			body:      `{"type":"message","summary":"disk full","text":"<b>node-3</b>","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":{}}]}`,
			wantTitle: "disk full",
			wantText:  "<b>node-3</b>",
			wantCards: 1,
		},
		{
			name:     "attachments this service cannot send, alongside text it can",
			body:     `{"type":"message","text":"still readable","attachments":[{"contentType":"image/png","content":{}}]}`,
			wantText: "still readable",
		},
		{
			name:     "plain text",
			body:     `{"text":"something broke"}`,
			wantText: "something broke",
		},
		{
			name:      "plain text with a summary",
			body:      `{"summary":"CPU\nspiking","text":"on node-1"}`,
			wantTitle: "CPU spiking",
			wantText:  "on node-1",
		},
		{
			name:      "message card by @type",
			body:      `{"@type":"MessageCard","title":"Build failed","text":"branch main"}`,
			wantTitle: "Build failed",
			wantCards: 1,
		},
		{
			name:      "message card without @type, recognised by themeColor",
			body:      `{"themeColor":"FF0000","summary":"Deploy","text":"rolled back"}`,
			wantTitle: "Deploy",
			wantCards: 1,
		},
		{
			name:      "message card without @type, recognised by sections",
			body:      `{"title":"Nightly","sections":[{"text":"all green"}]}`,
			wantTitle: "Nightly",
			wantCards: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := Parse([]byte(tt.body))
			if err != nil {
				t.Fatalf("Parse() error = %v", err)
			}
			if msg.Title != tt.wantTitle {
				t.Errorf("Title = %q, want %q", msg.Title, tt.wantTitle)
			}
			if msg.Text != tt.wantText {
				t.Errorf("Text = %q, want %q", msg.Text, tt.wantText)
			}
			if len(msg.Cards) != tt.wantCards {
				t.Errorf("len(Cards) = %d, want %d", len(msg.Cards), tt.wantCards)
			}
		})
	}
}

func TestParseForwardsAnEnvelopeCardUnchanged(t *testing.T) {
	t.Parallel()

	// Whatever this service does not model has to survive the round trip, or a
	// card that worked against Teams stops working against this.
	card := `{"type":"AdaptiveCard","msteams":{"width":"Full"},"unknownToUs":[1,2,3]}`
	body := `{"type":"message","attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":` + card + `}]}`

	msg, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if got := string(msg.Cards[0]); got != card {
		t.Errorf("card = %s, want %s", got, card)
	}
}

func TestParseSanitizesText(t *testing.T) {
	t.Parallel()

	msg, err := Parse([]byte(`{"text":"<b>bold</b><script>alert(1)</script>"}`))
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if strings.Contains(msg.Text, "script") {
		t.Errorf("Text = %q, want the script element removed", msg.Text)
	}
	if !strings.Contains(msg.Text, "<b>bold</b>") {
		t.Errorf("Text = %q, want the safe markup kept", msg.Text)
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{name: "not JSON", body: `{`, wantErr: "invalid JSON"},
		{name: "JSON but not an object", body: `"a string"`, wantErr: "invalid JSON"},
		{name: "nothing to send", body: `{"type":"message"}`, wantErr: "neither text nor an adaptive card"},
		{name: "blank text", body: `{"text":"   "}`, wantErr: "neither text nor an adaptive card"},
		{
			name:    "only attachments this service cannot send",
			body:    `{"type":"message","attachments":[{"contentType":"image/png","content":{}}]}`,
			wantErr: "no attachment of type",
		},
		{
			name:    "card content is not an object",
			body:    `{"attachments":[{"contentType":"application/vnd.microsoft.card.adaptive","content":"nope"}]}`,
			wantErr: "not a JSON object",
		},
		{name: "message card that says nothing", body: `{"@type":"MessageCard"}`, wantErr: "neither text nor an adaptive card"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := Parse([]byte(tt.body))
			if err == nil {
				t.Fatalf("Parse() error = nil, want %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Parse() error = %q, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestParseReportsAnEmptyPayloadAsErrEmpty(t *testing.T) {
	t.Parallel()

	_, err := Parse([]byte(`{"type":"message"}`))
	if !errors.Is(err, ErrEmpty) {
		t.Errorf("Parse() error = %v, want ErrEmpty", err)
	}
}

// decodeCard is how the conversion tests read what was built, so they assert on
// the card rather than on one particular spelling of it.
func decodeCard(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()

	var card map[string]any
	if err := json.Unmarshal(raw, &card); err != nil {
		t.Fatalf("card is not valid JSON: %v", err)
	}
	return card
}
