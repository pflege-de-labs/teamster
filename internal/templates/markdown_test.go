package templates

import (
	"strings"
	"testing"

	"github.com/pflege-de-labs/teamster/internal/models"
)

// The emitter's input is closed: whatever Sanitize admits, and nothing else.
// Each case is therefore a tag from allowedTags rather than markup in general.
func TestToMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		html string
		want string
	}{
		{name: "empty", html: "", want: ""},
		{name: "whitespace only", html: "   \n ", want: ""},
		{name: "a paragraph is a block", html: "<p>hello</p>", want: "hello"},
		{name: "two paragraphs are separated by a blank line", html: "<p>one</p><p>two</p>", want: "one\n\ntwo"},
		{name: "bold", html: "<p><b>loud</b></p>", want: "**loud**"},
		{name: "strong is bold too", html: "<p><strong>loud</strong></p>", want: "**loud**"},
		{name: "italic", html: "<p><i>soft</i></p>", want: "_soft_"},
		{name: "em is italic too", html: "<p><em>soft</em></p>", want: "_soft_"},
		{name: "strikethrough", html: "<p><s>gone</s></p>", want: "~~gone~~"},
		{
			// Bot Framework's subset has no underline, so the emphasis is what
			// is lost rather than the words.
			name: "underline degrades to its text",
			html: "<p><u>underlined</u></p>",
			want: "underlined",
		},
		{
			name: "emphasis keeps surrounding space outside the markers",
			html: "<p>a<b> b </b>c</p>",
			want: "a **b** c",
		},
		{name: "empty emphasis emits no markers", html: "<p><b>  </b>x</p>", want: "x"},
		{name: "a break is a hard break", html: "<p>one<br>two</p>", want: "one  \ntwo"},
		{name: "headings", html: "<h1>a</h1><h2>b</h2><h3>c</h3>", want: "# a\n\n## b\n\n### c"},
		{name: "inline code", html: "<p><code>x := 1</code></p>", want: "`x := 1`"},
		{
			// A fence has to outlast the longest backtick run inside it.
			name: "inline code containing backticks",
			html: "<p><code>a ` b</code></p>",
			want: "``a ` b``",
		},
		{name: "code block", html: "<pre>go build\ngo test</pre>", want: "```\ngo build\ngo test\n```"},
		{name: "unordered list", html: "<ul><li>one</li><li>two</li></ul>", want: "- one\n- two"},
		{name: "ordered list numbers itself", html: "<ol><li>one</li><li>two</li></ol>", want: "1. one\n2. two"},
		{
			name: "a nested list stays inside its item",
			html: "<ul><li>one<ul><li>deeper</li></ul></li></ul>",
			want: "- one\n\n  - deeper",
		},
		{name: "blockquote", html: "<blockquote><p>quoted</p></blockquote>", want: "> quoted"},
		{
			name: "a multi-block quote carries the marker through the blank line",
			html: "<blockquote><p>one</p><p>two</p></blockquote>",
			want: "> one\n>\n> two",
		},
		{name: "a link", html: `<p><a href="https://example.com">docs</a></p>`, want: "[docs](https://example.com)"},
		{
			// The plain destination form ends at the first ")", so a URL
			// carrying one needs the angle-bracket form.
			name: "a link whose url has parentheses",
			html: `<p><a href="https://example.com/a(b)">docs</a></p>`,
			want: "[docs](<https://example.com/a(b)>)",
		},
		{
			name: "a link with no text falls back to its url",
			html: `<p><a href="https://example.com"></a></p>`,
			want: `[https://example\.com](https://example.com)`,
		},
		{
			// Alert data is what reaches here, so punctuation that would
			// restyle the message is neutralized rather than trusted.
			name: "markdown metacharacters in text are escaped",
			html: "<p>50% *not emphasis* [not a link]</p>",
			want: `50% \*not emphasis\* \[not a link\]`,
		},
		{
			name: "an entity is unescaped once, not emitted as markup",
			html: "<p>a &lt;b&gt; c</p>",
			want: `a \<b\> c`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ToMarkdown(tt.html)
			if err != nil {
				t.Fatalf("ToMarkdown(%q): %v", tt.html, err)
			}
			if got != tt.want {
				t.Errorf("ToMarkdown(%q) = %q, want %q", tt.html, got, tt.want)
			}
		})
	}
}

// The two outputs come from one sanitized source, which is the whole point of
// composing it this way: a template renders to the same message in a channel
// and in a chat, and neither is reached without passing Sanitize.
func TestMessageReachesBothTransports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		text         string
		wantHTML     string
		wantMarkdown string
	}{
		{
			// Authored before ADR 0029, so HTML. It must arrive in a channel
			// exactly as it always did, and as markdown rather than literal
			// tags in a chat.
			name:         "an html template",
			text:         "<p><b>HighCPU</b> is firing</p><p>worker is hot</p>",
			wantHTML:     "<p><b>HighCPU</b> is firing</p><p>worker is hot</p>",
			wantMarkdown: "**HighCPU** is firing\n\nworker is hot",
		},
		{
			name:         "a markdown template",
			text:         "**HighCPU** is firing\n\nworker is hot",
			wantHTML:     "<p><strong>HighCPU</strong> is firing</p>\n<p>worker is hot</p>",
			wantMarkdown: "**HighCPU** is firing\n\nworker is hot",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RenderMessage(models.Template{Text: tt.text}, RenderData{Alert: sampleAlert()})
			if err != nil {
				t.Fatalf("RenderMessage: %v", err)
			}
			if msg.Text != tt.wantHTML {
				t.Errorf("html = %q, want %q", msg.Text, tt.wantHTML)
			}

			markdown, err := ToMarkdown(msg.Text)
			if err != nil {
				t.Fatalf("ToMarkdown: %v", err)
			}
			if markdown != tt.wantMarkdown {
				t.Errorf("markdown = %q, want %q", markdown, tt.wantMarkdown)
			}
		})
	}
}

// An annotation is attacker-controlled. Neither output may carry anything out
// of it that the sanitizer did not admit.
func TestAlertDataCannotEscapeIntoEitherTransport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		summary      string
		wantHTML     []string
		wantNotHTML  []string
		wantMarkdown string
	}{
		{
			name:         "a script tag is dropped with its contents",
			summary:      "<script>steal()</script>fine",
			wantNotHTML:  []string{"script", "steal"},
			wantMarkdown: "fine",
		},
		{
			// Accepted, not overlooked: since templates are Markdown, an
			// annotation spelling a link produces one, on both transports.
			// ADR 0029 records why -- the same link was already reachable by
			// writing <a href> in an annotation, templates need runbook links,
			// and the schemes that execute rather than navigate are what the
			// allowlist actually holds back. The case below is that bound.
			name:         "a link from an annotation is accepted, with rel set",
			summary:      "[click](https://evil.example)",
			wantNotHTML:  []string{"javascript:", "<script"},
			wantHTML:     []string{`<a href="https://evil.example" rel="noopener noreferrer">`},
			wantMarkdown: "[click](https://evil.example)",
		},
		{
			name:         "a javascript href does not survive",
			summary:      `<a href="javascript:steal()">click</a>`,
			wantNotHTML:  []string{"javascript:"},
			wantMarkdown: "click",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg, err := RenderMessage(
				models.Template{Text: "{{ .Alert.Annotations.summary }}"},
				RenderData{Alert: models.Alert{Annotations: map[string]string{"summary": tt.summary}}},
			)
			if err != nil {
				t.Fatalf("RenderMessage: %v", err)
			}
			for _, unwanted := range tt.wantNotHTML {
				if strings.Contains(msg.Text, unwanted) {
					t.Errorf("html %q still contains %q", msg.Text, unwanted)
				}
			}
			for _, wanted := range tt.wantHTML {
				if !strings.Contains(msg.Text, wanted) {
					t.Errorf("html %q does not contain %q", msg.Text, wanted)
				}
			}

			markdown, err := ToMarkdown(msg.Text)
			if err != nil {
				t.Fatalf("ToMarkdown: %v", err)
			}
			if markdown != tt.wantMarkdown {
				t.Errorf("markdown = %q, want %q", markdown, tt.wantMarkdown)
			}
		})
	}
}

// Pinning the option rather than the parser: without WithUnsafe every template
// written before ADR 0029 -- which is all of them -- renders to an HTML comment
// saying the content was omitted, and every deployment silently stops
// formatting. It is the kind of line that reads like a smell and gets removed.
func TestRawHTMLPassesThroughTheMarkdownParser(t *testing.T) {
	t.Parallel()

	msg, err := RenderMessage(
		models.Template{Text: "<p><b>still here</b></p>"},
		RenderData{Alert: sampleAlert()},
	)
	if err != nil {
		t.Fatalf("RenderMessage: %v", err)
	}
	if msg.Text != "<p><b>still here</b></p>" {
		t.Fatalf("text = %q, want the html to have passed through untouched", msg.Text)
	}
}
