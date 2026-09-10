// Draws the routing graph and explains a match. Routing priority is hard to read
// from three separate tables, so the picture and the probe live on one page.
(function () {
  const container = document.getElementById("routing-graph");
  if (!container) return;

  const form = document.getElementById("match-form");
  const result = document.getElementById("match-result");

  const fills = {
    route: "#0ea5e9",
    destination: "#10b981",
    template: "#f59e0b",
  };

  let groups = null;

  function empty(message) {
    container.replaceChildren();
    const p = document.createElement("p");
    p.className = "flex h-full items-center justify-center text-sm text-slate-500";
    p.textContent = message;
    container.appendChild(p);
  }

  function tooltip(node) {
    if (node.kind !== "route") return node.detail || "";
    const parts = [node.selector ? node.selector : "matches nothing on its own"];
    parts.push("priority " + node.priority);
    if (node.default) parts.push("default route");
    return parts.join("\n");
  }

  function draw(nodes, links) {
    const width = container.clientWidth;
    const height = container.clientHeight;

    container.replaceChildren();
    const svg = d3
      .select(container)
      .append("svg")
      .attr("width", "100%")
      .attr("height", "100%")
      .attr("viewBox", [0, 0, width, height]);

    const root = svg.append("g");
    svg.call(
      d3.zoom().scaleExtent([0.2, 4]).on("zoom", (event) => {
        root.attr("transform", event.transform);
      }),
    );

    const simulation = d3
      .forceSimulation(nodes)
      .force(
        "link",
        d3
          .forceLink(links)
          .id((d) => d.id)
          .distance(110),
      )
      .force("charge", d3.forceManyBody().strength(-320))
      .force("center", d3.forceCenter(width / 2, height / 2))
      .force("collide", d3.forceCollide(28));

    const link = root
      .append("g")
      .attr("stroke", "#cbd5e1")
      .attr("stroke-width", 1.5)
      .selectAll("line")
      .data(links)
      .join("line");

    const node = root
      .append("g")
      .selectAll("g")
      .data(nodes)
      .join("g")
      .call(
        d3
          .drag()
          .on("start", (event, d) => {
            if (!event.active) simulation.alphaTarget(0.3).restart();
            d.fx = d.x;
            d.fy = d.y;
          })
          .on("drag", (event, d) => {
            d.fx = event.x;
            d.fy = event.y;
          })
          .on("end", (event, d) => {
            if (!event.active) simulation.alphaTarget(0);
            d.fx = null;
            d.fy = null;
          }),
      );

    node
      .append("circle")
      .attr("r", 10)
      .attr("fill", (d) => (d.missing ? "#ef4444" : fills[d.kind] || "#64748b"))
      .attr("stroke", "#ffffff")
      .attr("stroke-width", 1.5);

    node
      .append("text")
      .attr("x", 14)
      .attr("dy", "0.35em")
      .attr("font-size", 11)
      .attr("fill", "#0f172a")
      .text((d) => d.label);

    node.append("title").text((d) => tooltip(d));

    simulation.on("tick", () => {
      link
        .attr("x1", (d) => d.source.x)
        .attr("y1", (d) => d.source.y)
        .attr("x2", (d) => d.target.x)
        .attr("y2", (d) => d.target.y);
      node.attr("transform", (d) => "translate(" + d.x + "," + d.y + ")");
    });

    groups = node;
  }

  function highlight(id) {
    if (!groups) return;
    if (!id) {
      groups.attr("opacity", 1);
      groups.select("circle").attr("stroke-width", 1.5);
      return;
    }
    groups.attr("opacity", (d) => (d.id === id ? 1 : 0.25));
    groups.select("circle").attr("stroke-width", (d) => (d.id === id ? 3.5 : 1.5));
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
    if (nodes.length === 0) {
      empty("Nothing to draw yet. Create a route, a destination and a template first.");
      return;
    }

    const links = payload && Array.isArray(payload.links) ? payload.links : [];
    draw(nodes, links);
  })();
})();
