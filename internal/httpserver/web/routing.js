// Draws the routing graph and explains a match. Routing priority is hard to read
// from three separate tables, so the picture and the probe live on one page.
// The layout is a tangled tree (tangle.js); see ADR 0097.
(function () {
  const flowContainer = document.getElementById("routing-graph");
  if (!flowContainer) return;

  const templateContainer = document.getElementById("template-graph");
  const form = document.getElementById("match-form");
  const result = document.getElementById("match-result");

  const geometry = {
    padding: 16,
    nodeHeight: 29,
    nodeWidth: 210,
    bundleWidth: 7,
    levelYPadding: 18,
    metroD: 4,
    minFamilyHeight: 29,
    c: 10,
    bigc: 40,
    compact: true,
  };
  // Below this the 12px labels stop being readable, so a tall graph scrolls instead.
  const minScale = 0.9;
  const maxScale = 1.6;
  const surface = "#f8fafc";
  const ink = "#0f172a";
  const lineLit = "#334155";
  // Lines take the colour of the first-column node they come from, in fixed order
  // (the validated categorical palette); past eight they fold into grey.
  const palette = ["#2a78d6", "#eb6834", "#1baf7a", "#eda100", "#e87ba4", "#008300", "#6250d6", "#e34948"];
  const otherColour = "#94a3b8";
  const strandGap = 3;

  const kindNames = {
    source: "Webhook",
    endpoint: "Teams V2 endpoint",
    route: "Route",
    destination: "Destination",
    recipient: "Person",
    addressed: "People",
    template: "Template",
  };

  const headings = {
    source: "Webhooks",
    endpoint: "Teams V2 endpoints",
    route: "Routes",
    destination: "Destinations",
    recipient: "Destinations",
    addressed: "Destinations",
    template: "Templates",
  };

  // The flow graph is the one the match probe highlights.
  let flow = null;

  function empty(container, message) {
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
      if (node.synthetic) return node.detail;
      if (node.selector) return node.selector;
      return node.default ? "no selector — catches the rest" : "no selector — matches every alert";
    }
    return node.detail || "";
  }

  // The template a route renders with is a label, not a node: rendering is not
  // a step the alert takes towards a channel.
  function templateLine(node) {
    if ((node.kind !== "route" && node.kind !== "endpoint") || !node.template) return "";
    if (node.template_missing) return "renders with a deleted template";
    return "renders with " + node.template + (node.template_inherited ? " (inherited)" : "");
  }

  // What an edge means, spelled out rather than left to the reader.
  function edgeMeaning(link) {
    switch (link.kind) {
      case "enters":
        return "alerts arriving here are offered to this route";
      case "refines":
        return link.greedy ? "child route, delivers instead of its parent" : "child route, delivers as well as its parent";
      case "delivers":
        return "delivers here";
      case "direct":
        return "posts here without routing";
      case "renders":
        return "renders with this template";
      case "default":
        return "default template of this webhook";
      default:
        return "";
    }
  }

  function dashOf(link) {
    if (link.kind === "refines") return "4 3";
    if (link.kind === "direct") return "1 3";
    return null;
  }

  // Server columns become levels and server rows the starting order within them; the
  // tangled tree only needs every link to point to a later level.
  function levelsOf(nodes, links) {
    const columns = Array.from(new Set(nodes.map((n) => n.x))).sort((a, b) => a - b);
    const rank = new Map(columns.map((x, i) => [x, i]));
    const byID = new Map(nodes.map((n) => [n.id, n]));
    const parents = new Map();
    const kept = new Map();
    links.forEach((link) => {
      const from = byID.get(link.source);
      const to = byID.get(link.target);
      if (!from || !to) return;
      if (rank.get(from.x) >= rank.get(to.x)) {
        console.warn("routing graph: skipped a link that points against the flow", link);
        return;
      }
      if (!parents.has(to.id)) parents.set(to.id, []);
      if (!parents.get(to.id).includes(from.id)) parents.get(to.id).push(from.id);
      kept.set(from.id + ">" + to.id, link);
    });
    const levels = columns.map(() => []);
    nodes
      .slice()
      .sort((a, b) => a.y - b.y)
      .forEach((n) => levels[rank.get(n.x)].push({ id: n.id, parents: parents.get(n.id) || [] }));
    return { levels, links: kept };
  }

  function glyphOf(node) {
    return node.missing ? "missing" : node.kind;
  }

  function el(tag, className, text) {
    const e = document.createElement(tag);
    if (className) e.className = className;
    if (text) e.textContent = text;
    return e;
  }

  // Labels, selectors and channel names are user data: textContent only.
  function nodeCard(node) {
    const card = [];
    card.push(el("div", "text-[10px] font-semibold uppercase tracking-wide text-slate-500", node.synthetic ? "Built-in route" : kindNames[node.kind] || node.kind));
    card.push(el("div", "font-semibold text-slate-900 break-words", node.label));
    const sub = subtitle(node);
    if (sub) card.push(el("div", (node.kind === "route" && node.selector ? "font-mono " : "") + "text-slate-600 break-words", sub));
    const template = templateLine(node);
    if (template) card.push(el("div", node.template_missing ? "text-red-700" : "text-slate-500 italic", template));
    if (node.kind === "route" && !node.synthetic) {
      const facts = ["priority " + (node.priority || 0)];
      if (node.default) facts.push("default route");
      if (node.greedy) facts.push("delivers instead of the route it refines");
      card.push(el("div", "text-slate-500", facts.join(" · ")));
    }
    if (node.synthetic) card.push(el("div", "text-slate-500", "Change it by making another destination the global default."));
    if (node.missing) card.push(el("div", "text-red-700", "Missing: referenced by a route but no longer exists."));
    if (node.app_missing) card.push(el("div", "text-amber-800", "Warning: the Teams app is not installed in this team, so nothing arrives."));
    card.push(el("div", "mt-1 text-[10px] text-slate-400", "Click to show everything connected to it"));
    return card;
  }

  function linkCard(from, to, link) {
    return [
      el("div", "text-[10px] font-semibold uppercase tracking-wide text-slate-500", "Link"),
      el("div", "font-semibold text-slate-900 break-words", from.label + " → " + to.label),
      el("div", "text-slate-600", edgeMeaning(link)),
    ];
  }

  function legendRoot(colour, text) {
    const li = el("li", "flex items-center gap-2");
    const swatch = el("span", "inline-block h-0.5 w-5 shrink-0 rounded");
    swatch.style.background = colour;
    li.append(swatch, el("span", "truncate", text));
    return li;
  }

  // A click redraws the graph with only what leads to the node and from it, laid
  // out afresh, so its paths read without the rest of the graph crossing them.
  function drawGraph(container, nodes, rawLinks) {
    const left = Math.min(...nodes.map((n) => n.x));
    const roots = nodes.filter((n) => n.x === left).sort((a, b) => a.y - b.y);
    // Colours come from the whole graph, so a redrawn part keeps them.
    const colours = new Map(roots.map((n, i) => [n.id, i < palette.length ? palette[i] : otherColour]));

    // The legend names the colours, since only the graph knows which roots there are.
    const legend = container.parentElement.querySelector("[data-legend-roots]");
    if (legend) {
      legend.replaceChildren(...roots.slice(0, palette.length).map((n) => legendRoot(colours.get(n.id), n.label)));
      if (roots.length > palette.length) legend.appendChild(legendRoot(otherColour, "+" + (roots.length - palette.length) + " more"));
    }

    // Everything an alert could take to reach the node, and everything it reaches from it.
    function reachOf(id) {
      const ids = new Set([id]);
      const links = [];
      [["source", "target"], ["target", "source"]].forEach(([from, to]) => {
        const queue = [id];
        const seen = new Set(queue);
        while (queue.length) {
          const at = queue.shift();
          rawLinks.forEach((l) => {
            if (l[from] !== at) return;
            if (!links.includes(l)) links.push(l);
            ids.add(l[to]);
            if (!seen.has(l[to])) {
              seen.add(l[to]);
              queue.push(l[to]);
            }
          });
        }
      });
      return { nodes: nodes.filter((n) => ids.has(n.id)), links };
    }

    const showAll = container.parentElement.querySelector("[data-show-all]");
    let focus = null;
    let current = null;
    function show(id) {
      focus = id;
      const part = id ? reachOf(id) : { nodes, links: rawLinks };
      current = render(container, part.nodes, part.links, { colours, focus: id, onSelect: show, onReset: () => show(null) });
      if (showAll) showAll.hidden = !id;
    }
    if (showAll) showAll.addEventListener("click", () => show(null));
    show(null);

    return {
      edgeData: rawLinks,
      reset: () => (focus ? show(null) : current.reset()),
      fitAll: () => current.fitAll(),
      resized: () => current.resized(),
      highlight(set) {
        if (focus) show(null);
        current.highlight(set);
      },
    };
  }

  function render(container, nodes, rawLinks, view) {
    const byID = new Map(nodes.map((n) => [n.id, n]));
    const { levels, links: kept } = levelsOf(nodes, rawLinks);
    // The global default and default routes go first, so their line to the default channel runs straight.
    const pinned = nodes.filter((n) => n.kind === "route" && (n.synthetic || n.default)).map((n) => n.id);
    const tangle = window.teamsterTangle(window.teamsterTangleOrder(levels, { pinned }), geometry);
    const keyOf = (l) => l.target.id + ">" + l.source.id;

    container.replaceChildren();
    const svg = d3.select(container).append("svg").attr("class", "block h-full w-full").style("cursor", "grab");
    const root = svg.append("g");
    let moved = false;
    const zoom = d3
      .zoom()
      .scaleExtent([0.15, 4])
      .on("zoom", (event) => {
        root.attr("transform", event.transform);
        if (event.sourceEvent) moved = true;
      });
    svg.call(zoom).on("dblclick.zoom", null);
    // Whatever was under the pointer moves away when the view does.
    zoom.on("start", () => hideCard());

    const card = el("div", "pointer-events-none absolute z-30 hidden max-w-xs space-y-0.5 rounded-md border border-slate-200 bg-white px-3 py-2 text-xs shadow-lg");
    container.appendChild(card);

    function showCard(event, rows) {
      card.replaceChildren(...rows);
      card.classList.remove("hidden");
      moveCard(event);
    }

    function moveCard(event) {
      const box = container.getBoundingClientRect();
      let x = event.clientX - box.left + 14;
      let y = event.clientY - box.top + 14;
      if (x + card.offsetWidth > box.width - 8) x = event.clientX - box.left - card.offsetWidth - 14;
      if (y + card.offsetHeight > box.height - 8) y = event.clientY - box.top - card.offsetHeight - 14;
      card.style.left = Math.max(8, x) + "px";
      card.style.top = Math.max(8, y) + "px";
    }

    function hideCard() {
      card.classList.add("hidden");
    }

    // One heading per level, above its first node: a child column is a different step.
    let routeLevels = 0;
    const headingData = tangle.levels.map((level) => {
      const first = byID.get(level[0].id);
      let text = headings[first.kind] || first.kind;
      if (first.kind === "route" && container === flowContainer) {
        routeLevels += 1;
        if (routeLevels > 1) text = "Child routes";
      }
      return { text, x: level[0].x, y: level[0].y - level[0].height / 2 - 30 };
    });
    root
      .append("g")
      .selectAll("text")
      .data(headingData)
      .join("text")
      .attr("x", (d) => d.x)
      .attr("y", (d) => d.y)
      .attr("font-size", 11)
      .attr("font-weight", 600)
      .attr("letter-spacing", "0.04em")
      .attr("fill", "#64748b")
      .attr("stroke", surface)
      .attr("stroke-width", 4)
      .attr("paint-order", "stroke")
      .text((d) => d.text.toUpperCase());

    // Which first-column nodes reach each node: a webhook, endpoint or template.
    const colourOf = view.colours;
    const rootsOf = new Map();
    tangle.levels.forEach((level, i) =>
      level.forEach((n) => {
        if (i === 0) {
          rootsOf.set(n.id, [n.id]);
          return;
        }
        const reached = new Set();
        n.parents.forEach((p) => rootsOf.get(p.id).forEach((r) => reached.add(r)));
        rootsOf.set(n.id, Array.from(reached));
      }),
    );
    // One strand per root a link carries alerts from, side by side like metro lines.
    // Roots past the palette share one grey strand.
    function strandsOf(l) {
      const colours = [];
      rootsOf.get(l.target.id).forEach((r) => {
        // A part drawn without its webhook starts at a node the whole graph gave no colour.
        const colour = colourOf.get(r) || otherColour;
        if (!colours.includes(colour)) colours.push(colour);
      });
      if (colours.length === 0) colours.push(otherColour);
      return colours.map((colour, i) => ({ l, colour, offset: (i - (colours.length - 1) / 2) * strandGap }));
    }

    const linkGroup = root.append("g").attr("fill", "none");
    const link = linkGroup
      .selectAll("g")
      .data(tangle.links)
      .join("g")
      .style("cursor", "help");
    // A casing in the surface colour keeps crossings apart, as in the original.
    link
      .append("path")
      .attr("d", window.teamsterTanglePath)
      .attr("stroke", surface)
      .attr("stroke-width", (l) => 3.5 + strandsOf(l).length * strandGap);
    // Horizontal runs shift down and vertical runs right by the same offset, so one translate moves both.
    link
      .selectAll("path[data-line]")
      .data(strandsOf)
      .join("path")
      .attr("data-line", "")
      .attr("d", (d) => window.teamsterTanglePath(d.l))
      .attr("transform", (d) => "translate(" + d.offset + "," + d.offset + ")")
      .attr("stroke", (d) => d.colour)
      .attr("stroke-width", 1.5)
      .attr("stroke-dasharray", (d) => dashOf(kept.get(keyOf(d.l)) || {}));
    // "Instead of" ends in a bar, so ADR 0096's distinction stays visible without hovering.
    link
      .filter((l) => {
        const raw = kept.get(keyOf(l));
        return raw && raw.kind === "refines" && raw.greedy;
      })
      .append("path")
      .attr("d", (l) => "M" + (l.xs - 3) + " " + (l.ys - 5) + "v10")
      .attr("stroke", "#475569")
      .attr("stroke-width", 2.5);
    link
      .append("path")
      .attr("d", window.teamsterTanglePath)
      .attr("stroke", "transparent")
      .attr("stroke-width", 10)
      .on("mouseenter", (event, l) => showCard(event, linkCard(byID.get(l.target.id), byID.get(l.source.id), kept.get(keyOf(l)) || {})))
      .on("mousemove", moveCard)
      .on("mouseleave", hideCard);

    const node = root
      .append("g")
      .selectAll("g")
      .data(tangle.nodes)
      .join("g")
      .attr("transform", (d) => "translate(" + d.x + "," + d.y + ")")
      .attr("tabindex", 0)
      .attr("role", "button")
      .attr("aria-label", (d) => (kindNames[byID.get(d.id).kind] || "") + " " + byID.get(d.id).label)
      .style("cursor", "pointer")
      .style("outline", "none");

    // The station: where the lines of one node meet. It grows with them.
    node
      .append("path")
      .attr("d", (d) => "M0 " + -d.height / 2 + "V" + d.height / 2)
      .attr("stroke", lineLit)
      .attr("stroke-width", 8)
      .attr("stroke-linecap", "round");
    node
      .append("path")
      .attr("d", (d) => "M0 " + -d.height / 2 + "V" + d.height / 2)
      .attr("stroke", "#ffffff")
      .attr("stroke-width", 4)
      .attr("stroke-linecap", "round");

    const top = (d) => -d.height / 2;
    node
      .append("use")
      .attr("href", (d) => "#glyph-" + glyphOf(byID.get(d.id)))
      .attr("class", (d) => "graph-kind-" + glyphOf(byID.get(d.id)))
      // A first-column node wears its line colour, which is how a line is traced back.
      .style("color", (d) => (colourOf.has(d.id) && !byID.get(d.id).missing ? colourOf.get(d.id) : null))
      .attr("x", 6)
      .attr("y", (d) => top(d) - 22)
      .attr("width", 15)
      .attr("height", 15)
      .attr("fill", "none")
      .attr("stroke", "currentColor")
      .attr("stroke-width", 1.75)
      .attr("stroke-linecap", "round")
      .attr("stroke-linejoin", "round");

    const label = node
      .append("text")
      .attr("x", 25)
      .attr("y", (d) => top(d) - 10)
      .attr("font-size", 13)
      .attr("font-weight", 500)
      .attr("fill", (d) => (byID.get(d.id).missing ? "#b91c1c" : ink))
      .attr("stroke", surface)
      .attr("stroke-width", 4)
      .attr("paint-order", "stroke")
      .text((d) => {
        const n = byID.get(d.id);
        const suffix = n.kind === "route" && !n.synthetic ? (n.default ? " · default" : " · p" + (n.priority || 0)) : "";
        return clip(n.label, 28 - suffix.length) + suffix;
      });

    // A status glyph after the label, so a warning is never colour alone.
    node
      .filter((d) => byID.get(d.id).app_missing)
      .append("use")
      .attr("href", "#glyph-app_missing")
      .attr("class", "graph-kind-app_missing")
      .attr("y", (d) => top(d) - 22)
      .attr("width", 14)
      .attr("height", 14)
      .attr("fill", "none")
      .attr("stroke", "currentColor")
      .attr("stroke-width", 1.75)
      .attr("stroke-linejoin", "round")
      .attr("x", function (d) {
        const text = label.filter((other) => other.id === d.id).node();
        return 29 + (text ? text.getComputedTextLength() : 0);
      });

    // A hit area larger than the marks, so the icon, label and station all answer.
    node
      .insert("rect", ":first-child")
      .attr("x", -8)
      .attr("y", (d) => top(d) - 21)
      .attr("width", geometry.nodeWidth - 24)
      .attr("height", (d) => d.height + 26)
      .attr("fill", "transparent");

    // Hovering previews what a click would focus, unless something is focused already.
    node
      .on("mouseenter", (event, d) => {
        showCard(event, nodeCard(byID.get(d.id)));
        if (!lit) paint(connected(d.id));
      })
      .on("mousemove", moveCard)
      .on("mouseleave", () => {
        hideCard();
        if (!lit) paint(null);
      })
      .on("click", (event, d) => {
        event.stopPropagation();
        view.onSelect(d.id);
      })
      .on("keydown", (event, d) => {
        if (event.key === "Enter" || event.key === " ") {
          event.preventDefault();
          view.onSelect(d.id);
        }
      })
      .on("focus", function (event, d) {
        d3.select(this).select("text").attr("text-decoration", "underline");
      })
      .on("blur", function () {
        d3.select(this).select("text").attr("text-decoration", null);
      });

    svg.on("click", () => (view.focus ? view.onReset() : reset()));

    const parentsOf = new Map();
    const childrenOf = new Map();
    tangle.links.forEach((l) => {
      const key = keyOf(l);
      if (!childrenOf.has(l.target.id)) childrenOf.set(l.target.id, []);
      childrenOf.get(l.target.id).push([l.source.id, key]);
      if (!parentsOf.has(l.source.id)) parentsOf.set(l.source.id, []);
      parentsOf.get(l.source.id).push([l.target.id, key]);
    });

    // Everything an alert could take to reach the node, and everything it reaches from it.
    function connected(id) {
      const nodeIDs = new Set([id]);
      const linkIDs = new Set();
      [childrenOf, parentsOf].forEach((next) => {
        const queue = [id];
        while (queue.length) {
          const at = queue.shift();
          (next.get(at) || []).forEach(([other, key]) => {
            linkIDs.add(key);
            if (!nodeIDs.has(other)) {
              nodeIDs.add(other);
              queue.push(other);
            }
          });
        }
      });
      return { nodes: nodeIDs, links: linkIDs };
    }

    let lit = null;
    let selected = view.focus;

    function paint(preview) {
      const shown = preview === undefined ? lit : preview;
      const on = (d) => !shown || shown.nodes.has(d.id);
      const onLink = (l) => !shown || shown.links.has(keyOf(l));
      // Faded context stays legible enough to orient by; faded links get out of the way.
      node.attr("opacity", (d) => (on(d) ? 1 : 0.3));
      label.attr("font-weight", (d) => (d.id === selected ? 700 : 500));
      link.attr("opacity", (l) => (onLink(l) ? 1 : 0.12));
      link
        .selectAll("[data-line]")
        .attr("stroke-width", (d) => (shown && onLink(d.l) ? 2.25 : 1.5));
      if (shown) link.filter(onLink).raise();
    }

    function boxOf(ids) {
      let x0 = Infinity;
      let y0 = Infinity;
      let x1 = -Infinity;
      let y1 = -Infinity;
      tangle.nodes.forEach((d) => {
        if (ids && !ids.has(d.id)) return;
        x0 = Math.min(x0, d.x - 8);
        x1 = Math.max(x1, d.x + geometry.nodeWidth - 24);
        y0 = Math.min(y0, d.y - d.height / 2 - 30);
        y1 = Math.max(y1, d.y + d.height / 2 + 8);
      });
      if (!ids) y0 -= 24;
      return { x: x0, y: y0, w: x1 - x0, h: y1 - y0 };
    }

    // The widest vertical band of the view no panel covers, so a fitted graph is
    // not hidden behind one; when the panels leave too little, the whole view.
    function freeBand() {
      const w = container.clientWidth;
      const area = container.getBoundingClientRect();
      const edges = [0, w];
      const covered = [];
      container.parentElement.querySelectorAll("[data-floating-panel]").forEach((panel) => {
        if (panel.hidden) return;
        const r = panel.getBoundingClientRect();
        covered.push([r.left - area.left, r.right - area.left]);
        edges.push(r.left - area.left, r.right - area.left);
      });
      let best = { left: 0, width: 0 };
      edges.forEach((left) =>
        edges.forEach((right) => {
          if (right - left <= best.width || left < 0 || right > w) return;
          if (covered.some(([a, b]) => a < right && b > left)) return;
          best = { left, width: right - left };
        }),
      );
      return best.width >= w * 0.4 ? best : { left: 0, width: w };
    }

    // Fit the box, but never shrink labels past reading: a tall box scrolls from its top.
    function fit(box, animate) {
      const band = freeBand();
      const w = band.width;
      const h = container.clientHeight;
      if (!w || !h || !isFinite(box.w)) return;
      const pad = 40;
      const k = Math.max(minScale, Math.min(maxScale, (w - 2 * pad) / box.w, (h - 2 * pad) / box.h));
      const tx = band.left + (box.w * k <= w - 2 * pad ? (w - box.w * k) / 2 - box.x * k : pad - box.x * k);
      const ty = box.h * k <= h - 2 * pad ? (h - box.h * k) / 2 - box.y * k : pad - box.y * k;
      const transform = d3.zoomIdentity.translate(tx, ty).scale(k);
      (animate ? svg.transition().duration(450) : svg).call(zoom.transform, transform);
      moved = false;
    }

    function reset() {
      if (!lit) return;
      lit = null;
      selected = null;
      paint();
      fit(boxOf(null), true);
    }

    fit(boxOf(null), false);
    if (view.focus) {
      paint();
      // The redraw replaced the node a keyboard user was on.
      const focused = node.filter((d) => d.id === view.focus).node();
      if (focused) focused.focus({ preventScroll: true });
    }

    return {
      reset,
      fitAll: () => fit(boxOf(null), true),
      // A resize keeps a view the user chose and refits one they did not.
      resized: () => {
        if (!moved) fit(lit ? boxOf(lit.nodes) : boxOf(null), false);
      },
      highlight(set) {
        selected = null;
        lit = set;
        paint();
        fit(set ? boxOf(set.nodes) : boxOf(null), true);
      },
    };
  }

  // A matched route is only half the answer: what an operator wants to see is
  // where that alert ends up. An alert can fan out across a tree of routes, so
  // the path is every node the server says it reaches, plus the edges between
  // them and the routes they were reached through. A root has one parent per
  // webhook, so only the webhooks the server named are on the path.
  function pathOf(matched) {
    const nodeIDs = new Set(matched);

    const parents = new Map();
    flow.edgeData.forEach((link) => {
      if (link.kind !== "refines") return;
      parents.set(link.target, link.source);
    });
    matched.forEach((id) => {
      let walked = 0;
      for (let at = parents.get(id); at && walked < 16; at = parents.get(at)) {
        nodeIDs.add(at);
        walked += 1;
      }
    });

    const linkIDs = new Set();
    flow.edgeData.forEach((link) => {
      if (nodeIDs.has(link.source) && nodeIDs.has(link.target)) {
        linkIDs.add(link.source + ">" + link.target);
      }
    });
    return { nodes: nodeIDs, links: linkIDs };
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

      const matched = payload && Array.isArray(payload.nodes) ? payload.nodes : [];
      report(payload && payload.explanation ? payload.explanation : "No explanation came back.", matched.length > 0);
      if (flow) flow.highlight(matched.length > 0 ? pathOf(matched) : null);
    });
  }

  async function load(endpoint) {
    const res = await fetch(endpoint);
    if (!res.ok) throw new Error("status " + res.status);
    const payload = await res.json();
    return {
      nodes: payload && Array.isArray(payload.nodes) ? payload.nodes : [],
      links: payload && Array.isArray(payload.links) ? payload.links : [],
    };
  }

  // A hidden tab has no size to lay out in, so a graph is drawn the first time
  // its container has one and refitted whenever that changes.
  function drawWhenSized(container, draw) {
    let graph = null;
    const fitButton = container.parentElement.querySelector("[data-fit]");
    if (fitButton) fitButton.addEventListener("click", () => graph && graph.fitAll());
    new ResizeObserver(() => {
      if (!container.clientWidth || !container.clientHeight) return;
      if (graph) {
        graph.resized();
        return;
      }
      graph = draw();
    }).observe(container);
    return () => graph;
  }

  const graphs = [];
  document.addEventListener("keydown", (event) => {
    if (event.key !== "Escape" || event.defaultPrevented) return;
    graphs.forEach((current) => {
      const graph = current();
      if (graph) graph.reset();
    });
  });

  (async () => {
    let data;
    try {
      data = await load("/api/routing/graph");
    } catch (err) {
      empty(flowContainer, "Nothing to draw yet. Create a route, a destination and a template first.");
      data = null;
    }

    // The webhooks are always there, so they alone are not a graph worth drawing.
    if (data && data.nodes.filter((node) => node.kind !== "source").length === 0) {
      empty(flowContainer, "Nothing to draw yet. Create a route, a destination and a template first.");
    } else if (data) {
      graphs.push(
        drawWhenSized(flowContainer, () => {
          flow = drawGraph(flowContainer, data.nodes, data.links);
          return flow;
        }),
      );
    }

    if (!templateContainer) return;
    try {
      const templates = await load("/api/routing/templates");
      if (templates.nodes.length === 0) {
        empty(templateContainer, "No templates yet.");
        return;
      }
      graphs.push(drawWhenSized(templateContainer, () => drawGraph(templateContainer, templates.nodes, templates.links)));
    } catch (err) {
      empty(templateContainer, "The template graph could not be loaded.");
    }
  })();

  // Panels over the graph: moved by their header, folded, hidden and brought
  // back, and left where they were across visits. Storage may be unavailable.
  (function panels() {
    const key = "teamster.routing.panels";
    let saved = {};
    try {
      saved = JSON.parse(localStorage.getItem(key)) || {};
    } catch (e) {
      saved = {};
    }
    function save() {
      try {
        localStorage.setItem(key, JSON.stringify(saved));
      } catch (e) {
        // Remembering is a convenience; the panel still works.
      }
    }

    document.querySelectorAll("[data-floating-panel]").forEach((panel) => {
      const id = panel.dataset.floatingPanel;
      // A legend starts folded: it is looked up once, and open it covers the graph.
      const state = (saved[id] = saved[id] || { folded: panel.hasAttribute("data-panel-folded") });
      const area = panel.parentElement;
      const handle = panel.querySelector("[data-panel-handle]");
      const body = panel.querySelector("[data-panel-body]");
      const fold = panel.querySelector("[data-panel-fold]");
      const hide = panel.querySelector("[data-panel-hide]");
      const show = area.querySelector('[data-panel-show="' + id + '"]');

      function place(x, y) {
        if (!area.clientWidth) return;
        const left = Math.max(0, Math.min(x, area.clientWidth - panel.offsetWidth));
        const top = Math.max(0, Math.min(y, area.clientHeight - panel.offsetHeight));
        panel.style.left = left + "px";
        panel.style.top = top + "px";
        panel.style.right = "auto";
        panel.style.bottom = "auto";
      }

      function apply() {
        body.hidden = !!state.folded;
        fold.setAttribute("aria-expanded", String(!state.folded));
        panel.hidden = !!state.hidden;
        if (show) show.hidden = !state.hidden;
        if (state.x !== undefined) place(state.x, state.y);
      }

      fold.addEventListener("click", () => {
        state.folded = !state.folded;
        apply();
        save();
      });
      hide.addEventListener("click", () => {
        state.hidden = true;
        apply();
        save();
        if (show) show.focus();
      });
      if (show) {
        show.addEventListener("click", () => {
          state.hidden = false;
          apply();
          save();
          hide.focus();
        });
      }

      handle.addEventListener("pointerdown", (event) => {
        if (event.button !== 0 || event.target.closest("button")) return;
        const startX = event.clientX - panel.offsetLeft;
        const startY = event.clientY - panel.offsetTop;
        handle.setPointerCapture(event.pointerId);
        const move = (e) => place(e.clientX - startX, e.clientY - startY);
        const up = () => {
          handle.removeEventListener("pointermove", move);
          handle.removeEventListener("pointerup", up);
          handle.removeEventListener("pointercancel", up);
          state.x = panel.offsetLeft;
          state.y = panel.offsetTop;
          save();
        };
        handle.addEventListener("pointermove", move);
        handle.addEventListener("pointerup", up);
        handle.addEventListener("pointercancel", up);
      });

      // Keeps a remembered position inside the area when the window shrinks or the tab first shows.
      new ResizeObserver(() => {
        if (state.x !== undefined) place(state.x, state.y);
      }).observe(area);
      apply();
    });
  })();

  // The help opens on hover too; a click pins it open until clicked again or Esc.
  (function help() {
    const button = document.querySelector("[data-help-toggle]");
    const popover = document.getElementById("routing-help");
    if (!button || !popover || !popover.showPopover) return;
    let byHover = false;
    let closing = 0;
    const open = () => popover.matches(":popover-open");
    const later = () => {
      clearTimeout(closing);
      closing = setTimeout(() => {
        if (byHover && open()) popover.hidePopover();
        byHover = false;
      }, 200);
    };
    button.addEventListener("mouseenter", () => {
      clearTimeout(closing);
      if (open()) return;
      byHover = true;
      popover.showPopover();
    });
    button.addEventListener("mouseleave", later);
    popover.addEventListener("mouseenter", () => clearTimeout(closing));
    popover.addEventListener("mouseleave", later);
    button.addEventListener("click", (event) => {
      if (!byHover) return;
      // Opened by hovering: the click pins it rather than toggling it shut.
      event.preventDefault();
      byHover = false;
      clearTimeout(closing);
    });
  })();
})();
