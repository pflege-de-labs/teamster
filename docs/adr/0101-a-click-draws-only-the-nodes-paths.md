# 0101. A click draws only the node's paths

* Status: Accepted
* Date: 2026-11-09

## Context

[ADR 0097](0097-lay-out-the-routing-graph-as-a-tangled-tree.md) made a click on a node light up
everything upstream and downstream of it and zoom there. The rest of the graph stayed in place,
faded. On a real configuration the lit paths are spread over the whole layout: they were placed for
the full graph, so they keep its gaps and cross faded lines on the way. Zooming to them often
cannot get closer than the full graph does, and the path is still hard to follow.

## Decision

We will redraw the graph with only the clicked node's paths, laid out from scratch.

* **The same part as before.** The redrawn part is what the click used to light up: the node,
  everything that leads to it and everything it leads to, and only the links on those paths. A
  route that shares a channel with the node, but not a path, stays out.
* **The same layout.** The part goes through the same ordering
  ([ADR 0100](0100-order-the-routing-graph-to-cross-fewer-lines.md)) and tangled tree as the whole
  graph. Empty columns disappear.
* **Colours come from the whole graph.** A webhook keeps its colour in the part, and the legend
  keeps listing every webhook.
* **The way back.** Clicking the background, `Esc`, or the **Show the whole graph** button in the
  toolbar draws the whole graph again. The button only shows while a part is drawn. Clicking a node
  in the part draws that node's paths, taken from the whole graph.
* **The probe and hover keep highlighting.** A match result draws the whole graph and lights up
  its paths, as before. Hovering still previews a node's connections within what is drawn.

We considered keeping the highlight and adding the redraw as a second action. Two ways of focusing
a node is one more thing to learn, and the highlight is what the redraw does better.

## Consequences

* A clicked path reads on its own, straight and close up.
* The rest of the graph is out of sight while a part is drawn, so seeing what a node is *not*
  connected to takes a step back to the whole graph.
* Redrawing replaces the keyboard focus, so the clicked node is focused again after the redraw.
* No migration and no API change.
