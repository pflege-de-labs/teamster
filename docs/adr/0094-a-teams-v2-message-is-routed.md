# 0094. A Teams V2 message is routed

* Status: Accepted
* Date: 2026-11-05

## Context

A Teams V2 message went straight to its endpoint's destination and ran no routing
([ADR 0030](0030-teams-v2-compatible-webhooks.md)). The label `teamster_source=teamsv2` existed for
templates only ([ADR 0052](0052-the-receiving-webhook-is-a-label.md)), so no route could deliver
such a message to a second channel, and a webhook's routing was invisible on the routes page.

An untouched endpoint must keep behaving as it did, and the previous release must still run against
the new schema, because migrations run before the new pods do.

## Decision

We will route a message posted to `/teamsv2/{team}/{channel}/{token}` through regular label routing.
The event carries the labels `teamster_team` and `teamster_channel`, the URL slugs, next to
`teamster_source=teamsv2`. All three are stripped from a sender's labels on every other webhook, so
only the URL can set them.

Every endpoint has one linked route, `routes.webhook_endpoint_id`, a column with a partial unique
index and no foreign key. It is created, synced and deleted with the endpoint in one transaction.
Its name is `webhook <team>/<channel>`, priority 0, not greedy, with the selector
`teamster_source=teamsv2, teamster_team=<team>, teamster_channel=<channel>`, and the endpoint's
destination and template. The routes page and API lock its selector, target, template, parent and
default flag; name, priority and greedy stay editable. It cannot be deleted on its own: "managed by
webhook <team>/<channel>; delete the webhook instead". Child routes and independent routes on the
same labels follow the normal rules. An endpoint with child routes under its route cannot be
deleted; the API answers 400. An endpoint update renames the route only while it still has the
default name.

An endpoint without a linked route, as the previous release creates, posts straight to its own
destination. A startup reconcile backfills routes for existing endpoints, copying the `own`
permissions (shared grants are not copied), and deletes a linked route whose endpoint is gone, or
unlinks it when it has children. Concurrent replicas insert plainly: the loser's transaction rolls
back on the unique index, logs a warning and startup continues.

Alternatives:

* **Endpoint as a synthetic root**, like `routing.GlobalDefaultRouteID`. No schema change, but it is
  not a regular route: nothing hangs under it and it is not listed.
* **`webhook_endpoints.route_id`.** Equivalent, but listing routes needs a reverse lookup to say
  which endpoint manages one.

## Consequences

* Behaviour changes for operators:
  * A message fans out to every matching root route ([ADR 0034](0034-routes-form-a-tree.md)).
  * One failing delivery answers 502, and a sender retry may duplicate the deliveries that
    succeeded, as on `/webhook/universal`.
  * The routed success body is the universal webhook's report instead of `{"status":"ok"}`; the
    fallback path keeps `{"status":"ok"}`.
  * The delivery metric label is the route label instead of `teamsv2:<team>/<channel>` on the routed
    path.
  * Existing routes selecting `teamster_source=teamsv2`, which never fired, start firing.
* Linked routes and their descendants are left out of export bundles, like endpoints, so child
  routes under a linked route do not travel. Import refuses a bundle route whose id is a managed
  route, and replace-import never deletes managed routes.
* A replace import that deletes a destination or template still referenced by an endpoint or its
  linked route leaves a dangling reference. That already held for endpoints and now holds for
  their routes too.
* The "no template" hint for a text-only message still links to the templates panel, not the
  endpoint form.
* Schema: one additive migration per dialect. The previous release ignores the column and may
  delete or edit a linked route; the fallback and the reconcile cover both.
