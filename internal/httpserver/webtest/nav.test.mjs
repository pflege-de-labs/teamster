// Tests the pure logic in web/nav.js: which clicks leave the page, and the legacy
// /admin#section redirect (ADR 0103).
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/nav.js", import.meta.url));
const leaves = globalThis.teamsterNavLeaves;

const here = { origin: "https://t.example", pathname: "/admin/teams", search: "" };
const click = { button: 0 };
const link = (over = {}) => ({
  tagName: "A",
  target: "",
  origin: here.origin,
  pathname: "/admin/people",
  search: "",
  hash: "",
  attrs: [],
  hasAttribute(name) {
    return this.attrs.includes(name);
  },
  ...over,
});

test("leaves", () => {
  const cases = [
    { name: "plain link", event: click, el: link(), want: true },
    { name: "middle click", event: { button: 1 }, el: link(), want: false },
    { name: "ctrl click", event: { button: 0, ctrlKey: true }, el: link(), want: false },
    { name: "new tab", event: click, el: link({ target: "_blank" }), want: false },
    { name: "download", event: click, el: link({ attrs: ["download"] }), want: false },
    { name: "other origin", event: click, el: link({ origin: "https://x.example" }), want: false },
    { name: "anchor on this page", event: click, el: link({ pathname: "/admin/teams", hash: "#x" }), want: false },
    { name: "anchor on another page", event: click, el: link({ hash: "#x" }), want: true },
    { name: "form post", event: {}, el: { tagName: "FORM", method: "post", target: "" }, want: true },
    { name: "dialog form", event: {}, el: { tagName: "FORM", method: "dialog", target: "" }, want: false },
    { name: "form to new tab", event: {}, el: { tagName: "FORM", method: "get", target: "_blank" }, want: false },
  ];
  for (const c of cases) assert.equal(leaves(c.event, c.el, here), c.want, c.name);
});

const target = globalThis.teamsterNavLegacyTarget;

const cases = [
  ["/admin/routing", "#templates", "/admin/templates"],
  ["/admin/routing", "#destinations", "/admin/destinations"],
  ["/admin/routing", "#webhooks", "/admin/webhooks"],
  ["/admin/routing", "#routes", "/admin/routes"],
  ["/admin/routing", "#grants", "/admin/grants"],
  ["/admin/routing", "#flow", null],
  ["/admin/routing", "", null],
  ["/admin/templates", "#templates", null],
  ["/admin/routing", "#templatesX", null],
];

for (const [path, hash, want] of cases) {
  test(`${path}${hash || " (no hash)"}`, () => assert.equal(target(path, hash), want));
}
