# 0010. Let a template decide the message, not just the card

* Status: Accepted
* Date: 2026-09-11

## Context

Every message this service sent was a card with a one-line summary in front of it, and that summary
was decided in Go: `annotations.summary`, then `labels.alertname`, then the literal `Alert update`.

Microsoft Teams previews a message in the activity feed by its body. A body whose visible content
is an attached card previews as `Card`, so a reader scanning notifications sees a column of
identical entries and has to open each one to learn what fired. That is most of the value of a
notification, lost to a formatting detail.

Two things follow. The line the feed shows has to be the template's to write, because only the
deployment knows what its operators want to read. And a card is not always the right shape: an
alert that is one sentence does not need a card at all, and a text message previews itself.

## Decision

We will make a template three optional parts instead of one required one:

* `Title` — a Go template rendering to a single line of plain text, escaped and collapsed to one
  line. It becomes the first thing in the message body, which is what the feed previews.
* `Text` — a Go template rendering to formatted text, for a message that wants prose.
* `Body` — the Adaptive Card JSON, now optional.

A template needs at least one of the three; `templates.Validate` rejects the rest on both write
paths. `templates.RenderMessage` renders all three and returns a `Message`, and the Graph client
assembles the body from it, referencing the attachment explicitly with `<attachment id="1">` so the
card sits below the text rather than wherever Graph decides to put it.

A card with no title keeps the summary line the service always sent: `templates.DefaultTitle` is
the old fallback chain written as a template. Text without a title is left alone, because the feed
previews the text itself and a generated title would only repeat it.

**Rendered text is sanitized before it leaves the service.** `Text` renders to HTML, and its inputs
include the alert — so an annotation is an injection vector into a Teams channel. Output is parsed
with `golang.org/x/net/html` and written back through an allowlist of the formatting elements Teams
supports; `script` and `style` are dropped with their contents, unknown elements are unwrapped to
their text, every attribute is dropped except a link's `href`, and an `href` survives only for
`http`, `https` and `mailto`. The alternative — a third-party sanitizer — was rejected because
`x/net/html` is already in the module graph and the allowlist is small enough to read in one sitting
and test exhaustively.

The preview endpoint returns the whole message, and the preview pane leads with the feed line. The
rendered text is shown in a `sandbox`ed iframe rather than injected into the admin page: the
sanitizer is the first lock and the sandbox is the second.

## Consequences

Operators get a feed that says what fired, and can send plain messages where a card was overkill.
The summary line stops being a Go decision, so changing it no longer needs a release.

The `templates` table gains `title` and `message_text`. `CREATE TABLE IF NOT EXISTS` leaves an
existing table alone, so the store now also applies a short list of added columns on open. A
database written by an older version is migrated in place on the next start, and its templates keep
rendering exactly as before, because an untitled card falls back to the old summary line.

The `messenger` interface changes shape — `PostMessage(teamID, channelID string, msg graph.Message)`
— which [ADR 0002](0002-messenger-interface.md) anticipated as the reason for having the interface
at all.

The `default` template helper now takes `any` rather than a `string`, because a missing key of a nil
map arrives as an invalid value that a `string` parameter rejects outright: the exact case a
fallback exists for. Templates that already used it keep working.

Sanitizing is lossy by design. A template author who writes markup Teams does not support sees it
disappear rather than arrive broken, and the preview shows exactly what will be sent.
