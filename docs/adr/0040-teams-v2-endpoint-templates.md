# 0040. A Teams V2 endpoint may name a template

* Status: Accepted
* Date: 2026-09-23

## Context

[ADR 0030](0030-teams-v2-compatible-webhooks.md) forwards what a Teams V2 sender posts without
routing it or rendering it. That keeps a sender's migration to a URL change. It also leaves the
channel with whatever the sender's card looks like, and a converted MessageCard loses some of its
styling. Every other message teamster sends can be shaped by a template.

[ADR 0039](0039-built-in-default-message.md) puts a hint card under every message sent without a
template. For that hint to help on this path, the reader has to be able to act on it.

## Decision

We will let an endpoint name a template, and keep everything else about ADR 0030:

* **Stored on the endpoint.** `webhook_endpoints.template_id` is empty by default. Saving an
  endpoint that names a template which does not exist is refused, in the form and in the API.
* **With a template.** The message renders through `templates.RenderMessage`. `.Alert` carries the
  parsed message: `Source` is `teamsv2`, `Title` and `Text` hold the sanitized HTML, and `Card`
  holds the first card. The new `RenderData.Payload` field holds the raw body as decoded JSON, so a
  template can reach MessageCard fields such as `themeColor` or `sections` that the parsed form
  flattens. A missing template or a render failure answers `502`, just as a Graph failure does.
* **Without a template.** The payload is sent exactly as before, and the ADR 0039 hint card is
  appended after the payload's own cards. Its link opens this endpoint's form
  (`/admin?edit=webhooks&id=…`), which is where its template is chosen.
* **Preview.** The template preview gets a Teams V2 sample, so a template meant for an endpoint
  can be tried against both `.Alert` and `.Payload`.

Still no routing and no tracking. The URL decides where the message goes, and there is no status to
follow.

## Consequences

This amends ADR 0030's "renders no template". A sender that never wants the hint card has to be
given a template, even one that only passes the payload's card through.

The routing page's template graph shows only routes. A template that only endpoints use therefore
looks unused there. Deleting it makes those endpoints answer `502`, just as deleting a template a
route uses makes that route fail.

The schema change is additive. The previous release ignores the column, and endpoints it creates
have no template.
