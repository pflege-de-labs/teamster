# 0076. Webhook permissions, and overviews of who may do what

* Status: Accepted; the page is opened to share holders by [ADR 0088](0088-open-the-access-page-to-whoever-may-share.md)
* Date: 2026-10-01

## Context

Webhook access tokens admit a sender to both webhooks and every route
([ADR 0044](0044-webhook-access-tokens.md)). Users are to mint tokens of their own, scoped to what
they themselves may do (milestone 25.7 in the [roadmap](../roadmap.md)). That needs a permission to
use each webhook, and it has to be assignable later to users, groups, provider groups and roles.

Permissions now live in roles, groups and generated policies
([ADR 0075](0075-own-and-share-records-through-generated-policies.md)), so "why may Bob do this?"
has no single place to look. Admins need the whole picture. Everyone else needs at least their own.

## Decision

* **Webhooks are resources.** `Webhook::"alertmanager"` and `Webhook::"universal"` are entities
  whose parent is `Webhook::"*"`. The new action `use` is sending to a webhook. A webhook grant is a
  permission row on `Webhook` that holds `use`, on one webhook or on `*`. A grant on `*` renders
  with `resource in Webhook::"*"`, so it reaches both webhooks. `administer` on `*` makes a webhook
  admin, who manages others' tokens in the next step.
* **Who may use them by default.** A base policy lets editors use both webhooks, and admins can
  through the admin policy. A viewer, or someone with no role, needs a grant.
* **Levels.** The admin UI offers five levels per principal instead of raw rows:

  | Level | Row written |
  | --- | --- |
  | none | none |
  | Alertmanager | `use` on `Webhook::"alertmanager"` |
  | Universal | `use` on `Webhook::"universal"` |
  | both | `use` on `Webhook::"*"` |
  | admin | `use` and `administer` on `Webhook::"*"` |

  Setting a level replaces the principal's webhook rows in one transaction.
  `/admin/access/webhooks` and `PUT /api/access/webhooks` are the `administer` action on
  `Webhook::"*"`, so webhook admins can set levels as well as admins.
* **`/admin/access` (admins only)** shows:
  * webhook levels by principal;
  * every grant, with a filter;
  * the policies in force: the embedded document, and the generated policies as the snapshot parsed
    them, with its generation;
  * **Who can?**: a subject, an action and a resource, answered with the deciding policies from
    Cedar's diagnostics. The subject is answered as of their last sign-in, using the user
    registry's roles and provider groups.
* **`/admin/me` (anyone signed in)** shows the caller's subject, roles, provider groups, local
  groups (resolved through nesting), the webhooks they may use, and the generated policies that
  name them. It sits outside `authorize`, like `/admin/userinfo`, so someone with no role can read
  it. It asks the engine for the current snapshot itself.

Alternatives considered:

* **A role per webhook.** Roles come from the identity provider, which the people managing alerts
  rarely control. Grants can be changed here, and changed again later.
* **Token scopes without user permissions.** A token could then name any webhook, whatever its
  creator may do. That is the gap the next step closes.

## Consequences

* Nothing checks `use` yet. Tokens use it in the next step, so this step changes no sender's
  access.
* "Who can?" answers for a stored user. It cannot see roles or groups the provider would send at
  that user's next sign-in.
* No schema change: webhook grants are `permissions` rows.
