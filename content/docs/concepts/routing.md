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
`<team>/<channel>`**. Its selector, target, template, parent, default flag and the "child routes
deliver as well as this route" setting (see [Nested routes](#nested-routes)) are locked; its name,
priority and greedy flag are not. You cannot delete it while its endpoint exists, and you cannot
delete the endpoint while the route has child routes.

Because the message goes through routing, any other route that selects the same labels also
delivers it. See
[Send the messages to more channels](../../guides/teams-v2-webhook/#send-the-messages-to-more-channels).
A child of the linked route always delivers as well as it, so the endpoint's own channel always
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
* **Keep a route from being replaced.** Tick **Child routes deliver as well as this route, never
  instead of it** on a route and every child below it delivers as well as it. Saving a greedy
  child under such a route is refused with `route "<name>" keeps its own deliveries, so a child
  route can only deliver as well as it`. If the route already has greedy children, you can still
  set the box: they then deliver as well as it, and the route list shows them as *as well as*. The
  box protects only that route; a greedy grandchild still replaces its own parent. In the API the
  field is `no_greedy_children`, and export and import carry it.
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

`/admin/routing` draws the path an event takes. The graph fills the page and reads left to right:
the webhooks and Teams V2 endpoints events arrive at, the routes, their child routes in a column to
the right of them, and the channels and people they deliver to.

Each column except the first is ordered so that as few lines cross as possible. The **Global
default** node and the default route sit at the top of the routes column, so the lines unmatched
events take run straight along the top. The column therefore does not show the order in which
routes are evaluated: read a route's priority from its label (for example `· p100`) or its hover
card, and see the Routes list for the routes in evaluation order. Channels and people can mix in
the destinations column.

Links with the same parents share one track, as in a metro map. Every line has the colour of the
webhook or Teams V2 endpoint its alerts come from, and a line that carries alerts from several
webhooks runs as several strands side by side. On the **Templates** tab, the colour is the
template instead.

* An icon tells what a node is: a chevron for a webhook, a double chevron for a Teams V2 endpoint,
  a fork for a route, an arrow into a bar for a destination, a person for a chat, and `@` for
  anyone named in the message. Missing nodes (red) and teams without the Teams app installed
  (amber) have their own marks.
* A route shows the labels it selects, or *no selector — matches every alert*, and the template it
  renders with, marked when it is inherited. The Routes list says *matches every alert* for an
  empty selector, and *catches what no other route matches* for a default route with one.
* A dashed line between two routes is a child refining its parent. It ends in a bar when the child
  delivers *instead of* its parent; otherwise it delivers *as well as* it. A dotted line is a
  Teams V2 endpoint posting straight to its channel, without routing.
* A route pointing at a deleted destination or person shows up as a missing node.
* The **Templates** tab pairs templates with the routes and webhooks that use them. A template
  with nothing beside it is unused. Open it directly with `/admin/routing?tab=templates`.

Lines carry no text. Hover a node or a line for what it is and does: the route's selector, priority,
template and warnings, and a preview of its connections. Click a node, or focus it with `Tab` and
press `Enter`, to redraw the graph with only that node, everything leading to it and everything it
leads to, laid out afresh. Colours stay those of the whole graph. Click a node in the part to draw
that node's paths. To get the whole graph back, click the background, press `Esc` or use **Show the
whole graph**, which appears only while a part is drawn. Drag the background to pan, scroll to zoom,
and use **Fit to view** to fit what is drawn. Nodes cannot be moved. The info button (ⓘ) explains
how to read the graph: hover opens it, click pins it, `Esc` closes it.

Paste `key=value` labels into **Which routes match?** to ask which routes a given event would take.
Every route that delivers is named and explained (a route without a selector says it takes every
alert), and its path stays lit while the rest fades.

The label box and the **Legend** float over the graph. Drag them by their title, fold them, or
hide them and bring them back from the buttons at the top left. The legend starts folded. Your
browser remembers where you left them.

## Related

* How a delivered card is updated and resolved:
  [Alert lifecycle]({{< ref "/docs/concepts/alert-lifecycle" >}}).
* What a template sees: [Template data](../../reference/template-data/).
