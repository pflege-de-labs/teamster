# 0097. Lay out the routing graph as a tangled tree

* Status: Accepted, amended by [0100](0100-order-the-routing-graph-to-cross-fewer-lines.md):
  the browser reorders each column; and by
  [0101](0101-a-click-draws-only-the-nodes-paths.md): a click draws only the node's paths; and by
  [0102](0102-a-link-that-skips-columns-gets-a-lane.md): a link that skips columns gets a lane
* Date: 2026-11-09

## Context

`/admin/routing` drew the flow graph as 220×64 boxes joined by curves, in a 32rem box inside the
page's `max-w-6xl` column, beside the label probe and under a legend and a paragraph of help. With
a few webhooks, a few dozen routes and a dozen destinations the curves crossed until neither the
boxes nor the paths could be read. The template graph sat below it on the same page.

The graph is close to a tree: an alert enters at one of a few webhooks, falls through routes and
their children, and lands in a channel. It is not one, though. A root route is entered from every
webhook its selector does not exclude, and several routes deliver to the same channel, so most
nodes have more than one parent. Positions were computed in Go
(`internal/httpserver/routing_api.go`): one column per step, one row per node, which the browser
drew and let people drag.

## Decision

We will draw both graphs with the **tangled tree** technique from nitaku's
[Tangled tree visualization II](https://observablehq.com/@nitaku/tangled-tree-visualization-ii)
(MIT). Links that share the same parents are bundled into one vertical track, and each node is a
small "station" that grows with the tracks leaving it, as in a metro map.

* **Port, not dependency.** `web/tangle.js` is a port of the notebook's layout function with its
  MIT notice. It needs no d3 or lodash, because the original only used them for `min`, `max` and a
  clone. It is not an npm package, so it is not vendored under ADR 0031. A one-off comparison
  against the original on the notebook's data gave identical coordinates.
* **Compact mode.** The original stacks every column below the previous one, which suits a
  genealogy but makes our graph a tall staircase. A focused path then still spans the full height
  and cannot be framed at a readable size. We top-align the columns instead and let a link rise as
  well as fall. Whichever way a link goes it has the same two bends, mirrored.
* **The server orders and the browser places.** The API is unchanged. The browser reads the
  server's column (`x`) as the level and its row (`y`) as the order within the level, so the Go
  code still decides which column a node belongs to and keeps children next to their parents. A
  link that does not point to a later column is dropped with a console warning rather than folding
  the layout.
* **Colour is where an alert came from.** Every link takes the colour of the first-column node it
  carries alerts from: the webhook or Teams V2 endpoint on the flow graph, the template on the
  template graph. A link reached from several of them is drawn as parallel strands, one per colour.
  The colours are the categorical palette in a fixed order, and past eight they fold into grey.
  Node kinds are told apart by an icon, not a colour. Status (missing, Teams app not installed)
  keeps red and amber, always next to a glyph.
* **The graph gets the page.** A `FullLayout` beside `Layout` gives `<main>` the whole space below
  the header and beside the sidebar. The label probe and the legend become panels that float over
  the graph. They can be dragged, folded and hidden, and the browser remembers them in
  `localStorage` under `teamster.routing.panels`. The help text sits behind an info button using the
  native `popover` attribute. The template graph moves to a second tab, `?tab=templates`.
* **Reading instead of arranging.** Hovering shows an HTML card with what the node or link means;
  the old edge labels are gone from the drawing. Hovering also previews the node's connections.
  Clicking lights up everything upstream and downstream of the node and zooms to it, without
  shrinking the labels below a readable scale. Nodes can no longer be dragged: a computed layout
  with bundled tracks has no free position to drag a node to.

We considered keeping the box graph and adding a force or Sugiyama layout from a library. A
library would have been a new vendored dependency, and none of them bundle shared parents, which
is where the crowding came from.

## Consequences

* The drawing is readable at the scale of a real configuration, and a path can be followed by its
  colour or brought into focus with one click.
* Geometry now lives in the browser, where Go tests do not reach it. The server's column and row
  assignment is still tested in Go. The layout port was checked against the original, and the page
  is checked in a browser.
* "As well as" and "instead of" are no longer printed on each edge. A greedy child's link ends in a
  bar instead, and the legend and the hover card spell it out.
* Strings the script builds (hover cards, column headings, empty states) are still English, as they
  were before.
* No migration. Panel positions are a per-browser convenience, and losing them only resets the
  panels.
