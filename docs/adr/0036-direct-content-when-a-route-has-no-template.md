# 0036. A route with no template sends the payload's own title, text and card

* Status: Superseded by [0039](0039-built-in-default-message.md) in part: an empty payload is
  no longer an error
* Date: 2026-09-21

## Context

`/webhook/universal` exists for a sender that is not Alertmanager. Until now it still needed a
route with a `TemplateID`: routing picks the destination from labels, but rendering always went
through a stored Adaptive Card template, and `ValidateRoute` already allowed a root route to be
saved without one — a legal configuration that could never actually deliver anything, because
`renderMessage` called `store.GetTemplate` unconditionally and failed when `TemplateID` was empty.

A sender that already knows what it wants to say — a CI job posting a build result, a script
posting a one-line status — gains nothing from writing a template and a lot from not having to.
The Alertmanager-shaped annotation dance (`ADR 0010`, superseded by `ADR 0028`) is exactly the
unwieldiness this endpoint's own payload was meant to avoid.

## Decision

We will let `UniversalWebhookPayload` carry `title`, `text` and `card` directly, and let
`models.Alert` carry the same three fields end to end. `renderMessage` decides between them and a
stored template by whether the delivery's route names one:

* **A route with a `TemplateID` always renders through the template**, exactly as before. The
  payload's `title`/`text`/`card` are ignored for that delivery. A route is a deliberate choice by
  whoever owns it; a sender's payload must not be able to blank out a working template by
  accident, which is what "the payload wins when both are present" would risk.
* **A route with no `TemplateID` sends the payload's `title`/`text`/`card` as given.** If none of
  the three are set, the delivery fails with a clear error rather than posting an empty message —
  the same posture `directMessage` already takes for `Text`.

**`text` still goes through `templates.RenderText`**, the same Markdown-to-sanitized-HTML pipeline
a stored template's text goes through (`ADR 0029`). Sanitization is a property of the transport, not
of who authored the string; a sender not using a template does not get to skip the trust boundary
that protects the Teams client rendering it.

**`card` is passed through after only a JSON-validity check**, the same leniency a template's
rendered `Body` already gets — teamster does not validate Adaptive Card schema anywhere, and a
direct-content payload does not need a stricter rule than a template does.

**The field is `card`, singular, not `cards`.** `graph.Message.Cards` is a slice because a Teams V2
payload (`ADR 0030`) forwards an externally-composed multi-attachment message verbatim, with no
routing or rendering step to decide otherwise. A universal webhook message still goes through one
route to one template-shaped `templates.Message`, which has only ever carried one `Card`; widening
that struct for a use nothing here has yet would touch `bot.Message`, `teamsv2`'s conversion path
and the card preview UI for a capability this feature does not need. Multi-attachment fan-out
through a routed, templated path is a wider change than "let a template-less route send
something," and is left for if a sender ever asks for it.

**The wire field is named `title`, not `subject`.** `templates.Message.Title`, `graph.Message.Title`
and `Template.Title` already use `Title` throughout; introducing `subject` on the wire would be a
second name for the same concept with no reader benefit.

**The admin UI's route form gets a "— none —" template option.** A `<select>` always submits some
value, so without an explicit empty choice a route could never actually be saved without a
template through the UI — the configuration `ValidateRoute` already permitted was reachable only
through the API.

## Consequences

A sender can use `/webhook/universal` without ever creating a template, as long as the route it
matches has none configured either. A route that mixes the two — some deliveries templated, some
not — is not possible per route, only per route-across-different-routes, which matches how a
route's `TemplateID` already works everywhere else (inherited by children, one value per route).

The schema is untouched. `models.Alert` and `UniversalWebhookPayload` grow three optional fields,
additively, so the previous release still decodes a payload that carries them — it simply never
reads the new fields, and a route without a template still fails to render on that release exactly
as it does today.

Not decided here: whether `/webhook/alertmanager` should ever accept direct content. Alertmanager
alerts are Alertmanager-shaped by construction and always carry annotations for a template to
read; nothing about that path asked for this.
