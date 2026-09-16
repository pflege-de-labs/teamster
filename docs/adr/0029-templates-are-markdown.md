# 0029. Author message text as Markdown, sanitize once for both transports

* Status: Accepted
* Date: 2026-09-16
* Supersedes: [0010](0010-message-shape.md)

## Context

[ADR 0010](0010-message-shape.md) made a template three parts and settled that `Text` renders to
HTML, sanitized through a small allowlist before it leaves the service. That held while there was
one transport. Milestone 13 adds a second: [ADR 0026](0026-alerts-in-a-persons-chat.md) lets a route
deliver to a person's chat through the Bot Connector, and `bot.Message.Text` is not HTML — it is Bot
Framework's Markdown subset. A template authored for a channel arrives in a chat as literal `<p>`
and `<b>`.

So the two transports need a common source. Three shapes were available.

**Keep `Text` as HTML and convert to Markdown for the chat path.** Minimal, but it leaves the
authored format defined by the transport that happened to ship first, and HTML is the more awkward
of the two to write by hand for what is usually a sentence and a link.

**Add a second field** — `Text` for HTML, something else for chat. Two fields mean two things to
keep in step, and an operator who updates one and forgets the other gets a message that disagrees
with itself depending on where it lands. Worse, it gives the chat path its own way in: the field
would need its own sanitizing, or it would have none.

**Make the authored format Markdown and render it to HTML.** One authored source, one rendered
output, and the existing sanitizer still standing between them.

The constraint that decides it: `Text` is documented as HTML today (`README.md`), so every template
in every existing deployment is HTML. Nothing shipped in this repository populates `Text` — the
whole installed base is operator-authored — so whatever is chosen has to keep that markup working
without a migration nobody can run on data they cannot see.

## Decision

We will make `Text` **authored as Markdown**, rendered to HTML, and sanitized exactly as before:

```text
template Text (Markdown, authored)
        │  goldmark, raw HTML passthrough enabled
        ▼
      HTML
        │  templates.Sanitize   ← unchanged, still the only trust boundary
        ▼
sanitized HTML, closed 16-tag allowlist
        ├──► graph.Message.Text   (exactly as before)
        └──► templates.ToMarkdown ──► bot.Message.Text
```

`templates.Message.Text` stays post-sanitizer HTML. Only the *authored* format changes, so
`internal/graph` needs no change and neither does the preview: `POST /api/templates/preview` returns
a `templates.Message` and drops `Text` into a sandboxed iframe as HTML, which keeps working.

Three properties follow from composing it this way rather than converting in one direction only.

**Existing templates keep working, in both places.** Raw HTML passthrough is enabled
(`html.WithUnsafe()`), so HTML survives the CommonMark parser untouched, is sanitized exactly as
before for the channel, and — because the chat path emits from the *sanitized HTML* rather than from
the template source — reaches a chat correctly too. No rewrite, no migration, no author retraining.

**The chat path cannot bypass the sanitizer.** Alert data is interpolated into templates and comes
from whoever can POST a webhook. Emitting Markdown from the template source would hand that data an
unchecked path to Bot Framework. Emitting from the sanitized HTML means one boundary protects both
outputs.

**The emitter is bounded.** Its input vocabulary is closed — exactly what `Sanitize` admits: `p br b
strong i em u s code pre blockquote ul ol li h1 h2 h3 a`. Every one has an exact Markdown form
except `u`, which the subset cannot express and which degrades to its text. It is a second tree walk
in the shape `writeNode` already has, in the same package, over `golang.org/x/net/html`.

### Why goldmark, when 0010 rejected a third-party sanitizer

0010 rejected a third-party sanitizer because "`x/net/html` is already in the module graph and the
allowlist is small enough to read in one sitting and test exhaustively". That reasoning is sound and
still holds for the sanitizer — which is why the sanitizer is untouched. It does not carry to a
CommonMark parser: an allowlist walk is a weekend, a Markdown parser is not.

The precedent deserves answering rather than ignoring, because `gomarkdown/markdown` *is* already in
the module graph indirectly — via `mmark`, via the shell-completion library — so promoting it would
add no module at all. We will use **`github.com/yuin/goldmark`** anyway:

* it is tagged, so Renovate can track it; `gomarkdown/markdown` is pseudo-versioned with no tags,
  which is the exact property this project rejected `msbotbuilder-go` for in ADR 0026
* it has no transitive dependencies of its own
* it is CommonMark-compliant, which a pseudo-versioned blackfriday fork does not promise

The trade is honest: one module is added where zero were strictly necessary. Applying the dependency
standard selectively because the free option is convenient would be the wrong call.

`goldmark.WithRendererOptions(html.WithUnsafe())` is required for the passthrough above and is safe
only because `Sanitize` runs afterwards. That reasoning is written where the option is set, and
`TestRawHTMLPassesThroughTheMarkdownParser` fails if it is removed — otherwise it reads like a smell
and somebody deletes it, silently emptying every deployed template.

### Links from alert data are accepted

`sanitize.go` used to claim that "a crafted annotation cannot put a link ... into a channel". That
was already wrong when it was written: `atom.A` is on the allowlist and `safeHref` permits `http`,
`https` and `mailto`, so an annotation containing `<a href>` already produced a link. Markdown only
lowers the bar from writing a tag to writing `[x](https://evil.example)`.

We will **keep `a` on the allowlist and state the position plainly** rather than drop it. Templates
link to runbooks, and that is most of what message text is for; removing links would break existing
templates to close a hole that stays open through the card body anyway. What the allowlist actually
holds back is the schemes that execute rather than navigate — `javascript:`, `data:` — and that is
unchanged.

The consequence is worth naming: anyone who can POST a webhook can put a link, with arbitrary link
text, into a Teams channel or chat. Link text can differ from its destination, which is a phishing
primitive. The webhook endpoint is authenticated by a shared token, so this is an escalation
available to something that can already send alerts, not to the public — but a deployment that
treats its webhook token as low-value should know that it carries this. The misleading comment is
corrected in the same change.

## Consequences

Operators write message text in the format the content actually wants, and one template renders
correctly to a channel and to a chat without being authored twice.

**Existing HTML templates keep rendering, with one visible exception.** Block-level HTML — a
template that formats anything — is a CommonMark HTML block and passes through byte for byte. A
fragment that is *only* inline markup (`<b>bold</b>`, with no block element around it) is not an HTML
block, so it becomes the paragraph it always implied: `<p><b>bold</b></p>`. Teams renders the two
identically. No deployment needs to act.

`Text` was asserted to be HTML in five independent places and all five moved together: the sanitizer's
comment, both locale files (`templates.text_field`), the README, and this record. The transport's
`contentType: "html"` is unchanged and correct — what reaches Graph is still HTML.

The sanitizer is unchanged, and remains the only trust boundary. That is the point of the shape: the
new transport reuses it rather than acquiring one of its own.

`internal/templates` gains a direct dependency on goldmark and a second tree walk beside the
sanitizer. Both are covered by table tests over the closed tag vocabulary, and a round-trip test
asserts that one template reaches both transports correctly.
