// Tests the pure logic of web/preview.js: which samples a template is offered.
import { test } from "node:test";
import assert from "node:assert/strict";

await import(new URL("../web/preview.js", import.meta.url));
const samplesFor = globalThis.teamsterSamplesFor;

const samples = [
  { key: "alertmanager", source: "alertmanager" },
  { key: "open", source: "universal" },
  { key: "password-reminder", source: "universal" },
  { key: "teamsv2", source: "teamsv2" },
];

test("samplesFor", () => {
  const cases = [
    { name: "no webhook ticked offers every sample", sources: [], want: ["alertmanager", "open", "password-reminder", "teamsv2"] },
    { name: "one webhook", sources: ["universal"], want: ["open", "password-reminder"] },
    { name: "two webhooks keep the list order", sources: ["teamsv2", "alertmanager"], want: ["alertmanager", "teamsv2"] },
    { name: "a webhook without samples", sources: ["other"], want: [] },
  ];
  for (const c of cases) {
    assert.deepEqual(samplesFor(samples, c.sources), c.want, c.name);
  }
});
