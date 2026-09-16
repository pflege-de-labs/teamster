package templates

import (
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// markdown renders a template's authored text to HTML on its way to Sanitize.
//
// html.WithUnsafe passes raw HTML through instead of stripping it, which is
// what keeps every template written before this change working: Text was
// documented as HTML, so the installed base is HTML, and it survives the
// parser untouched. That option is only safe because Sanitize runs after this
// and is the trust boundary -- see docs/adr/0029-templates-are-markdown.md.
// Removing it does not make anything safer; it silently empties every deployed
// HTML template.
//
// A goldmark.Markdown is immutable once built and safe for concurrent use, so
// this is shared rather than rebuilt per render.
var markdown = goldmark.New(goldmark.WithRendererOptions(goldmarkhtml.WithUnsafe()))

// renderMarkdown turns authored text into the HTML Sanitize expects. The
// output is trimmed because goldmark terminates every block with a newline,
// which would otherwise arrive as a trailing text node in the message.
func renderMarkdown(source string) (string, error) {
	if strings.TrimSpace(source) == "" {
		return "", nil
	}
	var out strings.Builder
	if err := markdown.Convert([]byte(source), &out); err != nil {
		return "", fmt.Errorf("render markdown: %w", err)
	}
	return strings.TrimSpace(out.String()), nil
}

// ToMarkdown converts sanitized HTML back into the Markdown subset Bot
// Framework renders in a chat, where graph's HTML would show up as literal
// tags.
//
// It reads the sanitized HTML rather than the template source on purpose: the
// alert data interpolated into a template comes from whoever can POST a
// webhook, and emitting from the source would give that data a path around the
// one boundary that checks it. Its input vocabulary is therefore closed --
// exactly the tags Sanitize admits -- and every one of them has an exact
// Markdown form except u, which the subset cannot express and which degrades
// to its text.
func ToMarkdown(fragment string) (string, error) {
	if strings.TrimSpace(fragment) == "" {
		return "", nil
	}

	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return "", fmt.Errorf("parse sanitized text: %w", err)
	}

	var e mdEmitter
	e.walk(nodes)
	return e.String(), nil
}

// mdEmitter accumulates whole blocks rather than writing as it goes, because a
// blockquote and a list item both have to prefix every line of what their
// children produced, which is only knowable once those children are done.
type mdEmitter struct {
	blocks []string
}

func (e *mdEmitter) String() string { return strings.Join(e.blocks, "\n\n") }

func (e *mdEmitter) block(s string) {
	if s = strings.Trim(s, " \t\n"); s != "" {
		e.blocks = append(e.blocks, s)
	}
}

// blockTags are the elements that start a new block. Everything else Sanitize
// admits is inline, and a run of inline siblings becomes one block together --
// splitting them would put a blank line inside a sentence.
var blockTags = map[atom.Atom]bool{
	atom.P: true, atom.Pre: true, atom.Blockquote: true,
	atom.Ul: true, atom.Ol: true,
	atom.H1: true, atom.H2: true, atom.H3: true,
}

func (e *mdEmitter) walk(nodes []*html.Node) {
	var run strings.Builder
	flush := func() {
		e.block(run.String())
		run.Reset()
	}
	for _, node := range nodes {
		if node.Type == html.ElementNode && blockTags[node.DataAtom] {
			flush()
			e.blockNode(node)
			continue
		}
		run.WriteString(e.inlineNode(node))
	}
	flush()
}

func (e *mdEmitter) children(node *html.Node) []*html.Node {
	var out []*html.Node
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		out = append(out, child)
	}
	return out
}

// sub renders a node's children as blocks of their own, which is what a
// blockquote and a list item need before they can indent the result.
func (e *mdEmitter) sub(node *html.Node) string {
	var child mdEmitter
	child.walk(e.children(node))
	return child.String()
}

func (e *mdEmitter) blockNode(node *html.Node) {
	switch node.DataAtom {
	case atom.H1:
		e.block("# " + e.inline(node))
	case atom.H2:
		e.block("## " + e.inline(node))
	case atom.H3:
		e.block("### " + e.inline(node))
	case atom.Pre:
		e.block(fencedCode(rawText(node)))
	case atom.Blockquote:
		e.block(prefixLines(e.sub(node), "> "))
	case atom.Ul:
		e.block(e.list(node, false))
	case atom.Ol:
		e.block(e.list(node, true))
	default:
		e.block(e.inline(node))
	}
}

func (e *mdEmitter) list(node *html.Node, ordered bool) string {
	var items []string
	number := 1
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.DataAtom != atom.Li {
			continue
		}
		marker := "- "
		if ordered {
			marker = fmt.Sprintf("%d. ", number)
			number++
		}
		body := e.sub(child)
		if body == "" {
			continue
		}
		// Continuation lines line up under the marker, so a nested list or a
		// second paragraph stays inside the item rather than ending it.
		items = append(items, marker+strings.TrimPrefix(prefixLines(body, strings.Repeat(" ", len(marker))), strings.Repeat(" ", len(marker))))
	}
	return strings.Join(items, "\n")
}

func (e *mdEmitter) inline(node *html.Node) string {
	var b strings.Builder
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(e.inlineNode(child))
	}
	return b.String()
}

func (e *mdEmitter) inlineNode(node *html.Node) string {
	switch node.Type {
	case html.TextNode:
		return escapeMarkdown(node.Data)
	case html.ElementNode:
	default:
		return ""
	}

	switch node.DataAtom {
	case atom.Br:
		// Two spaces before the newline is CommonMark's hard break; a bare
		// newline would be folded back into the surrounding line.
		return "  \n"
	case atom.B, atom.Strong:
		return emphasize("**", e.inline(node))
	case atom.I, atom.Em:
		return emphasize("_", e.inline(node))
	case atom.S:
		return emphasize("~~", e.inline(node))
	case atom.Code:
		return inlineCode(rawText(node))
	case atom.A:
		return mdLink(node, e.inline(node))
	case atom.U:
		// Bot Framework's Markdown subset has no underline. The text survives,
		// the emphasis does not, which is the same bargain Sanitize already
		// makes with markup Teams cannot show.
		return e.inline(node)
	default:
		return e.inline(node)
	}
}

// markdownMetachars are the ASCII punctuation CommonMark gives meaning to.
// Escaping all of them rather than guessing which are load-bearing in context
// is what makes alert data inert here, and matches what bot.escapeMarkdown
// already does to a title. Teams renders an escaped character as itself.
const markdownMetachars = "\\`*_{}[]()#+-.!|~<>"

func escapeMarkdown(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 && strings.ContainsRune(markdownMetachars, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// emphasize wraps content in a marker, keeping any surrounding whitespace
// outside it: "** bold **" is not emphasis in CommonMark, it is literal
// asterisks.
func emphasize(marker, content string) string {
	trimmed := strings.Trim(content, " \t\n")
	if trimmed == "" {
		return content
	}
	lead := content[:strings.Index(content, trimmed)]
	trail := content[len(lead)+len(trimmed):]
	return lead + marker + trimmed + marker + trail
}

// inlineCode picks a fence longer than the longest backtick run in the code,
// which is what lets code containing backticks survive.
func inlineCode(code string) string {
	if strings.Trim(code, " \t\n") == "" {
		return ""
	}
	fence := "`"
	for strings.Contains(code, fence) {
		fence += "`"
	}
	pad := ""
	if strings.HasPrefix(code, "`") || strings.HasSuffix(code, "`") {
		pad = " "
	}
	return fence + pad + code + pad + fence
}

func fencedCode(code string) string {
	code = strings.Trim(code, "\n")
	if strings.TrimSpace(code) == "" {
		return ""
	}
	fence := "```"
	for _, line := range strings.Split(code, "\n") {
		for strings.HasPrefix(strings.TrimSpace(line), fence) {
			fence += "`"
		}
	}
	return fence + "\n" + code + "\n" + fence
}

// mdLink emits the link form. A destination carrying parentheses needs the
// angle-bracket form, because the plain one ends at the first ")". Sanitize
// has already url-escaped everything else that would need it.
func mdLink(node *html.Node, text string) string {
	href := ""
	for _, attr := range node.Attr {
		if attr.Key == "href" {
			href = attr.Val
			break
		}
	}
	if href == "" {
		return text
	}
	if strings.ContainsAny(href, "()<> ") {
		href = "<" + strings.NewReplacer("<", "%3C", ">", "%3E", " ", "%20").Replace(href) + ">"
	}
	if strings.Trim(text, " \t\n") == "" {
		text = escapeMarkdown(strings.Trim(href, "<>"))
	}
	return "[" + text + "](" + href + ")"
}

// rawText is the text of a subtree with no escaping, for the two places where
// the content is code and Markdown means nothing inside it.
func rawText(node *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return b.String()
}

func prefixLines(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		if line == "" {
			// A blank line inside a blockquote still has to carry the marker,
			// or the quote ends there.
			lines[i] = strings.TrimRight(prefix, " ")
			continue
		}
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}
