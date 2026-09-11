package templates

import (
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

func modelsTemplate(text string) models.Template { return models.Template{Text: text} }

func alertWithSummary(summary string) models.Alert {
	return models.Alert{Annotations: map[string]string{"summary": summary}}
}

func TestSanitize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty stays empty", input: "   ", want: ""},
		{name: "plain text is escaped", input: "5 < 6 & rising", want: "5 &lt; 6 &amp; rising"},
		{name: "formatting survives", input: "<p>a <b>bold</b> <i>claim</i><br></p>", want: "<p>a <b>bold</b> <i>claim</i><br></p>"},
		{name: "lists survive", input: "<ul><li>one</li><li>two</li></ul>", want: "<ul><li>one</li><li>two</li></ul>"},
		{
			// Dropping the content would lose the message; only the markup goes.
			name:  "an unknown element is unwrapped",
			input: `<div class="x">kept</div>`,
			want:  "kept",
		},
		{
			name:  "a script is dropped whole",
			input: "<p>before</p><script>steal()</script><p>after</p>",
			want:  "<p>before</p><p>after</p>",
		},
		{
			name:  "a style block is dropped whole",
			input: "<style>p{display:none}</style><p>after</p>",
			want:  "<p>after</p>",
		},
		{
			name:  "attributes are dropped",
			input: `<p onclick="steal()" style="color:red">text</p>`,
			want:  "<p>text</p>",
		},
		{
			name:  "an http link keeps its href",
			input: `<a href="https://example.com/runbook">runbook</a>`,
			want:  `<a href="https://example.com/runbook" rel="noopener noreferrer">runbook</a>`,
		},
		{
			name:  "a javascript link loses it",
			input: `<a href="javascript:steal()">click</a>`,
			want:  "<a>click</a>",
		},
		{
			name:  "a data link loses it too",
			input: `<a href="data:text/html;base64,PHNjcmlwdD4=">click</a>`,
			want:  "<a>click</a>",
		},
		{
			name:  "an image cannot be smuggled in",
			input: `<img src="https://tracker.example/pixel.gif">`,
			want:  "",
		},
		{
			name:  "a comment carries nothing a reader needs",
			input: "<p>a</p><!-- note -->",
			want:  "<p>a</p>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Sanitize(tt.input)
			if err != nil {
				t.Fatalf("Sanitize: %v", err)
			}
			if got != tt.want {
				t.Errorf("Sanitize(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// The alert, not just the template author, reaches the rendered text.
func TestSanitizeNeutralisesTemplatedMarkup(t *testing.T) {
	t.Parallel()

	msg, err := RenderMessage(
		modelsTemplate("<p>{{ .Alert.Annotations.summary }}</p>"),
		RenderData{Alert: alertWithSummary(`<img src=x onerror="steal()">`)},
	)
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	if want := "<p></p>"; msg.Text != want {
		t.Errorf("text = %q, want the injected markup gone (%q)", msg.Text, want)
	}
}
