# Teams V2 messages are routed — design

* Date: 2026-11-05
* Status: Approved in chat, awaiting spec review
* ADR: 0094 (to be written with the implementation)

## Goal

A message posted to `/teamsv2/{team}/{channel}/{token}` goes through regular label routing instead
of bypassing it. Creating a webhook endpoint creates a route that delivers to the endpoint's
destination, so the behaviour of an untouched endpoint does not change. Further routes can select
the same message and deliver it to additional destinations.

## Decisions taken

| Question | Decision |
| ---------- | ---------- |
| How the endpoint and its route relate | Linked and synced: the endpoint form writes destination and template through to its route |
| Label names | `teamster_team`, `teamster_channel` (URL slugs) |
| What the routes page may do with a linked route | Selector, target and template locked; no delete on its own; children and independent routes on the same labels allowed |
| Where the link lives | `routes.webhook_endpoint_id` (approach A) |

Rejected approaches:

* **Endpoint as a synthetic root** (like `routing.GlobalDefaultRouteID`): no schema change, but
  not a regular route — nothing can hang under it and it is not listed.
* **`webhook_endpoints.route_id`**: equivalent, but the route list then needs a reverse lookup to
  say which endpoint manages a route.

## Message flow

`handleTeamsV2` keeps the path, token check, size limit and `teamsv2.Parse`, then builds an event
and calls `processEvent`:

* `Source = teamsv2`, `State = none`, so delivery is `deliverOnce` and nothing is tracked
  (ADR 0035/0056 contract unchanged).
* `Labels = {teamster_team: <team slug>, teamster_channel: <channel slug>}`; `processEvent` adds
  `teamster_source = teamsv2` as for every source.
* `Title`, `Text`, `Card` (first card) from the parsed message.
* New `Event.TeamsV2 *TeamsV2Event{Payload any, Cards []json.RawMessage}`.

Rendering:

* `renderMessageFor` passes `ev.TeamsV2.Payload` as `RenderData.Payload`, so endpoint templates
  reading `.Payload` (ADR 0040) keep working.
* `directMessage` sends every card of `ev.TeamsV2.Cards` when no template renders, keeping ADR
  0030's "all attachments are forwarded".
* Precedence is unchanged in effect: route template (the endpoint's, synced) → `teamsv2` source
  default → payload as given plus hint card. On the linked route the hint links to the endpoint
  form (`/admin?edit=webhooks&id=…`); on other routes to the templates panel.

Label trust:

* Next to `models.WithSourceLabel`, `teamster_team` and `teamster_channel` are always removed from a
  sender's labels and set only for teamsv2 events, from the URL.
* The linked route selects `teamster_source=teamsv2, teamster_team=…, teamster_channel=…`, so a
  universal sender cannot reach it.

Fallback: an endpoint without a linked route (created or unlinked by the previous release) is
posted straight to its own destination, as today. Its messages never fall to the default route or
global default by accident.

Behaviour changes to record in the ADR:

* A message fans out to every matching root (ADR 0034).
* One failing delivery answers `502`; a sender retry can duplicate the deliveries that succeeded,
  as on `/webhook/universal`.
* The success body becomes `writeReport`'s instead of `{"status":"ok"}`.
* The delivery metric label becomes `routeLabel(delivery)` instead of `teamsv2:<team>/<channel>`.
* Existing routes selecting `teamster_source=teamsv2`, which never fired, start firing.

## Schema

One additive migration per dialect (`internal/store/migrations/{sqlite,postgres}/`):

* `routes.webhook_endpoint_id TEXT NOT NULL DEFAULT ''`, in the style of `routes.recipient_id`.
* Partial unique index `WHERE webhook_endpoint_id <> ''`: at most one linked route per endpoint.
* No foreign key: SQLite does not enforce them here (no `foreign_keys` pragma), so orphans are
  handled by the startup reconcile instead.

The previous release ignores the column; routes it creates get `''`. It may delete or edit a linked
route; the fallback and the reconcile cover both.

`models.Route.WebhookEndpointID` is output only in the JSON API, ignored on input.

## Endpoint writes

Form (`webhooks_admin.go`) and API (`webhooks_api.go`) share one helper per operation.

* **Create**: endpoint and linked route in one transaction; the creator owns both (`grantOwner`).
  Route: name `webhook <team>/<channel>`, priority 0, not greedy, not default, selector as above,
  destination and template from the endpoint.
* **Update**: in the same transaction, rewrite the linked route's selector (slugs), destination and
  template. An endpoint without a linked route gets one.
* **Delete**: refused with `routing.ValidateDelete`'s message when the linked route has children.
  Otherwise endpoint, route and the permissions of both are deleted in one transaction.

## Route writes

Enforced in `saveRouteChecked` and `deleteRouteChecked`, which serve form and API alike.

* **Update of a linked route**: refused when it changes the selector, the target, the template, the
  parent or the default flag. Name, priority and greedy stay editable.
* **Create**: cannot set `webhook_endpoint_id`.
* **Delete of a linked route**: refused — "managed by webhook <team>/<channel>; delete the webhook
  instead".
* Children of a linked route and independent roots on the same labels follow the normal rules.

UI: the route form shows the locked fields read-only with a "Managed by webhook …" link to the
endpoint; the endpoint form links to its route. New strings in `en.json` and `de.json`.

## Startup reconcile

Runs on every start, idempotent; replicas starting together are safe through the unique index and
a plain insert: the losing replica rolls back on the unique index, logs a warning and carries on.
(Corrected during implementation; the first draft said the insert does nothing on conflict.)

1. Every endpoint without a linked route gets one, owned by the principals holding `own` on the
   endpoint (permissions copied). This is the backfill for existing installations and the repair
   after the previous release deleted a route.
2. A linked route whose endpoint is gone is deleted, or unlinked (`''`) and kept as a plain route
   when it has children.
3. A linked route the previous release edited gets its locked fields back; name, priority and
   greedy are kept. (Added in the final review.)

## Export and import

* Linked routes and their descendants are left out of the bundle, like endpoints (ADR 0030). Their
  children therefore do not travel; the ADR records this.
* Replace-mode import leaves linked routes and their subtrees in place.
* An imported route never carries a link.

## Testing

Table-driven, `t.Parallel()`:

* Handler: linked route only; linked plus independent route; child route; fallback without a
  route; forged `teamster_team` on universal ignored; `.Payload` reaches a template; several cards
  forwarded without a template.
* Route write paths: lock refusals per field, editable fields accepted, delete refused.
* Endpoint write paths: route created with owner, synced on update, delete refused with children.
* Store conformance for the new column and index in both dialects.
* Reconcile: backfill, repair, orphan deleted, orphan with children unlinked, two runs equal one.
* Transfer: linked routes and subtrees excluded on export, kept on replace import.

Coverage stays at or above 75 % (`make coverage`).

## Documentation

* ADR 0094 "A Teams V2 message is routed", amending 0030 (runs no routing), 0052 (Teams V2 stays
  unrouted) and 0055 (Teams V2 precedence bullet); status lines of those three updated.
* `docs/architecture.md`, `README.md`, `CHANGELOG.md` under `Unreleased`.
* User docs PR against `pages`, English and German, via the docs-writer and docs-translator agents,
  linked both ways with the code PR.
