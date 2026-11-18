// Tests the pure logic of web/principal-picker.js: the filter and the arrow-key step.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/principal-picker.js", import.meta.url));
const filter = globalThis.teamsterFilterPrincipals;

const e = (value, label, search = "") => ({ value, label, kind: value.split(":")[0], search });
const all = [
  e("group:g1", "Ops", "ops team"),
  e("user:u2", "Bob", "bob@x.de u2"),
  e("user:u1", "Alice", "alice@y.de u1"),
];

test("filterPrincipals", () => {
  const cases = [
    { name: "label", q: "bob", want: ["user:u2"] },
    { name: "search field", q: "x.de", want: ["user:u2"] },
    { name: "case insensitive", q: "BOB", want: ["user:u2"] },
    { name: "all words needed", q: "ops bob", want: [] },
    { name: "both words", q: "ops team", want: ["group:g1"] },
    { name: "empty is kind then label", q: "", want: ["user:u1", "user:u2", "group:g1"] },
    { name: "limit", q: "", limit: 2, want: ["user:u1", "user:u2"] },
  ];
  for (const c of cases) {
    assert.deepEqual(filter(all, c.q, c.limit).map((x) => x.value), c.want, c.name);
  }
});

test("kind order puts idp_group and role last", () => {
  const list = [e("role:r", "A"), e("idp_group:i", "A"), e("group:g", "A"), e("user:u", "Z")];
  assert.deepEqual(filter(list, "").map((x) => x.kind), ["user", "group", "idp_group", "role"]);
});

test("stepPrincipal", () => {
  const step = globalThis.teamsterStepPrincipal;
  const cases = [
    { name: "down from none", active: -1, step: 1, want: 0 },
    { name: "up from none is the last", active: -1, step: -1, want: 2 },
    { name: "down wraps", active: 2, step: 1, want: 0 },
    { name: "up wraps", active: 0, step: -1, want: 2 },
    { name: "down", active: 0, step: 1, want: 1 },
  ];
  for (const c of cases) {
    assert.equal(step(c.active, c.step, 3), c.want, c.name);
  }
});
