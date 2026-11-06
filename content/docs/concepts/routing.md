---
title: Routing
weight: 1
---

Routing decides, for each incoming event, where it is delivered and which template renders it.
It looks only at the event's labels.

## Label selectors

A route has a label selector: a set of `key: value` pairs, such as `{"severity": "critical"}`. A
selector matches an event when every key in it is present among the event's labels with exactly
the same value. Extra labels on the event do not matter.

* Matching is exact. There are no wildcards, regular expressions or negations.
* An empty selector matches every alert. A root route without a selector receives every alert from
  every webhook, alongside any other root that matches. The default route stays a fallback, so a
  selector-less root leaves it nothing to catch. See [Fallbacks](#fallbacks).

For an Alertmanager notification the labels are its `commonLabels`, the labels every alert in the
group shares; for a group of one alert, that alert's labels. For a universal event they are its
`labels` object.

### The `teamster_source` label

Before routing, Teamster sets the label `teamster_source` to the webhook the event arrived at:
`alertmanager` or `universal`. It overwrites any value the sender set. A route can therefore
select on the source, for example `{"teamster_source": "alertmanager"}`.

### The `teamster_team` and `teamster_channel` labels

A Teams V2 message arrives with `teamster_source=teamsv2` and the two URL segments it was posted to
as `teamster_team` and `teamster_channel`. Only the URL sets them: Teamster removes all three from
a sender's labels on every other webhook.

Teams V2 messages are routed like any other event. Each Teams V2 endpoint has a linked route that
selects these three labels and delivers to the endpoint's destination with its template. It is
created, updated and deleted with the endpoint, and the Routes panel marks it **Managed by webhook
`<team>/<channel>`**. Its selector, target, template, parent and default flag are locked; its name,
priority and greedy flag are not. You cannot delete it while its endpoint exists, and you cannot
delete the endpoint while the route has child routes.

Because the message goes through routing, any other route that selects the same labels also
delivers it. See
[Send the messages to more channels](../../guides/teams-v2-webhook/#send-the-messages-to-more-channels).
A greedy child of the linked route replaces it, and the endpoint's own channel then no longer
receives the message.

An endpoint without a linked route, such as one created by an earlier release, posts straight to
its destination. Teamster creates the missing route when it starts.

## Root routes and priority

A route without a parent is a root route. Teamster evaluates every root route, in descending
priority, with ties ordered by name. **Every root whose selector matches delivers.** Priority sets
the order of the deliveries and of the routes in the admin UI; it does not make one match suppress
another.

So two independent roots that both select `severity=critical` both fire. If you want one route to
take over from another, nest it under that route and mark it greedy, as described next.

## Nested routes

A route can refine another route. A child is considered only when its parent matched, and applies
when its own selector matches too. A child without a selector receives everything its parent
receives: the way to send all of a route's alerts to one more channel. It must still add a target
or a template, and it cannot be greedy; saving is refused.

* **As well as.** By default a matching child delivers in addition to its parent.
* **Instead of.** A *greedy* child delivers instead of its parent. It suppresses all of its
  parent's deliveries, not just the one it shares a kind with.
* **Inheritance.** A child that names no target or no template uses the nearest ancestor's. "The
  same card, to one more channel" is a child with a single field set.
* Every matching child delivers, not just the first.
* Routes nest at most five levels deep.

A route with children cannot be deleted. Remove or reparent the children first: an orphan would
become a root and match alerts its parent used to filter out.

## Targets

A route delivers to exactly one kind of target:

| Target | Delivers to |
| --- | --- |
| A destination | a Teams channel |
| A person | that person's chat with the bot, once they linked it |
| Any person named in the message | everyone the event names in `recipients` or the `teamster_recipient` label |

A root route must name a target. A child that keeps its parent's target must use a different
template, or it would add nothing. A route may name only your own chat as its person, unless you
are an admin.

Anyone who may edit routes may create a route to **Any person named in the message**. Whom it
reaches is decided by the sender, not by the route: a token that may name only its creator reaches
only them through it, however broad the selector. See
[Send messages to individual people](../../guides/direct-messages/).

Routes are global, though. Such a route matches every message its selector matches, whoever sent
it, and renders them with its author's template. A route with a broad selector, such as
`{"teamster_source": "universal"}`, therefore sends an extra message, in its author's words, to the
people other senders name. It reaches no one those senders could not message already. Look for
such routes in the [routing picture](#seeing-the-routes) and the
[audit trail](../../guides/audit-trail/).

## Fallbacks

When no root route matches, Teamster falls back in two steps:

1. **The default route.** A root route marked as the default is used only when no other root
   matched. It never fires alongside a real match.
2. **The global default destination.** When nothing matched and there is no default route,
   including when no routes exist at all, the event goes to the global default destination. The
   first destination you create becomes it, and an admin can make another destination the
   default. Its template is chosen on its row in the Routes panel; without one it sends a built-in
   message.

Only when no destination exists at all is an event delivered nowhere.

## Seeing the routes

`/admin/routing` draws the path an event takes: the webhook it arrives at, the routes in the order
they are evaluated with their children hanging off them, and the channels and people they deliver
to.

* Route nodes show the labels they select, or *no selector — matches every alert*, and the
  template they render with, marked when it is inherited. The Routes list says *matches every
  alert* for an empty selector, and *catches what no other route matches* for a default route
  with one.
* A dashed arrow between two routes is a refinement, labelled *as well as* or *instead of*.
* A route pointing at a deleted destination or person shows up as a missing node.
* A second graph pairs templates with the routes and webhooks that use them. A template with
  nothing beside it is unused.

Paste `key=value` labels into the page to ask which routes a given event would take. Every route
that delivers is named and explained (a route without a selector says it takes every alert), and
its path is highlighted while the rest dims.

## Related

* How a delivered card is updated and resolved:
  [Alert lifecycle]({{< ref "/docs/concepts/alert-lifecycle" >}}).
* What a template sees: [Template data](../../reference/template-data/).
