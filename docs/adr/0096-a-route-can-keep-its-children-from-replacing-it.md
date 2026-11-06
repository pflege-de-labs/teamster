# 0096. Let a route keep its children from replacing it

* Status: Accepted
* Date: 2026-11-06

## Context

A greedy child delivers instead of its parent ([ADR 0011](0011-nested-routes.md)). That is what a
child is for when it narrows a route down, and it is a hazard when the parent's own delivery is the
point. [ADR 0094](0094-a-teams-v2-message-is-routed.md) recorded the sharpest case as a known gap:
a greedy child under a Teams V2 webhook's linked route stops the webhook's own channel from
receiving its messages, its creator needs no permission on the parent, and the endpoint cannot be
deleted while the child exists.

## Decision

We will give a route the flag `no_greedy_children`. Below a route that sets it, every child
delivers as well as the route, never instead of it.

* `ValidateRoute` refuses a greedy child under such a route, naming the parent. Setting the flag on
  a route that already has greedy children is allowed; those children stop replacing it.
* The router ignores `Greedy` on a child whose parent has the flag (`models.Route.Replaces`), so a
  child stored before the flag, or by the previous release during a rollout, cannot silence the
  parent either. The route list and the routing graph show such a child as "as well as".
* The flag protects only the route that sets it. A greedy grandchild still replaces its own parent.
* Every linked route has the flag, locked like its selector and target. The webhook owns it: an
  edit that leaves it out keeps it, and the startup reconcile sets it on linked routes created
  before this change.

We considered refusing the flag while greedy children exist. That would make the startup reconcile
fail on exactly the installations that need it, and the router has to ignore such children anyway.

## Consequences

* A webhook's own channel always receives the webhook's messages; ADR 0094's known gap is closed.
* Any route can opt in, for a channel that must see everything its route matches.
* A greedy child that already exists under a linked route keeps delivering, now as well as its
  parent, so that channel gets the message twice where both point at it. Saving that child again is
  refused until greedy is cleared.
* Schema: `routes.no_greedy_children` (`NOT NULL DEFAULT false`), by `0033` in SQLite and `0030` in
  Postgres. The previous release never reads or writes it, so its updates keep the stored value.
