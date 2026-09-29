# 0050. The catch-all route's template is a setting

* Status: Accepted
* Date: 2026-09-29

## Context

The global default route ([ADR 0038](0038-global-default-destination.md)) is built in, not stored.
It always renders without a template: the payload's own content, or the built-in default message
([ADR 0039](0039-built-in-default-message.md)). The built-in message dumps the alert as JSON and
adds a "no template" hint. An installation that relies on the catch-all cannot make it look like
any other alert.

The choice needs a home that:

* survives an admin making a different destination the default
* is editable in the admin UI
* travels with a configuration bundle

## Decision

We will store the catch-all's template in a new key/value `settings` table, under the key
`global_default.template_id`. Migrations are `0017` in SQLite and `0014` in Postgres.

* No row means the built-in default message, which is what every release before this one sends.
* `Store.GetGlobalDefaultTemplate` and `SetGlobalDefaultTemplate` wrap the key. Setting an unknown
  template is `ErrNotFound`, and setting `""` deletes the row.
* `DeleteTemplate` clears the key in the same transaction when it names the deleted template. The
  catch-all then falls back to the built-in message instead of failing every message it catches.
* `routing.Plan` puts the template id on the synthetic global default delivery.
* The global default row in the Routes panel has a template select, posting to
  `/admin/routes/global-default`. The API is `GET`/`PUT /api/routes/global-default` with
  `{"template_id": "…"}`.
* Choosing the template is a route edit (`edit` on `Route`), unlike choosing the default
  destination, which is the admin's. It changes how unclaimed messages look, not where they go.
* The bundle carries `global_default_template_id`. It must name a template the bundle carries. A
  merge keeps the installation's choice when the bundle names none; a replace applies the bundle's,
  including the built-in one.

Alternatives considered:

* **A column on `destinations`.** Switching the default destination would silently switch the
  template with it.
* **A config file key.** It could not be edited in the UI, and it would name a template by an id or
  a name that the database can change underneath it.

## Consequences

* Additive schema: the previous release ignores the new table and keeps sending the built-in
  message.
* The bundle format stays at version 1, and the new field is optional. An older build importing a
  newer bundle ignores the field.
* `settings` is where the next single installation-wide value goes, rather than a new table each
  time.
