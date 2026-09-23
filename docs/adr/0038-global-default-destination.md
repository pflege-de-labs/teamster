# 0038. Catch every unclaimed message in a global default destination

* Status: Accepted
* Date: 2026-09-23

## Context

A message that matches no root route, on an installation with no default route, is rejected with a
`502`. The same happens to every message before the first route exists. The sender retries a
message that can never succeed, and whoever set up the destination sees nothing arrive.

A default route (`routes.is_default`) already covers "everything else", but it has to be created by
hand, it can be deleted, and nothing stops two routes from claiming it. It is also a route. It
carries a selector, a priority and children that make no sense for a fallback.

## Decision

We will mark one **destination** as the global default and use it as the last resort:

* **Where the flag lives.** `destinations.is_default` holds it. A partial unique index
  (`WHERE is_default`) allows at most one default, in both SQLite and Postgres.
* **The first destination becomes the default.** `CreateDestination` sets the flag in the same
  statement that inserts the row whenever no default exists yet. That covers a fresh installation,
  and also one where the previous release has deleted the default. The migration marks the oldest
  existing destination, so an installation that already has destinations behaves as though the rule
  had always applied.
* **Switching is explicit.** `SetDefaultDestination` clears the old default and marks the new one
  in one transaction. The switch is the admin role's action (`administer` on `Destination`), not an
  editor's. It changes where every unclaimed message goes, which reaches further than editing one
  destination.
* **The default cannot be deleted while another destination could replace it.** The last
  destination can still be deleted.
* **Layered, not replacing.** `routing.Plan` still tries every matching root first, and then the
  default route. The global default applies only where `Plan` would otherwise have answered `none`
  or `no-routes`, and the reason it reports is `global-default`. Its delivery comes from a
  synthetic route, `routing.GlobalDefaultRouteID`, which has no template.
* **Shown, not stored.** The admin route list and the routing picture draw the synthetic route last,
  where it is evaluated, with no controls to edit or delete it.

We rejected replacing route defaults with the new flag. It would have meant dropping
`routes.is_default`, which takes two releases, and it would have changed the behaviour of every
installation that relies on a default route today.

## Consequences

An installation with at least one destination no longer rejects a message for being unrouted. The
only reason left for rejecting one is that there is no destination at all.

A message caught by the global default has no template. Until the built-in default message lands
(see the roadmap), a payload carrying none of `title`, `text` or `card` still fails to render
there. That affects every Alertmanager alert.

The schema change is additive. The previous release ignores the column, inserts rows that take its
default, and may delete the default destination. The next destination created then takes over.

Config transfer carries the flag. An import that names a default applies it. A replace that deletes
the current default without naming a new one promotes the bundle's first destination instead.
