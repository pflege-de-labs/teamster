package httpserver

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestParseBotCommand(t *testing.T) {
	t.Parallel()

	mention := []botEntity{{Type: "mention", Text: "<at>Teamster</at>"}}
	tests := []struct {
		name     string
		text     string
		entities []botEntity
		want     string
		wantOK   bool
	}{
		{name: "slash help", text: "/help", want: commandHelp, wantOK: true},
		{name: "cased and padded", text: "  /HeLp ", want: commandHelp, wantOK: true},
		{name: "bare word", text: "status", want: commandStatus, wantOK: true},
		{name: "after a mention", text: "<at>Teamster</at>&nbsp;/test", entities: mention, want: commandTest, wantOK: true},
		{name: "slash with trailing words", text: "/unlink please", want: commandUnlink, wantOK: true},
		{name: "legacy stop", text: "stop", want: commandUnlink, wantOK: true},
		{name: "slash synonym", text: "/unsubscribe", want: commandUnlink, wantOK: true},
		{name: "unknown slash command", text: "/route team=ops", want: commandUnknown, wantOK: true},
		{name: "bare word inside a sentence", text: "help me please", wantOK: false},
		{name: "a link code", text: "ABCD-EFGH-JKMN", wantOK: false},
		{name: "empty", text: "   ", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseBotCommand(tt.text, tt.entities)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("parseBotCommand(%q) = %q, %v, want %q, %v", tt.text, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestBotCommandsReply(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		text    string
		linked  bool
		sendErr error
		// universalDefault seeds a default template for the universal source.
		universalDefault bool
		// want is matched against every text the bot sent, in order.
		want []string
	}{
		{name: "help", text: "/help", want: []string{helpReply}},
		{name: "unknown", text: "/nope", want: []string{unknownCommandReply}},
		{name: "status unlinked", text: "/status", want: []string{notLinkedStatus}},
		{name: "status linked", text: "/status", linked: true, want: []string{"**linked** to Alice"}},
		{name: "status lists routes", text: "/status", linked: true, want: []string{"- Mine: `severity=critical,team=ops`"}},
		{name: "test unlinked", text: "/test", want: []string{testNotLinkedReply}},
		{name: "test linked", text: "/test", linked: true, want: []string{"alerts reach this chat"}},
		{name: "test renders the universal default", text: "/test", linked: true, universalDefault: true, want: []string{"from the default template"}},
		{
			// The failed send and the reply both reach the fake; the reply carries a reference.
			name: "test failing", text: "/test", linked: true, sendErr: errors.New("401"),
			want: []string{"alerts reach this chat", "Reference"},
		},
		{name: "unlink", text: "/unlink", linked: true, want: []string{unlinkConfirmedReply}},
		{name: "a sentence", text: "help me please", want: []string{linkNeutralReply}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newBotFixture(t)
			f.botClient.err = tt.sendErr
			if tt.linked {
				recipient := seedRecipient(t, f.store, "alice", "conv-1")
				f.store.routes["mine"] = models.Route{
					ID: "mine", Name: "Mine", RecipientID: recipient.ID,
					LabelSelector: map[string]string{"team": "ops", "severity": "critical"},
				}
				f.store.routes["other"] = models.Route{ID: "other", Name: "Other", DestinationID: "dest"}
			}
			if tt.universalDefault {
				f.store.templates["uni"] = models.Template{ID: "uni", Name: "Uni", Text: "Sent from the default template", Sources: []string{models.SourceUniversal}}
				f.store.sourceDefaults[models.SourceUniversal] = "uni"
			}

			activity := botActivityFields()
			activity["text"] = tt.text
			if rec := f.post(t, activity); rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}

			sent := f.botClient.sentTexts()
			if len(sent) != len(tt.want) {
				t.Fatalf("sent = %q, want %d messages", sent, len(tt.want))
			}
			for i, want := range tt.want {
				if !strings.Contains(sent[i], want) {
					t.Errorf("message %d = %q, want it to contain %q", i, sent[i], want)
				}
			}
			if tt.name == "status lists routes" && strings.Contains(sent[0], "Other") {
				t.Errorf("status = %q, lists a route that does not deliver here", sent[0])
			}
		})
	}
}

func TestFormatStatus(t *testing.T) {
	t.Parallel()

	since := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		recipient models.Recipient
		routes    []models.Route
		want      []string
	}{
		{
			name:      "no routes",
			recipient: models.Recipient{ID: "r", Subject: "sub", CreatedAt: since},
			want:      []string{"linked** to sub since 2026-09-01", "No route delivers to this chat yet"},
		},
		{
			name:      "blocked",
			recipient: models.Recipient{ID: "r", Name: "A", CreatedAt: since, BlockedAt: since, BlockedReason: "MessageWritesBlocked"},
			want:      []string{"last delivery failed (MessageWritesBlocked)"},
		},
		{
			name:      "markdown in names is escaped",
			recipient: models.Recipient{ID: "r", Name: "*bold*", CreatedAt: since},
			routes:    []models.Route{{Name: "a_b", RecipientID: "r"}},
			want:      []string{`\*bold\*`, "- a\\_b: `every alert`"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := formatStatus(tt.recipient, tt.routes)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("formatStatus = %q, want it to contain %q", got, want)
				}
			}
		})
	}
}
