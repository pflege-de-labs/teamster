// Draws the routing graph and explains a match. Routing priority is hard to read
// from three separate tables, so the picture and the probe live on one page.
(function () {
  const container = document.getElementById("routing-graph");
  if (!container) return;

  const form = document.getElementById("match-form");
  const result = document.getElementById("match-result");

  // Alerts flow left to right, so the drawing is a layered DAG: the server
  // places every node, the browser only draws and lets them be dragged.
  const boxWidth = 220;
  const boxHeight = 56;
  const margin = 48;

  const accents = {
    source: "#64748b",
    route: "#0ea5e9",
    destination: "#10b981",
    template: "#f59e0b",
  };

  const headings = {
    source: "Webhook",
    route: "Routes",
    destination: "Destinations",
    template: "Templates",
  };

  // Indigo belongs to nothing else on the page, so a highlighted path cannot be
  // mistaken for a kind of node.
  const matchColour = "#4f46e5";

  let groups = null;
  let edges = null;
  let edgeData = [];
  let sourceID = null;

  function empty(message) {
    container.replaceChildren();
    const p = document.createElement("p");
    p.className = "flex h-full items-center justify-center text-sm text-slate-500";
    p.textContent = message;
    container.appendChild(p);
  }

  function clip(text, max) {
    if (!text) return "";
    return text.length > max ? text.slice(0, max - 1) + "…" : text;
  }

  // What the node filters for, or why it filters for nothing.
  function subtitle(node) {
    if (node.kind === "route") {
      if (node.selector) return node.selector;
      return node.default ? "no selector — catches the rest" : "no selector — matches nothing";
    }
    return node.detail || "";
  }

  function tooltip(node) {
    const parts = [node.label];
    const sub = subtitle(node);
    if (sub) parts.push(sub);
    if (node.kind === "route") {
      parts.push("priority " + node.priority);
      if (node.default) parts.push("default route");
    }
    if (node.missing) parts.push("referenced by a route but no longer exists");
    return parts.join("\n");
  }

  // Column headings sit above the first node of each kind, so the two groups
  // sharing the right-hand column stay told apart.
  function columnHeadings(nodes) {
    const tops = new Map();
    nodes.forEach((node) => {
      const seen = tops.get(node.kind);
      if (!seen || node.y < seen.y) tops.set(node.kind, node);
    });
    return Array.from(tops, ([kind, node]) => ({
      kind: kind,
      text: headings[kind] || kind,
      x: node.x,
      y: node.y - 18,
    }));
  }

  function draw(nodes, links) {
    const width = container.clientWidth;
    const height = container.clientHeight;
    const byID = new Map(nodes.map((node) => [node.id, node]));

    container.replaceChildren();
    const svg = d3
      .select(container)
      .append("svg")
      .attr("width", "100%")
      .attr("height", "100%")
      .attr("viewBox", [0, 0, width, height]);

    const defs = svg.append("defs");
    [
      { id: "routing-arrow", fill: "#94a3b8" },
      { id: "routing-arrow-match", fill: matchColour },
    ].forEach((arrow) => {
      defs
        .append("marker")
        .attr("id", arrow.id)
        .attr("viewBox", "0 0 10 10")
        .attr("refX", 9)
        .attr("refY", 5)
        .attr("markerWidth", 8)
        .attr("markerHeight", 8)
        .attr("orient", "auto-start-reverse")
        .append("path")
        .attr("d", "M 0 0 L 10 5 L 0 10 z")
        .attr("fill", arrow.fill);
    });

    const root = svg.append("g");
    const zoom = d3.zoom().scaleExtent([0.2, 4]).on("zoom", (event) => {
      root.attr("transform", event.transform);
    });
    svg.call(zoom);

    // Edges leave the right edge of a node and arrive at the left edge of the
    // next, which is what makes the direction of the flow visible.
    function path(link) {
      const from = byID.get(link.source);
      const to = byID.get(link.target);
      if (!from || !to) return "";
      const x1 = from.x + boxWidth;
      const y1 = from.y + boxHeight / 2;
      const x2 = to.x;
      const y2 = to.y + boxHeight / 2;
      const bend = Math.max(24, (x2 - x1) / 2);
      return "M" + x1 + "," + y1 + "C" + (x1 + bend) + "," + y1 + " " + (x2 - bend) + "," + y2 + " " + x2 + "," + y2;
    }

    const link = root
      .append("g")
      .attr("fill", "none")
      .attr("stroke", "#94a3b8")
      .attr("stroke-width", 1.5)
      .selectAll("path")
      .data(links)
      .join("path")
      .attr("marker-end", "url(#routing-arrow)")
      .attr("d", path);

    const heading = root
      .append("g")
      .selectAll("text")
      .data(columnHeadings(nodes))
      .join("text")
      .attr("x", (d) => d.x)
      .attr("y", (d) => d.y)
      .attr("font-size", 11)
      .attr("font-weight", 600)
      .attr("fill", "#64748b")
      .text((d) => d.text);

    const node = root
      .append("g")
      .selectAll("g")
      .data(nodes)
      .join("g")
      .attr("transform", (d) => "translate(" + d.x + "," + d.y + ")")
      .call(
        d3
          .drag()
          .on("drag", (event, d) => {
            d.x = event.x;
            d.y = event.y;
            d3.select(event.sourceEvent.currentTarget).attr("transform", "translate(" + d.x + "," + d.y + ")");
            link.attr("d", path);
          }),
      );

    node
      .append("rect")
      .attr("width", boxWidth)
      .attr("height", boxHeight)
      .attr("rx", 6)
      .attr("fill", (d) => (d.missing ? "#fef2f2" : "#ffffff"))
      .attr("stroke", (d) => (d.missing ? "#ef4444" : "#cbd5e1"))
      .attr("stroke-width", 1);

    // A coloured spine rather than a filled box: the label has to stay readable.
    node
      .append("rect")
      .attr("width", 4)
      .attr("height", boxHeight)
      .attr("fill", (d) => (d.missing ? "#ef4444" : accents[d.kind] || "#64748b"));

    node
      .append("text")
      .attr("x", 14)
      .attr("y", 22)
      .attr("font-size", 12)
      .attr("font-weight", 600)
      .attr("fill", "#0f172a")
      .text((d) => clip(d.label, 28));

    node
      .append("text")
      .attr("x", 14)
      .attr("y", 40)
      .attr("font-size", 10)
      .attr("fill", (d) => (d.missing ? "#b91c1c" : "#475569"))
      .text((d) => clip(subtitle(d), 34));

    node
      .filter((d) => d.kind === "route")
      .append("text")
      .attr("x", boxWidth - 10)
      .attr("y", 22)
      .attr("text-anchor", "end")
      .attr("font-size", 10)
      .attr("fill", "#64748b")
      .text((d) => (d.default ? "default" : "p" + d.priority));

    node.append("title").text((d) => tooltip(d));

    groups = node;
    edges = link;
    edgeData = links;
    const start = nodes.find((candidate) => candidate.kind === "source");
    sourceID = start ? start.id : null;

    // Open on the whole graph rather than on its top left corner.
    const minX = d3.min(nodes, (d) => d.x) - margin;
    const minY = d3.min(nodes, (d) => d.y) - margin;
    const maxX = d3.max(nodes, (d) => d.x) + boxWidth + margin;
    const maxY = d3.max(nodes, (d) => d.y) + boxHeight + margin;
    const scale = Math.min(1, width / (maxX - minX), height / (maxY - minY));
    svg.call(
      zoom.transform,
      d3.zoomIdentity
        .translate((width - (maxX - minX) * scale) / 2, (height - (maxY - minY) * scale) / 2)
        .scale(scale)
        .translate(-minX, -minY),
    );

    return { link: link, heading: heading };
  }

  // A matched route is only half the answer: what an operator wants to see is
  // where that alert ends up, so the whole path from the webhook to the
  // destination is drawn in the match colour and the rest is dimmed.
  function pathOf(routeID) {
    const nodeIDs = new Set([routeID]);
    if (sourceID) nodeIDs.add(sourceID);

    const linkIDs = new Set();
    edgeData.forEach((link) => {
      if (link.target === routeID && link.source === sourceID) {
        linkIDs.add(link.source + ">" + link.target);
      }
      if (link.source === routeID) {
        nodeIDs.add(link.target);
        linkIDs.add(link.source + ">" + link.target);
      }
    });
    return { nodes: nodeIDs, links: linkIDs };
  }

  function highlight(routeID) {
    if (!groups || !edges) return;

    if (!routeID) {
      groups.attr("opacity", 1);
      groups.select("rect").attr("stroke", (d) => (d.missing ? "#ef4444" : "#cbd5e1")).attr("stroke-width", 1);
      edges
        .attr("opacity", 1)
        .attr("stroke", "#94a3b8")
        .attr("stroke-width", 1.5)
        .attr("marker-end", "url(#routing-arrow)");
      return;
    }

    const taken = pathOf(routeID);
    groups.attr("opacity", (d) => (taken.nodes.has(d.id) ? 1 : 0.2));
    groups
      .select("rect")
      .attr("stroke", (d) => {
        if (taken.nodes.has(d.id)) return matchColour;
        return d.missing ? "#ef4444" : "#cbd5e1";
      })
      .attr("stroke-width", (d) => (taken.nodes.has(d.id) ? 2.5 : 1));
    edges
      .attr("opacity", (d) => (taken.links.has(d.source + ">" + d.target) ? 1 : 0.15))
      .attr("stroke", (d) => (taken.links.has(d.source + ">" + d.target) ? matchColour : "#94a3b8"))
      .attr("stroke-width", (d) => (taken.links.has(d.source + ">" + d.target) ? 2.5 : 1.5))
      .attr("marker-end", (d) =>
        taken.links.has(d.source + ">" + d.target) ? "url(#routing-arrow-match)" : "url(#routing-arrow)",
      );
  }

  function parseLabels(text) {
    const labels = {};
    text.split("\n").forEach((line) => {
      const at = line.indexOf("=");
      if (at < 0) return;
      const key = line.slice(0, at).trim();
      if (!key) return;
      labels[key] = line.slice(at + 1).trim();
    });
    return labels;
  }

  function report(message, found) {
    if (!result) return;
    result.replaceChildren();
    const p = document.createElement("p");
    p.className = found
      ? "rounded-md border border-emerald-300 bg-emerald-50 px-3 py-2 text-sm text-emerald-800"
      : "rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm text-amber-900";
    p.textContent = message;
    result.appendChild(p);
  }

  if (form) {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const field = form.querySelector('[name="labels"]');

      let payload;
      try {
        const res = await fetch("/api/routing/match", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ labels: parseLabels(field ? field.value : "") }),
        });
        payload = await res.json();
      } catch (err) {
        report("Match request failed: " + err.message, false);
        return;
      }

      const route = payload ? payload.route : null;
      report(payload && payload.explanation ? payload.explanation : "No explanation came back.", !!route);
      highlight(route ? route.node : null);
    });
  }

  (async () => {
    let payload;
    try {
      const res = await fetch("/api/routing/graph");
      if (!res.ok) throw new Error("status " + res.status);
      payload = await res.json();
    } catch (err) {
      empty("Nothing to draw yet. Create a route, a destination and a template first.");
      return;
    }

    const nodes = payload && Array.isArray(payload.nodes) ? payload.nodes : [];
    // The webhook source is always there, so it alone is not a graph worth drawing.
    if (nodes.filter((node) => node.kind !== "source").length === 0) {
      empty("Nothing to draw yet. Create a route, a destination and a template first.");
      return;
    }

    const links = payload && Array.isArray(payload.links) ? payload.links : [];
    draw(nodes, links);
  })();
})();
