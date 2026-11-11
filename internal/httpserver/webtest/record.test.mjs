// Tests the pure logic in web/record.js: the record and ?edit URLs and the Graph name lookup.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/record.js", import.meta.url));
const { teamsterRecordURL: recordURL, teamsterNameOf: nameOf } = globalThis;

test("teamsterRecordURL", () => {
  const base = "/admin/destinations/7";
  const cases = [
    { name: "adds edit", editing: true, want: base + "?edit" },
    { name: "bare when not editing", editing: false, want: base },
  ];
  for (const c of cases) assert.equal(recordURL(base, c.editing), c.want, c.name);
});

test("teamsterNameOf", () => {
  const items = [{ id: "a", name: "Alpha" }, { id: "b", name: "Beta" }];
  assert.equal(nameOf(items, "b"), "Beta");
  assert.equal(nameOf(items, "z"), null);
  assert.equal(nameOf([], "a"), null);
});
