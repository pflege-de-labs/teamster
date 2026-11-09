// Tangled tree layout, ported from "Tangled tree visualization II" by nitaku:
// https://observablehq.com/@nitaku/tangled-tree-visualization-ii
// Changes: no d3 or lodash, tunable spacing, guarded empty levels, an optional
// compact mode with rising links (ADR 0097), and an ordering pass (ADR 0100) and
// lanes for links that skip levels (ADR 0102), which are not in the original.
//
// Copyright 2018 nitaku
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of this software and
// associated documentation files (the "Software"), to deal in the Software without restriction,
// including without limitation the rights to use, copy, modify, merge, publish, distribute,
// sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all copies or
// substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT
// NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND
// NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM,
// DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT
// OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.
(function (global) {
  function max(items, of) {
    let best;
    items.forEach((item) => {
      const v = of(item);
      if (v !== undefined && (best === undefined || v > best)) best = v;
    });
    return best;
  }

  function min(items, of) {
    let best;
    items.forEach((item) => {
      const v = of(item);
      if (v !== undefined && (best === undefined || v < best)) best = v;
    });
    return best;
  }

  // levels: [[{id, parents: [id, ...]}, ...], ...]; a parent sits on an earlier level.
  // The input is not modified.
  function tangle(input, options) {
    const opts = Object.assign(
      {
        padding: 8,
        nodeHeight: 22,
        nodeWidth: 70,
        bundleWidth: 14,
        levelYPadding: 16,
        metroD: 4,
        minFamilyHeight: 22,
        c: 16,
        // compact top-aligns every level instead of stacking each below the last;
        // links may then rise as well as fall.
        compact: false,
      },
      options || {},
    );
    if (opts.bigc === undefined) opts.bigc = opts.nodeWidth + opts.c;

    const levels = input.map((level) => level.map((n) => Object.assign({}, n)));
    levels.forEach((l, i) => l.forEach((n) => (n.level = i)));

    const nodes = levels.reduce((a, x) => a.concat(x), []);
    const nodesIndex = {};
    nodes.forEach((d) => (nodesIndex[d.id] = d));

    nodes.forEach((d) => {
      d.parents = (d.parents || []).map((p) => nodesIndex[p]).filter(Boolean);
    });

    levels.forEach((l, i) => {
      const index = {};
      l.forEach((n) => {
        if (n.parents.length === 0) return;
        const id = n.parents
          .map((d) => d.id)
          .sort()
          .join("-X-");
        if (id in index) {
          index[id].parents = index[id].parents.concat(n.parents);
        } else {
          index[id] = { id: id, parents: n.parents.slice(), level: i, span: i - min(n.parents, (p) => p.level) };
        }
        n.bundle = index[id];
      });
      l.bundles = Object.keys(index).map((k) => index[k]);
      l.bundles.forEach((b, i) => (b.i = i));
    });

    const links = [];
    nodes.forEach((d) => {
      d.parents.forEach((p) => links.push({ source: d, bundle: d.bundle, target: p }));
    });

    const bundles = levels.reduce((a, x) => a.concat(x.bundles), []);

    // Reverse pointer from parent to bundles. bundlesIndex values are arrays, and
    // their own .i (set below) places the metro line, distinct from bundle.i.
    bundles.forEach((b) =>
      b.parents.forEach((p) => {
        if (p.bundlesIndex === undefined) p.bundlesIndex = {};
        if (!(b.id in p.bundlesIndex)) p.bundlesIndex[b.id] = [];
        p.bundlesIndex[b.id].push(b);
      }),
    );

    nodes.forEach((n) => {
      if (n.bundlesIndex !== undefined) {
        n.bundles = Object.keys(n.bundlesIndex).map((k) => n.bundlesIndex[k]);
      } else {
        n.bundlesIndex = {};
        n.bundles = [];
      }
      n.bundles.sort((a, b) => max(b, (d) => d.span) - max(a, (d) => d.span));
      n.bundles.forEach((b, i) => (b.i = i));
    });

    links.forEach((l) => {
      if (l.bundle.links === undefined) l.bundle.links = [];
      l.bundle.links.push(l);
    });

    const { padding, nodeHeight, nodeWidth, bundleWidth, levelYPadding, metroD, minFamilyHeight, c } = opts;

    nodes.forEach((n) => (n.height = (Math.max(1, n.bundles.length) - 1) * metroD));

    let xOffset = padding;
    let yOffset = padding;
    levels.forEach((l) => {
      xOffset += l.bundles.length * bundleWidth;
      if (opts.compact) yOffset = padding;
      yOffset += levelYPadding;
      l.forEach((n) => {
        // A lane carries no label, so it needs less room than a node.
        const row = n.rowHeight || nodeHeight;
        n.x = n.level * nodeWidth + xOffset;
        n.y = row + yOffset + n.height / 2;
        yOffset += row + n.height;
      });
    });

    let i = 0;
    levels.forEach((l) => {
      l.bundles.forEach((b) => {
        b.x = max(b.parents, (d) => d.x) + nodeWidth + (l.bundles.length - 1 - b.i) * bundleWidth;
        b.y = i * nodeHeight;
      });
      i += l.length;
    });

    function metroY(l) {
      return l.target.y + l.target.bundlesIndex[l.bundle.id].i * metroD - (l.target.bundles.length * metroD) / 2 + metroD / 2;
    }

    links.forEach((l) => {
      l.xt = l.target.x;
      l.yt = metroY(l);
      l.xb = l.bundle.x;
      l.yb = l.bundle.y;
      l.xs = l.source.x;
      l.ys = l.source.y;
    });

    // Compress vertical space. The original relied on d3.min of nothing being
    // undefined, which turned the sum into NaN and then 0.
    let yNegativeOffset = 0;
    levels.forEach((l) => {
      if (opts.compact) return;
      const gap = min(l.bundles, (b) => min(b.links, (link) => link.ys - 2 * c - (link.yt + c)));
      yNegativeOffset += gap === undefined ? 0 : -minFamilyHeight + gap;
      l.forEach((n) => (n.y -= yNegativeOffset));
    });

    links.forEach((l) => {
      l.yt = metroY(l);
      l.ys = l.source.y;
      if (opts.compact) {
        // Both bends must fit into the rise or fall, or the arcs overshoot.
        const half = Math.abs(l.ys - l.yt) / 2;
        l.c1 = Math.min(c, half);
        l.c2 = Math.min(c, half);
      } else {
        l.c1 = l.source.level - l.target.level > 1 ? Math.min(opts.bigc, l.xb - l.xt, l.yb - l.yt) - c : c;
        l.c2 = c;
      }
    });

    const layout = {
      width: max(nodes, (n) => n.x) + nodeWidth + 2 * padding,
      height: max(nodes, (n) => n.y) + nodeHeight / 2 + 2 * padding,
    };

    return { levels, nodes, nodesIndex, links, bundles, layout };
  }

  // The SVG path of one link: across from the parent, down the bundle, into the child.
  function tanglePath(l) {
    if (l.ys < l.yt) {
      // Rising: the same shape mirrored, bending up and then right.
      return (
        "M" + l.xt + " " + l.yt +
        "L" + (l.xb - l.c1) + " " + l.yt +
        "A" + l.c1 + " " + l.c1 + " 90 0 0 " + l.xb + " " + (l.yt - l.c1) +
        "L" + l.xb + " " + (l.ys + l.c2) +
        "A" + l.c2 + " " + l.c2 + " 90 0 1 " + (l.xb + l.c2) + " " + l.ys +
        "L" + l.xs + " " + l.ys
      );
    }
    return (
      "M" + l.xt + " " + l.yt +
      "L" + (l.xb - l.c1) + " " + l.yt +
      "A" + l.c1 + " " + l.c1 + " 90 0 1 " + l.xb + " " + (l.yt + l.c1) +
      "L" + l.xb + " " + (l.ys - l.c2) +
      "A" + l.c2 + " " + l.c2 + " 90 0 0 " + (l.xb + l.c2) + " " + l.ys +
      "L" + l.xs + " " + l.ys
    );
  }

  // Reorders the levels so that fewer links cross: barycenter sweeps, as in Sugiyama's
  // method. The first level keeps its order, since callers colour by it, and pinned ids
  // lead their level. Index stands in for height because compact mode top-aligns levels.
  // Returns new levels; the input is not modified.
  function order(input, options) {
    const pinned = new Set((options && options.pinned) || []);
    const levels = input.map((level) => level.slice());
    const parentsOf = new Map();
    const childrenOf = new Map();
    levels.forEach((l) => l.forEach((n) => (parentsOf.set(n.id, []), childrenOf.set(n.id, []))));
    levels.forEach((l) =>
      l.forEach((n) =>
        (n.parents || []).forEach((p) => {
          if (!childrenOf.has(p)) return;
          parentsOf.get(n.id).push(p);
          childrenOf.get(p).push(n.id);
        }),
      ),
    );
    const index = new Map();
    const reindex = (l) => l.forEach((n, i) => index.set(n.id, i));
    levels.forEach(reindex);

    function sortBy(l, neighbours) {
      const key = new Map();
      l.forEach((n) => {
        const ns = neighbours.get(n.id);
        // A node with no neighbours this way keeps its place instead of floating to the top.
        key.set(n.id, ns.length ? ns.reduce((sum, id) => sum + index.get(id), 0) / ns.length : index.get(n.id));
      });
      // Ties keep the previous order, which starts as the server's priority order.
      l.sort((a, b) => pinned.has(b.id) - pinned.has(a.id) || key.get(a.id) - key.get(b.id) || index.get(a.id) - index.get(b.id));
      reindex(l);
    }

    for (let sweep = 0; sweep < 4; sweep++) {
      if (sweep > 0) for (let i = levels.length - 2; i >= 1; i--) sortBy(levels[i], childrenOf);
      for (let i = 1; i < levels.length; i++) sortBy(levels[i], parentsOf);
    }
    return levels;
  }

  // Gives a link that skips levels a lane node in every level it passes, so its line
  // runs in a row of its own instead of through the labels there: the dummy nodes of
  // Sugiyama's method. Links from one parent in one group share their lanes. A lane
  // has lane: true, the source it starts at and the targets it leads to.
  // Returns new levels; the input is not modified.
  function lanes(input, options) {
    const group = (options && options.group) || (() => "");
    const rowHeight = options && options.rowHeight;
    const levels = input.map((level) => level.map((n) => Object.assign({}, n, { parents: (n.parents || []).slice() })));
    const levelOf = new Map();
    levels.forEach((l, i) => l.forEach((n) => levelOf.set(n.id, i)));
    const made = new Map();
    levels.forEach((l, i) =>
      l.forEach((n) => {
        if (n.lane) return;
        n.parents = n.parents.map((p) => {
          if (!levelOf.has(p) || i - levelOf.get(p) < 2) return p;
          const g = group(p, n.id);
          let prev = p;
          for (let k = levelOf.get(p) + 1; k < i; k++) {
            const id = "~" + p + "~" + g + "@" + k;
            if (!made.has(id)) {
              const lane = { id, parents: [prev], lane: true, source: p, targets: [] };
              if (rowHeight) lane.rowHeight = rowHeight;
              made.set(id, lane);
              levels[k].push(lane);
            }
            made.get(id).targets.push(n.id);
            prev = id;
          }
          return prev;
        });
      }),
    );
    return levels;
  }

  global.teamsterTangle = tangle;
  global.teamsterTangleLanes = lanes;
  global.teamsterTangleOrder = order;
  global.teamsterTanglePath = tanglePath;
})(typeof window !== "undefined" ? window : globalThis);
