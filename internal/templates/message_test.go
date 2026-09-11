package templates

import (
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func sampleAlert() models.Alert {
	return models.Alert{
		Status:      "firing",
		Labels:      map[string]string{"alertname": "HighCPU", "severity": "critical"},
		Annotations: map[string]string{"summary": "CPU spiking"},
	}
}

func TestRenderMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		template  models.Template
		alert     models.Alert
		wantTitle string
		wantText  string
		wantCard  string
	}{
		{
			name:      "the title is rendered against the alert",
			template:  models.Template{Title: "{{ .Alert.Labels.alertname }} is {{ .Alert.Status }}"},
			alert:     sampleAlert(),
			wantTitle: "HighCPU is firing",
		},
		{
			// The feed shows one line, so a template spanning several must not
			// arrive with newlines in it.
			name:      "a multi-line title collapses",
			template:  models.Template{Title: "{{ .Alert.Labels.alertname }}\n  is  \n{{ .Alert.Status }}"},
			alert:     sampleAlert(),
			wantTitle: "HighCPU is firing",
		},
		{
			name:      "a card without a title gets the one this service always sent",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     sampleAlert(),
			wantTitle: "CPU spiking",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			name:      "that fallback walks to the alertname",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     models.Alert{Labels: map[string]string{"alertname": "HighCPU"}},
			wantTitle: "HighCPU",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			name:      "and then to a generic line",
			template:  models.Template{Body: `{"type":"AdaptiveCard"}`},
			alert:     models.Alert{},
			wantTitle: "Alert update",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
		{
			// Text previews itself in the feed, so inventing a title would only
			// repeat what the reader is about to see.
			name:     "text without a title is left alone",
			template: models.Template{Text: "<p>{{ .Alert.Labels.alertname }}</p>"},
			alert:    sampleAlert(),
			wantText: "<p>HighCPU</p>",
		},
		{
			name:      "all three parts render together",
			template:  models.Template{Title: "T", Text: "<b>bold</b>", Body: `{"type":"AdaptiveCard"}`},
			alert:     sampleAlert(),
			wantTitle: "T",
			wantText:  "<b>bold</b>",
			wantCard:  `{"type":"AdaptiveCard"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RenderMessage(tt.template, RenderData{Alert: tt.alert, Now: "2026-02-09T09:00:00Z"})
			if err != nil {
				t.Fatalf("RenderMessage: %v", err)
			}
			if msg.Title != tt.wantTitle {
				t.Errorf("title = %q, want %q", msg.Title, tt.wantTitle)
			}
			if msg.Text != tt.wantText {
				t.Errorf("text = %q, want %q", msg.Text, tt.wantText)
			}
			if string(msg.Card) != tt.wantCard {
				t.Errorf("card = %s, want %s", msg.Card, tt.wantCard)
			}
		})
	}
}

func TestRenderMessageReportsWhichPartBroke(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template models.Template
		wantErr  string
	}{
		{name: "title", template: models.Template{Title: "{{"}, wantErr: "title: parse template"},
		{name: "text", template: models.Template{Text: "{{ .Missing.Field }}"}, wantErr: "text: execute template"},
		{name: "card", template: models.Template{Body: "not json"}, wantErr: "not valid JSON"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := RenderMessage(tt.template, RenderData{Alert: sampleAlert()})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to name the %s", err, tt.name)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template models.Template
		wantErr  bool
	}{
		{name: "a card alone", template: models.Template{Body: "{}"}},
		{name: "a title alone", template: models.Template{Title: "hello"}},
		{name: "text alone", template: models.Template{Text: "hello"}},
		{name: "nothing to send", template: models.Template{Name: "named but empty"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := Validate(tt.template)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() = %v, want error: %v", err, tt.wantErr)
			}
		})
	}
}

// A nil map is what an alert without annotations arrives as, and a fallback
// that cannot survive one is no fallback at all.
func TestDefaultHelperSurvivesMissingKeys(t *testing.T) {
	t.Parallel()

	card, err := Render(`{"text":"{{ default .Alert.Annotations.summary "none" }}"}`, RenderData{Alert: models.Alert{}})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := `{"text":"none"}`; string(card) != want {
		t.Errorf("card = %s, want %s", card, want)
	}
}
