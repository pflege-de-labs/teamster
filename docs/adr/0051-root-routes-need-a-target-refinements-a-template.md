# 0051. A root route needs a target, and a refinement that keeps it needs a new template

* Status: Accepted
* Date: 2026-09-29

## Context

[ADR 0047](0047-a-route-targets-a-channel-or-yourself.md) gave the route form one **Delivers to**
select. Its empty choice means "inherit from the route it refines". Nothing stopped a root route
from choosing it. A root route has no parent, so it saved without a target and delivered nowhere.

A child route could also keep its parent's target and set the template its parent already renders
with. That child changes nothing but still fires, so a non-greedy one sends a second, identical
message.

## Decision

`routing.ValidateRoute` will enforce two more rules, on every write path and on import:

* **A root route must name a target:** a channel or a person.
* **A child that names no target keeps its parent's, and must set a template other than the one it
  inherits.** The inherited template is the nearest ancestor's that sets one.

A child that names its own target stays valid whatever its template, because it delivers somewhere
new. The form's inherit choice now says it is for refining routes only.

## Consequences

* A route saved before these rules keeps its current behaviour, but must be fixed before it can be
  saved again.
* A bundle carrying such a route is refused on import, as a cycle already is.
* The rule "a child that inherits everything changes nothing" is now part of the template rule.
* No schema change.
