# 0055. Each webhook has a default template

* Status: Accepted
* Date: 2026-09-29

## Context

A message that no template renders gets the built-in default: a title, the status and the alert as
fenced JSON, followed by a hint card ([ADR 0039](0039-built-in-default-message.md)). A Teams V2
payload without an endpoint template goes out as given. The only template-level default was the
catch-all route's ([ADR 0050](0050-catch-all-template-is-a-setting.md)), and it does not vary by
webhook.

The three webhooks deliver different shapes ([ADR 0053](0053-templates-name-the-webhooks-they-handle.md)).
So a sensible default depends on which webhook sent the message, and every route that wants a
readable message needs a template for it. The editor's only starting point was one example card
(`cards.Starter`) that fits none of the three in particular.

## Decision

We will give each webhook source a default template, and ship a preset for each.

* **Presets:** `cards.Presets()` holds one whole template per source: title, text, card and
  `sources`. They are Go constants, so a test renders each against its own payloads, and the Teams
  V2 one against the `samples/`.
  * **Alertmanager:** a status-coloured header, labels as facts, start and end times, and actions
    for `generatorURL` and a `runbook_url` annotation.
  * **Universal:** the sender's own card or text when there is one, otherwise the same alert card.
  * **Teams V2:** the parsed card, or text alone.
* **Defaults** are settings keys `default_template.<source>` in the settings table from ADR 0050.
  * There is no migration.
  * Setting a default requires a template that handles the source.
  * Deleting a template clears every default that names it.
* **Precedence at delivery:**
  * **Routed messages:** the route's own template, when it handles the source. Otherwise the
    source's default, then the payload's own content or the built-in message. The catch-all's
    template counts as the route's own.
  * **Teams V2:** the endpoint's template, then the `teamsv2` default, then the payload as given.
  * The hint card is only added when neither a route template nor a source default exists.
  * A default that has since stopped handling its source is ignored, as a mismatched route
    template is.
* **Seeding:** at start-up, `Store.SeedTemplates` creates the three presets and makes each the
  default of its source where none is set.
  * It runs once per installation. The first step claims the `presets.seeded` key with an insert
    that does nothing on conflict, so of several replicas starting together only one seeds.
  * It runs in one transaction, so a failed seed is retried on the next start.
  * A preset the operator deletes stays deleted.
* **An empty card:** a body that renders to nothing now means "no card" instead of failing. That is
  what lets one template carry a card for some payloads and text alone for others.
* **UI:** the template editor gets a "Start from a preset" picker. The templates panel gets one
  default select per source, offering only templates that handle it, and the list marks which
  template is whose default.
* **API:** `GET`/`PUT /api/templates/source-defaults`, and export/import carry
  `source_default_template_ids`.

Alternatives considered:

* **Defaults per delivery kind (channel or chat).** The payload shape is what makes a template fit
  or not; the transport already adapts a rendered message to a chat.
* **Presets seeded by a migration.** A migration cannot be skipped by an operator who does not want
  them, and templates are data an operator owns, not schema.
* **Presets only in the editor, no seeding.** Every new installation would still start with JSON
  dumps until somebody set defaults by hand.

## Consequences

* A new installation sends readable messages from every webhook before any template is written.
* An existing installation is seeded on its first start with this release. Messages from routes
  without a template stop showing the built-in message and hint, and use the preset instead.
  Deleting the preset, or clearing the default, brings the old behaviour back.
* A universal payload that brings its own title, text or card is now rendered by the universal
  default when its route has no template. The preset passes that content through, so it looks the
  same unless the default is changed.
