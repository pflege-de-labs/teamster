# 0100. Order the routing graph to cross fewer lines

* Status: Accepted
* Date: 2026-11-09

## Context

[ADR 0097](0097-lay-out-the-routing-graph-as-a-tangled-tree.md) left the order within a column to
the server: routes by priority, child routes next to their parents, destinations as they are
listed. The browser only placed them. On a real configuration that order has nothing to do with
where the lines go. A route at the top delivers to a channel at the bottom, its neighbour the other
way round, and the lines cross between every pair of columns. The global default is evaluated last,
so it sat at the bottom of the routes, while the default channel it delivers to was listed first.
Its line, the one most alerts end up on, crossed the whole graph.

## Decision

We will reorder each column in the browser, before the tangled tree lays it out, so that fewer
lines cross.

* **Barycenter sweeps.** `teamsterTangleOrder` in `web/tangle.js` sorts every column by the
  average position of the nodes it is linked to: left to right by parents, then right to left by
  children, four times over. This is the crossing-reduction step of Sugiyama's method. Compact mode
  top-aligns the columns, so a node's index stands in for its height.
* **Ties keep the server's order.** Routes fed by the same webhooks sort equal, and keep priority
  order among themselves. A node with no links in the direction of the sweep keeps its place.
* **The first column is fixed.** Line colours are assigned by the webhooks' order, so moving them
  would repaint the graph. The webhooks stay in the server's order.
* **Default routes go first.** The global default and any route marked default lead the routes
  column, so the line to the default channel runs straight along the top.
* **The server still decides the columns.** The API and its rows are unchanged; a row is now the
  starting order, not the final one.

We considered an exact layout (crossing minimisation is NP-hard, and an ILP solver would be a
dependency) and doing the pass in Go, which would have needed the same pass for the template graph
and would mean the order is tested in two places. Barycenter sweeps are a few dozen lines, cheap
for graphs of this size, and testable with `node:test`
([ADR 0098](0098-test-browser-script-logic-with-node-test.md)).

## Consequences

* Lines cross less: on the seeded test configuration, crossings between adjacent columns fall from
  149 to 62. The result is a heuristic, not a minimum.
* The routes column no longer reads top to bottom in evaluation order. A route's priority is on its
  label and in its hover card, and the Routes page lists routes in evaluation order.
* Channels and people may mix in the destinations column, since they sort by where their lines
  come from.
* No migration and no API change.
