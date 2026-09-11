package templates

import (
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// A template renders into a Teams message, and its data comes from whoever sent
// the alert. Rendered markup is therefore reduced to the formatting Teams
// understands: anything else is either unwrapped to its text or dropped, so a
// crafted annotation cannot put a link, an image or a script into a channel.
var allowedTags = map[atom.Atom]bool{
	atom.P: true, atom.Br: true, atom.B: true, atom.Strong: true,
	atom.I: true, atom.Em: true, atom.U: true, atom.S: true,
	atom.Code: true, atom.Pre: true, atom.Blockquote: true,
	atom.Ul: true, atom.Ol: true, atom.Li: true,
	atom.H1: true, atom.H2: true, atom.H3: true,
	atom.A: true,
}

// Elements whose content is not text to show. Unwrapping these would spill
// script source or stylesheet rules into the message.
var droppedTags = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Iframe: true,
	atom.Object: true, atom.Embed: true, atom.Template: true,
	atom.Noscript: true, atom.Title: true,
}

var voidTags = map[atom.Atom]bool{atom.Br: true}

// Sanitize parses rendered output as an HTML fragment and writes back only the
// allowed elements. Text is escaped, so output is safe to send even when the
// input was not HTML at all.
func Sanitize(fragment string) (string, error) {
	if strings.TrimSpace(fragment) == "" {
		return "", nil
	}

	context := &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return "", fmt.Errorf("parse rendered text: %w", err)
	}

	var out strings.Builder
	for _, node := range nodes {
		writeNode(&out, node)
	}
	return out.String(), nil
}

func writeNode(out *strings.Builder, node *html.Node) {
	switch node.Type {
	case html.TextNode:
		out.WriteString(html.EscapeString(node.Data))
		return
	case html.ElementNode:
		// An unknown element keeps its text: dropping the content of a <div>
		// would lose the message rather than the markup.
		if droppedTags[node.DataAtom] {
			return
		}
		if !allowedTags[node.DataAtom] {
			writeChildren(out, node)
			return
		}
	default:
		// Comments and doctypes carry nothing a reader needs.
		if node.Type != html.DocumentNode {
			return
		}
	}

	if node.Type == html.DocumentNode {
		writeChildren(out, node)
		return
	}

	tag := node.Data
	out.WriteString("<" + tag)
	if node.DataAtom == atom.A {
		if href, ok := safeHref(node); ok {
			out.WriteString(` href="` + html.EscapeString(href) + `" rel="noopener noreferrer"`)
		}
	}
	out.WriteString(">")

	if voidTags[node.DataAtom] {
		return
	}

	writeChildren(out, node)
	out.WriteString("</" + tag + ">")
}

func writeChildren(out *strings.Builder, node *html.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeNode(out, child)
	}
}

// Only schemes that open something a reader expects. javascript: and data:
// links are the reason this is an allowlist.
func safeHref(node *html.Node) (string, bool) {
	for _, attr := range node.Attr {
		if attr.Key != "href" {
			continue
		}
		parsed, err := url.Parse(strings.TrimSpace(attr.Val))
		if err != nil {
			return "", false
		}
		switch strings.ToLower(parsed.Scheme) {
		case "http", "https", "mailto":
			return parsed.String(), true
		}
		return "", false
	}
	return "", false
}
