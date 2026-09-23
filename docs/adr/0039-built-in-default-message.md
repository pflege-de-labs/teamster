# 0039. Send a built-in default message when nothing says what a message looks like

* Status: Accepted
* Date: 2026-09-23

## Context

[ADR 0036](0036-direct-content-when-a-route-has-no-template.md) lets a route without a template
send the payload's own `title`, `text` and `card`. When the payload carries none of them, the
delivery fails. An Alertmanager alert never carries them, so it always fails on a route without a
template, and so does anything the global default destination catches
([ADR 0038](0038-global-default-destination.md)), which never has a template.

Readers of a message that went out without a template also get no sign that a template is missing,
and nothing tells them where to create one.

## Decision

We will never fail a delivery for lack of a template.

* **The built-in default.** When a route has no template and the payload has no title, text or
  card, `templates.Default` renders the message:
  * the title from `summary`, then `alertname`, then "Alert update", the same fallback as
    `DefaultTitle`;
  * the status and the `description` annotation;
  * the alert itself as a fenced JSON block.

  The text goes through `RenderText`, like every other text, so it passes the same sanitizer. The
  fence is always longer than any backtick run in the JSON, so a label value cannot close the
  block early.
* **Direct content stays as it is.** A payload that carries a title, text or card is sent as
  ADR 0036 describes.
* **Every message without a template gets a hint.** `templates.Message.Notice` holds a second
  Adaptive Card that says no template is defined. When `server.external-url` is set, the card
  links to `/admin#templates`. The request host would be the wrong URL: delivery has no request,
  and the Alertmanager caller reaches an in-cluster address. When the setting is empty, the card
  says where to go but carries no link.
* **Placement.** A channel gets the hint card after the message's own card. A chat can carry only
  one card, so there the hint becomes the card when the message has none, and a line of Markdown
  otherwise.
* **Language.** The hint is English. It goes to a shared channel, not to one reader whose language
  is known.

This supersedes ADR 0036's rule that a delivery with none of the three fields fails.

## Consequences

Alertmanager alerts deliver on a route without a template, and so does everything the global
default catches. The JSON block is noisy by design, because it is a prompt to write a template.

Senders who deliberately use direct content now see a hint card under each message. The only way
to remove it is a template that renders the same fields, for example `{{ .Alert.Title }}`.

The schema is untouched. `server.external-url` is optional, and a deployment that does not set it
gets hints without links.
