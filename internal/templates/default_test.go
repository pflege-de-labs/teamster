package templates

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		alert     models.Alert
		wantTitle string
		want      []string
		wantNot   []string
	}{
		{
			name: "an Alertmanager alert",
			alert: models.Alert{
				Source: "alertmanager", Status: "firing",
				Labels:      map[string]string{"alertname": "HighCPU"},
				Annotations: map[string]string{"summary": "CPU is hot", "description": "load **15**"},
				StartsAt:    time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
			},
			wantTitle: "CPU is hot",
			want:      []string{"<strong>Status:</strong> firing", "load <strong>15</strong>", "<pre><code", "2026-09-23T12:00:00Z"},
			wantNot:   []string{"ends_at"},
		},
		{
			name:      "no annotations",
			alert:     models.Alert{Labels: map[string]string{"alertname": "DiskFull"}},
			wantTitle: "DiskFull",
			want:      []string{"&#34;alertname&#34;: &#34;DiskFull&#34;"},
			wantNot:   []string{"Status:", "annotations"},
		},
		{
			name:      "nothing at all",
			alert:     models.Alert{},
			wantTitle: "Alert update",
			want:      []string{"{}"},
		},
		{
			// A label cannot close the code block and smuggle Markdown in.
			name:      "backticks in a label",
			alert:     models.Alert{Labels: map[string]string{"alertname": "x```\n# heading"}},
			wantTitle: "x``` # heading",
			wantNot:   []string{"<h1>"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := Default(tt.alert)
			if err != nil {
				t.Fatalf("Default: %v", err)
			}
			if msg.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", msg.Title, tt.wantTitle)
			}
			for _, want := range tt.want {
				if !strings.Contains(msg.Text, want) {
					t.Errorf("text = %q, want it to contain %q", msg.Text, want)
				}
			}
			for _, unwanted := range tt.wantNot {
				if strings.Contains(msg.Text, unwanted) {
					t.Errorf("text = %q, want no %q", msg.Text, unwanted)
				}
			}
			if msg.Card != nil || msg.Notice != nil {
				t.Errorf("card = %s, notice = %s; want neither, the caller adds the notice", msg.Card, msg.Notice)
			}
		})
	}
}

func TestHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		adminURL string
		wantURL  string
	}{
		{name: "with an admin URL", adminURL: "https://teamster.example.com/", wantURL: "https://teamster.example.com/admin#templates"},
		{name: "without one"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			raw, err := HintCard(tt.adminURL, "/admin#templates")
			if err != nil {
				t.Fatalf("HintCard: %v", err)
			}
			var card struct {
				Type    string `json:"type"`
				Version string `json:"version"`
				Body    []struct {
					Text string `json:"text"`
				} `json:"body"`
				Actions []struct {
					Type string `json:"type"`
					URL  string `json:"url"`
				} `json:"actions"`
			}
			if err := json.Unmarshal(raw, &card); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if card.Type != "AdaptiveCard" || len(card.Body) != 1 || !strings.HasPrefix(card.Body[0].Text, HintNoTemplate) {
				t.Errorf("card = %s, want an Adaptive Card saying no template is defined", raw)
			}

			text := HintText(tt.adminURL, "/admin#templates")
			if !strings.Contains(text, HintNoTemplate) {
				t.Errorf("text = %q, want the hint", text)
			}

			if tt.wantURL == "" {
				if len(card.Actions) != 0 || strings.Contains(text, "](") {
					t.Errorf("card = %s, text = %q; want no link without an admin URL", raw, text)
				}
				return
			}
			if len(card.Actions) != 1 || card.Actions[0].Type != "Action.OpenUrl" || card.Actions[0].URL != tt.wantURL {
				t.Errorf("actions = %+v, want one link to %s", card.Actions, tt.wantURL)
			}
			if !strings.Contains(text, "("+tt.wantURL+")") {
				t.Errorf("text = %q, want a link to %s", text, tt.wantURL)
			}
		})
	}
}
