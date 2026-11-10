// Tests the pure logic in web/record.js: the ?edit URL and the Graph name lookup.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/record.js", import.meta.url));
const { teamsterRecordURL: recordURL, teamsterNameOf: nameOf } = globalThis;

test("teamsterRecordURL", () => {
  const base = "https://t.example/admin/destinations/7";
  const cases = [
    { name: "adds edit", href: base, editing: true, want: base + "?edit" },
    { name: "keeps notice", href: base + "?notice=saved", editing: true, want: base + "?notice=saved&edit" },
    { name: "removes edit", href: base + "?edit", editing: false, want: base },
    { name: "removes edit, keeps others", href: base + "?edit&notice=saved", editing: false, want: base + "?notice=saved" },
    { name: "add is idempotent", href: base + "?edit", editing: true, want: base + "?edit" },
    { name: "remove is idempotent", href: base, editing: false, want: base },
  ];
  for (const c of cases) assert.equal(recordURL(c.href, c.editing), c.want, c.name);
});

test("teamsterNameOf", () => {
  const items = [{ id: "a", name: "Alpha" }, { id: "b", name: "Beta" }];
  assert.equal(nameOf(items, "b"), "Beta");
  assert.equal(nameOf(items, "z"), null);
  assert.equal(nameOf([], "a"), null);
});

test("teamsterFormDirty", () => {
  const { teamsterFormState: state, teamsterFormDirty: dirty } = globalThis;
  const fd = (o) => Object.entries(o);
  const base = state(fd({ name: "a", team_id: "t1" }));
  assert.equal(dirty(base, state(fd({ name: "a", team_id: "t1" }))), false, "unchanged");
  assert.equal(dirty(base, state(fd({ name: "b", team_id: "t1" }))), true, "text field");
  assert.equal(dirty(base, state(fd({ name: "a", team_id: "t2" }))), true, "hidden value");
  // A real FormData leaves out buttons and disabled fields, which is what keeps them from counting.
  const form = new FormData();
  form.append("name", "a");
  form.append("team_id", "t1");
  assert.equal(dirty(base, state(form)), false, "FormData");
});
