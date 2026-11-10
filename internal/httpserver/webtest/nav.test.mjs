// Tests the legacy /admin#section redirect in web/nav.js; see ADR 0098 and 0103.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/nav.js", import.meta.url));
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
