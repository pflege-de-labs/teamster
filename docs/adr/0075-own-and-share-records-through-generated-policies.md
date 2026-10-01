# 0075. Own and share records through generated Cedar policies

* Status: Accepted
* Date: 2026-10-01

## Context

Access is granted per role and per collection ([ADR 0012](0012-role-based-authorization.md)): an
editor may change every template, and a viewer may change none. Teams want to look after their own
templates, destinations, routes, webhook endpoints and groups. They want to hand one record to a
colleague, or a group, without making them an editor of everything.

The snapshot ([ADR 0073](0073-authorize-from-a-versioned-policy-snapshot.md)) and groups
([ADR 0074](0074-local-groups-and-provider-groups.md)) give Cedar the principals. What is missing
is a way to say "this principal may do this to that record", and handlers that ask about the record
rather than the collection.

## Decision

We will store permissions as rows and render each row to its own Cedar `permit` policy.

* **Rows.** A `permissions` row holds:

  | Column | What it holds |
  | --- | --- |
  | principal | `user`, `group`, `idp_group` or `role`, with its id |
  | resource | a type and an id. The types are `Template`, `Destination`, `Route`, `WebhookEndpoint` and `Group`. `*` is the collection |
  | actions | what the principal may do |

  There is one row per principal and resource.
* **Rendering.** `authz.Grant.Render` builds the policy with the cedar-go AST, never from text, so
  an id cannot inject Cedar syntax. Each policy carries `@id("perm:<row id>")`. The snapshot keeps
  the rendered text for the permissions overview.

  ```cedar
  @id("perm:p1") permit (principal == User::"alice", action in [Action::"own"], resource == Template::"t1");
  @id("perm:p2") permit (principal in Group::"g-sre", action in [Action::"read", Action::"update"], resource == Destination::"d7");
  @id("perm:p3") permit (principal in IdpGroup::"platform", action in [Action::"create"], resource == Route::"*");
  ```

* **Owners.** Ownership is an `own` row, not a column, so the change stays additive. `own` holds
  every record action through the action hierarchy. Creating a record writes the creator's `own`
  row in the same transaction. A record without one, which is everything that existed before, is
  the admins'. Deleting a record deletes its rows. Deleting a group deletes the rows it held and
  the rows held on it.
* **Grantable actions.**

  | Action | Lets the holder |
  | --- | --- |
  | `read` | read the record |
  | `update` | change it |
  | `delete` | delete it |
  | `attach` | point a route or webhook endpoint at it |
  | `share` | grant ordinary actions to others |
  | `own` | do everything, including `transfer` |
  | `create` | create records; granted on the collection only |

  `attach` is the grant to route into a destination. For a grant holder it replaces the role's
  delivery scope. A role's own `attach` never does, so scoped editors stay scoped.
* **Rules for changing permissions.**
  * Granting an ordinary action takes `share`.
  * Granting or taking away `own` takes `transfer`.
  * Nobody can grant an action they do not hold.

  Removing the last owner is allowed: the record falls back to the admins. Permission changes write
  their audit row in their own transaction, as group changes do.
* **Record checks.** Handlers check the record they touch:
  * The JSON API and the forms for the five types, and the sharing endpoints, call
    `mayRecord`/`can`.
  * Lists go through `readable`. Destinations and endpoints keep the role's channel scoping for
    role holders.
  * The admin page shows edit and delete per row, and a sharing panel on the record being edited.
* **The middleware.** For an explicit list of record endpoints, a principal the role policies refuse
  but some grant names is let through to the handler (`recordPath`, `Authorizer.HasGrants`). Such a
  request is watched: one answered below 400 without a record check is logged and counted, and the
  tests assert none is. Derived views stay role-level: the routing graph, previews, samples, the
  export and the catch-all settings.

Alternatives considered:

* **Owner columns on each table.** These would need five migrations and still a table for shares.
  The previous release would also read records that changed shape.
* **Static policies over entity attributes**, such as `resource.readers contains principal`. That
  needs every record as an entity on every request, and the rule would not appear in Cedar text.
* **Checking everything in the middleware.** The middleware does not know which record a form post
  names, or which records a list returns.

## Consequences

* A grant holder with no role sees `/admin` with what was shared with them, and nothing else.
  Pages built from the whole configuration stay closed to them.
* A new handler for a permissioned type has to check its record. If it is added to `recordPath`
  and forgets the check, the witness catches it.
* Editors keep full access to everything. Sharing a record that predates this release needs an
  admin, because nobody owns it.
* `permissions` is a new table (SQLite `0027`, Postgres `0024`). If the release is rolled back,
  access that comes only from rows is lost (fails closed), and roles are unchanged.
* Milestone 22 ("personal routes for others, by grant") can be a row on `User::"<subject>"` with
  `deliverToRecipient`, once the UI offers it.
