# 0095. Let an empty selector match every alert

* Status: Accepted
* Date: 2026-11-06

## Context

A selector matches when every one of its pairs is among the alert's labels. An empty selector has
no pairs, yet the router treated it as matching nothing: a root without a selector never fired, and
[ADR 0011](0011-nested-routes.md) refused a child without one because it could never fire.

That leaves no way to say "everything", or "everything this route gets, in one more channel".
Operators reached for a dummy label every alert happens to carry, such as `teamster_source`, which
breaks as soon as a new webhook is added. The bot's `selectorLine` and the store already described
an empty selector as matching every alert, so the code disagreed with itself.

## Decision

We will make an empty selector match every alert.

* A root without a selector is entered for every alert, alongside every other root that matches
  ([ADR 0034](0034-fan-out-across-independent-routes.md)).
* The default route is unchanged. It is still a fallback, entered only when no other root matched,
  whatever its selector says. A root without a selector therefore leaves the default nothing to
  catch.
* A child without a selector receives every alert its parent receives. It has to add a target or a
  template, as every child does.
* A child without a selector cannot be greedy. Replacing its parent on every alert would silence the
  parent entirely; the same thing is a change to the parent. `ValidateRoute` refuses it, and the
  router ignores the flag on such a child if one is stored anyway (`models.Route.Replaces`).

We did not add an explicit "match all" flag. It would be a second way to say what the empty
selector already says, and the empty selector would still need a meaning.

## Consequences

* "Everything, in one more channel" is a child route with only a destination set.
* **Breaking:** an existing root without a selector, which never fired, starts receiving every
  alert, and a default behind it stops receiving any. No migration changes such routes; startup
  logs a warning naming each one, so an operator can give it a selector or delete it.
* The routing page, the route list and the preview describe an empty selector as matching every
  alert.
* This amends ADR 0011, whose rule that a child needs a selector no longer holds.
