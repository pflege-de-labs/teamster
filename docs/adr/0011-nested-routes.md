# 0011. Route alerts through a tree, not a single match

* Status: Accepted
* Date: 2026-09-11

## Context

One alert produced one message. A team that wants an alert in its own channel *and* in the platform
channel had to write two routes with the same selector and keep them in step by hand; the moment
they drift, one of the two channels goes quiet and nobody notices.

Alertmanager solved this years ago with a routing tree, and the operators configuring this service
already think in those terms. Borrowing the model is cheaper — for us and for them — than inventing
another one.

The constraint that shapes the rest: `active_alerts` was keyed by fingerprint alone and held one
message id, which is exactly the assumption fan-out breaks.

## Decision

We will let a route have a parent.

* `ParentID` empty makes a root route, selected as before: highest priority first, ties by name,
  the default last.
* A child is evaluated only once its parent matched, and its own selector decides whether it
  applies. A child therefore needs a selector; one without it could never fire.
* `Greedy` says a child delivers **instead of** its parent. A non-greedy child delivers **as well
  as** it. The admin UI words it that way rather than exposing the flag name.
* `DestinationID` and `TemplateID` are optional on a child and inherited from the nearest ancestor
  that sets one. "The same card, in one more channel" is then a route with a single field set. A
  child that inherits both and only refines the selector changes nothing and is refused.
* `routing.Match` and `SelectRoute` are replaced by `Plan`, which returns a `Result`: the reason the
  tree was entered where it was, the root that matched, and a `Delivery` per message with its
  destination and template already resolved.

**Every matching child delivers**, rather than the first one winning. The roadmap sketched
Alertmanager's first-match-wins with a `continue` flag to override it; with a single flag the
inverse default is the coherent one, because the entire point of the feature is sending an alert to
more places than one. A second flag can be added later if suppressing siblings turns out to be
wanted; adding it then is a smaller change than taking fan-out away.

Validation lives in `routing.ValidateRoute` and is called from both write paths. A parent must
exist, a route may not be its own parent, the parent chain may not cycle, nesting stops at
`MaxDepth` (5), and only a root may be the default. `routing.ValidateDelete` refuses to delete a
route that still has children, because orphaning them would silently promote them to roots where
their selectors match alerts their parent used to filter out. `Plan` also stops at `MaxDepth`, so a
tree broken by some other route still delivers rather than hanging.

`active_alerts` is re-keyed to `(fingerprint, team_id, channel_id)`. Delivery is best effort per
destination: one channel failing does not cost the others their message, and the failures are
reported together. Resolution walks the **stored cards** rather than the plan, so a card in a
channel the routes no longer name still stops claiming the alert is firing.

## Consequences

An alert can now produce several messages, which is a change in what this service is: "one alert,
one card" was an assumption several places leaned on. `webhooks.processAlert` splits into
`deliver`, `resolveAlert` and `render`, and the store grows `ListActiveAlerts`.

SQLite cannot change a primary key in place, so the store rebuilds `active_alerts` on first start
after this version — create, copy, drop, rename. The rows survive, because one card per alert is a
valid fan-out of one. This runs beside the added-columns migration from
[ADR 0010](0010-message-shape.md); together they are the beginning of a migration list this service
did not previously need.

A partial failure still answers `502`. That is safe now precisely because each delivery records its
own message id: an Alertmanager retry updates the cards that made it rather than posting them twice.

The routing visualization draws children hanging off their parents and resolves inherited targets,
which keeps it honest, but templates are still drawn as nodes. Milestone 6 finishes that.

The admin API can express a tree, so an import format ([milestone 8](../roadmap.md)) has to carry
`parent_id` and order its writes so that a parent exists before its children.
