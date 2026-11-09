// Tests the tangled tree layout in web/tangle.js; see ADR 0098.
// It lives outside web/ because everything in web/ is embedded and served.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/tangle.js", import.meta.url));
const tangle = globalThis.teamsterTangle;
const tanglePath = globalThis.teamsterTanglePath;
const order = globalThis.teamsterTangleOrder;

// Webhooks, routes, child routes and destinations, with a level skip and shared parents.
const levels = [
  [{ id: "alertmanager" }, { id: "universal" }, { id: "endpoint" }],
  [
    { id: "team-a", parents: ["alertmanager", "universal"] },
    { id: "team-b", parents: ["alertmanager", "universal"] },
    { id: "watchdog", parents: ["alertmanager"] },
    { id: "endpoint-route", parents: ["endpoint"] },
  ],
  [
    { id: "a-critical", parents: ["team-a"] },
    { id: "a-warning", parents: ["team-a"] },
    { id: "b-critical", parents: ["team-b"] },
  ],
  [
    { id: "oncall", parents: ["a-critical", "b-critical"] },
    { id: "alerts", parents: ["team-a", "watchdog"] },
    { id: "info", parents: ["a-warning"] },
    { id: "etl", parents: ["endpoint", "endpoint-route"] },
  ],
];

// What the notebook's own constructTangleLayout returns for these levels with its defaults,
// so the port cannot drift from the original. The -6 is the original's too.
const original = {
  nodes: [
    ["alertmanager", 8, 48, 4],
    ["universal", 8, 72, 0],
    ["endpoint", 8, 96, 4],
    ["team-a", 120, 142, 4],
    ["team-b", 120, 166, 0],
    ["watchdog", 120, 188, 0],
    ["endpoint-route", 120, 210, 0],
    ["a-critical", 218, 214, 0],
    ["a-warning", 218, 236, 0],
    ["b-critical", 218, 258, 0],
    ["oncall", 344, 328, 0],
    ["alerts", 344, 350, 0],
    ["info", 344, 372, 0],
    ["etl", 344, 394, 0],
  ],
  links: [
    ["alertmanager", "team-a", 8, 46, 106, 142, 16],
    ["universal", "team-a", 8, 72, 106, 142, 16],
    ["alertmanager", "team-b", 8, 46, 106, 166, 16],
    ["universal", "team-b", 8, 72, 106, 166, 16],
    ["alertmanager", "watchdog", 8, 50, 92, 188, 16],
    ["endpoint", "endpoint-route", 8, 98, 78, 210, 16],
    ["team-a", "a-critical", 120, 144, 204, 214, 16],
    ["team-a", "a-warning", 120, 144, 204, 236, 16],
    ["team-b", "b-critical", 120, 166, 190, 258, 16],
    ["a-critical", "oncall", 218, 214, 330, 328, 16],
    ["b-critical", "oncall", 218, 258, 330, 328, 16],
    ["team-a", "alerts", 120, 140, 218, 350, 64],
    ["watchdog", "alerts", 120, 188, 218, 350, 16],
    ["a-warning", "info", 218, 236, 302, 372, 16],
    ["endpoint", "etl", 8, 94, 190, 394, 70],
    ["endpoint-route", "etl", 120, 210, 190, 394, -6],
  ],
  layout: [430, 421],
};

test("the stacked layout matches the original", () => {
  const t = tangle(levels);
  assert.deepEqual(
    t.nodes.map((n) => [n.id, n.x, n.y, n.height]),
    original.nodes,
  );
  assert.deepEqual(
    t.links.map((l) => [l.target.id, l.source.id, l.xt, l.yt, l.xb, l.ys, l.c1]),
    original.links,
  );
  assert.deepEqual([t.layout.width, t.layout.height], original.layout);
});

test("the input is left as it was", () => {
  const before = JSON.stringify(levels);
  tangle(levels, { compact: true });
  assert.equal(JSON.stringify(levels), before);
});

test("the compact layout", async (t) => {
  const opts = { compact: true, nodeHeight: 29, nodeWidth: 210, bundleWidth: 7, c: 10 };
  const layout = tangle(levels, opts);
  const byID = layout.nodesIndex;

  await t.test("starts every level at the same height", () => {
    const tops = layout.levels.map((level) => level[0].y - level[0].height / 2);
    assert.equal(new Set(tops).size, 1);
  });

  await t.test("keeps the order within a level", () => {
    layout.levels.forEach((level) => {
      for (let i = 1; i < level.length; i++) assert.ok(level[i].y > level[i - 1].y, level[i].id);
    });
  });

  await t.test("puts every level right of the one before", () => {
    layout.links.forEach((l) => assert.ok(l.source.x > l.target.x, l.target.id + ">" + l.source.id));
    layout.links.forEach((l) => assert.ok(l.xb > l.xt && l.xb < l.xs, "bundle between " + l.target.id + " and " + l.source.id));
  });

  await t.test("keeps both bends inside the rise or fall", () => {
    layout.links.forEach((l) => {
      assert.ok(l.c1 >= 0 && l.c2 >= 0);
      assert.ok(l.c1 + l.c2 <= Math.abs(l.ys - l.yt) + 1e-9, l.target.id + ">" + l.source.id);
    });
  });

  await t.test("draws links that rise as well as fall", () => {
    // oncall heads its level, so b-critical, two rows down the level before, feeds it from below.
    assert.ok(byID["oncall"].y < byID["b-critical"].y);
    assert.ok(layout.links.some((l) => l.ys > l.yt));
  });

  await t.test("has no gaps in its geometry", () => {
    layout.nodes.forEach((n) => assert.ok(Number.isFinite(n.x) && Number.isFinite(n.y), n.id));
    layout.links.forEach((l) => assert.doesNotMatch(tanglePath(l), /NaN|undefined|Infinity/));
  });
});

test("a level whose nodes have no parents", () => {
  const t = tangle([[{ id: "a" }], [{ id: "b" }, { id: "c", parents: ["a"] }]]);
  t.nodes.forEach((n) => assert.ok(Number.isFinite(n.y), n.id));
});

test("a parent that is not in the levels is ignored", () => {
  const t = tangle([[{ id: "a" }], [{ id: "b", parents: ["a", "gone"] }]]);
  assert.equal(t.links.length, 1);
});

test("the path of a link", async (t) => {
  const link = { xt: 0, xb: 100, xs: 200, c1: 10, c2: 10 };
  const cases = [
    // The bends turn down then right when falling, up then right when rising.
    { name: "falling", yt: 0, ys: 50, sweeps: ["1", "0"] },
    { name: "rising", yt: 50, ys: 0, sweeps: ["0", "1"] },
  ];
  for (const tc of cases) {
    await t.test(tc.name, () => {
      const d = tanglePath({ ...link, yt: tc.yt, ys: tc.ys });
      assert.ok(d.startsWith("M0 " + tc.yt), d);
      assert.ok(d.endsWith("L200 " + tc.ys), d);
      const sweeps = Array.from(d.matchAll(/A\S+ \S+ 90 0 (\d)/g), (m) => m[1]);
      assert.deepEqual(sweeps, tc.sweeps);
    });
  }
});

// Pairs of links between the same two levels whose ends swap places.
function crossings(levels) {
  const at = new Map();
  levels.forEach((l, li) => l.forEach((n, i) => at.set(n.id, [li, i])));
  const links = levels.flat().flatMap((n) => (n.parents || []).map((p) => [at.get(p), at.get(n.id)]));
  let count = 0;
  links.forEach(([a, b], i) =>
    links.slice(i + 1).forEach(([c, d]) => {
      if (a[0] === c[0] && b[0] === d[0] && (a[1] - c[1]) * (b[1] - d[1]) < 0) count++;
    }),
  );
  return count;
}

const ids = (levels) => levels.map((l) => l.map((n) => n.id));

test("the order", async (t) => {
  // Two webhooks whose routes and channels the server listed the other way round.
  const crossed = [
    [{ id: "am" }, { id: "uni" }],
    [
      { id: "uni-route", parents: ["uni"] },
      { id: "am-route", parents: ["am"] },
      { id: "global-default", parents: ["am", "uni"] },
    ],
    [
      { id: "default", parents: ["global-default"] },
      { id: "uni-channel", parents: ["uni-route"] },
      { id: "am-channel", parents: ["am-route"] },
    ],
  ];

  await t.test("crosses fewer links", () => {
    assert.ok(crossings(crossed) > 0);
    assert.equal(crossings(order(crossed)), 0);
  });

  await t.test("keeps the first level, which colours the lines", () => {
    assert.deepEqual(ids(order(crossed))[0], ["am", "uni"]);
  });

  await t.test("puts pinned nodes first, and what they feed follows", () => {
    const got = ids(order(crossed, { pinned: ["global-default"] }));
    assert.deepEqual(got[1], ["global-default", "am-route", "uni-route"]);
    assert.deepEqual(got[2], ["default", "am-channel", "uni-channel"]);
  });

  await t.test("keeps the server's order between equals", () => {
    const tied = [[{ id: "am" }], [{ id: "p100", parents: ["am"] }, { id: "p50", parents: ["am"] }, { id: "p10", parents: ["am"] }]];
    assert.deepEqual(ids(order(tied))[1], ["p100", "p50", "p10"]);
  });

  await t.test("keeps a node without links where it was", () => {
    const lonely = [[{ id: "am" }, { id: "uni" }], [{ id: "orphan" }, { id: "b", parents: ["uni"] }, { id: "a", parents: ["am"] }]];
    assert.deepEqual(ids(order(lonely))[1], ["orphan", "a", "b"]);
  });

  await t.test("never crosses more than the server's order", () => {
    assert.ok(crossings(order(levels)) <= crossings(levels));
  });

  await t.test("leaves the input as it was", () => {
    const before = JSON.stringify(crossed);
    order(crossed, { pinned: ["global-default"] });
    assert.equal(JSON.stringify(crossed), before);
  });
});
