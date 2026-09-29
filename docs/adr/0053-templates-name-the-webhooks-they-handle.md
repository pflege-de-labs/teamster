# 0053. Templates name the webhooks they handle

* Status: Accepted
* Date: 2026-09-29

## Context

The three webhooks deliver different payload shapes:

* **Alertmanager:** labels, annotations, a status and a lifecycle.
* **Universal:** the same fields, plus the sender's own title, text and card.
* **Teams V2:** a MessageCard or an Adaptive Card that a template reads through `.Payload`
  ([ADR 0040](0040-teams-v2-endpoint-templates.md)).

A template written against one shape renders another badly or not at all. Every template picker
offered every template, whichever webhook would feed it.
[ADR 0052](0052-the-receiving-webhook-is-a-label.md) made the webhook a label, so a route can say
which one it receives from.

## Decision

We will let a template name the webhooks it handles.

* **Stored as** `templates.sources`: comma separated, from `alertmanager`, `universal` and
  `teamsv2`. Empty means any.
  * Migrations are SQLite `0018` and Postgres `0015`, both additive. The default `''` keeps every
    existing template, and every write by the previous release, as "any".
  * `models.NormalizeSources` orders the list and refuses an unknown value. `templates.Validate`
    and the store both apply it.
* **Pickers:**
  * The Teams V2 endpoint form offers only templates that handle `teamsv2`. Saving an endpoint with
    any other template is refused.
  * The route form's options carry `data-sources`. `sources.js` hides the templates that do not
    handle the `teamster_source` the selector pins.
  * On save, a route whose own selector, or the nearest ancestor's, pins a source must use a
    template that handles it (`routing.PinnedSource`).
  * The global default's picker is not narrowed: it catches both routed webhooks.
* **At delivery:**
  * An alert whose route's template does not handle its source gets the built-in message, and a
    warning is logged.
  * A Teams V2 endpoint whose template has since stopped handling `teamsv2` sends the payload as
    given.
  * Nothing is refused at delivery, because a message should never be lost to a template choice.

Alternatives considered:

* **Rendering anyway,** so sources only filter the UI. A mismatched template would still produce
  empty or broken cards.
* **Refusing at save unless the template covers every source a route can receive.** A route that
  pins no source can receive both routed webhooks, so a source-specific template could only ever be
  used below a pinning route. That is too strict for the common case.

## Consequences

* Templates can be written for one payload shape without breaking another.
* The bundle carries `sources` on each template, and an unknown value is refused on import.
* This must merge after the change that adds SQLite `0017` and Postgres `0014` (`settings`, ADR
  0050). goose refuses an older migration that turns up after a newer one has run.
