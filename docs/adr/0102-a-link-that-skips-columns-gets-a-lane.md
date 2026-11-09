# 0102. A link that skips columns gets a lane

* Status: Accepted
* Date: 2026-11-09

## Context

In the routing graph's tangled tree
([ADR 0097](0097-lay-out-the-routing-graph-as-a-tangled-tree.md)), a link turns down right after
its parent's column and then runs straight to its child. A root route that delivers to a channel
skips the child route columns, and a Teams V2 endpoint posting without routing skips every column.
Their lines crossed those columns at whatever height the child had, which was usually the height of
some other node's label. The labels were unreadable where lines ran through them, and a line
passing a node looked as if it came from that node. Labels could also run past their column into
the tracks of the next one.

## Decision

We will give a link that skips columns a **lane**: a node without a label in every column it
passes, which the ordering places and the layout gives a row of its own. These are the dummy nodes
of Sugiyama's method.

* `teamsterTangleLanes` in `web/tangle.js` inserts them before ordering
  ([ADR 0100](0100-order-the-routing-graph-to-cross-fewer-lines.md)), so the ordering moves lanes
  as it moves nodes.
* **Shared lanes.** The links of one parent with the same kind share their lanes, so a route that
  delivers to three channels runs as one line until its last column. Mixing kinds would mix dash
  patterns on one line.
* **Short rows.** A lane's row is 16px instead of 29px, enough for its strands and a gap to the
  label below it.
* **Mapped back.** Each drawn segment knows which of the server's links it stands for. Hover, the
  match highlight, the dash and the "instead of" bar all read from those links, so a lane looks
  and behaves like the link it carries.
* **Labels fit their column.** A label is measured and shortened with an ellipsis until it ends
  before the next column's tracks.

We considered more space between rows. It moves the labels, not the lines, so the lines still
cross some label.

## Consequences

* Lines no longer run through labels or through nodes they do not touch.
* The graph gets taller, by one short row per lane, and wider, by a track per lane in each gap.
* Lanes are not drawn as nodes, and a click on one does nothing. The line passing through is still
  hoverable.
* No migration and no API change.
