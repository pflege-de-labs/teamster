package templates

import (
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// maxSummaryRunes keeps the feed line to what a notification shows anyway.
const maxSummaryRunes = 150

// Summary is the plain-text line the Teams activity feed shows for a message:
// the title, or else the first line of the sanitized HTML text (ADR 0057).
func Summary(title, fragment string) string {
	line := strings.Join(strings.Fields(title), " ")
	if line == "" {
		line = firstLine(fragment)
	}
	runes := []rune(line)
	if len(runes) <= maxSummaryRunes {
		return line
	}
	return strings.TrimSpace(string(runes[:maxSummaryRunes-1])) + "…"
}

// firstLine is the first non-blank line of text in a sanitized fragment, with
// block elements and line breaks ending a line.
func firstLine(fragment string) string {
	if strings.TrimSpace(fragment) == "" {
		return ""
	}
	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return ""
	}

	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
		case html.ElementNode:
			if n.DataAtom == atom.Br {
				b.WriteByte('\n')
				return
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
			if isBlock(n.DataAtom) {
				b.WriteByte('\n')
			}
		}
	}
	for _, n := range nodes {
		walk(n)
	}

	for line := range strings.SplitSeq(b.String(), "\n") {
		if text := strings.Join(strings.Fields(line), " "); text != "" {
			return text
		}
	}
	return ""
}

func isBlock(a atom.Atom) bool {
	switch a {
	case atom.P, atom.Div, atom.Li, atom.Ul, atom.Ol, atom.Pre, atom.Blockquote, atom.Hr,
		atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6, atom.Table, atom.Tr:
		return true
	}
	return false
}
