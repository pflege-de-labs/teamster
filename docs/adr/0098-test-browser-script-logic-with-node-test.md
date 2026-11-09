# 0098. Test browser script logic with node:test

* Status: Accepted
* Date: 2026-11-09

## Context

The admin UI's scripts under `internal/httpserver/web/` had no tests of their own. Go tests check
the markup and the JSON the scripts read, and a person checks the result in a browser. That was
enough while the scripts mostly moved data into the page. [ADR 0097](0097-lay-out-the-routing-graph-as-a-tangled-tree.md)
changed that: `web/tangle.js` now computes the routing graph's geometry, which Go never sees, and
it was checked once against the original by a script that was not committed.

The repository has no JavaScript toolchain, and ADR 0031 keeps it that way for browser libraries:
they are vendored files, not npm dependencies. Everything under `web/` is embedded and served as
it is (`//go:embed web/*`).

## Decision

We will test the pure logic of the browser scripts with Node's built-in test runner, `node:test`
and `node:assert`.

* **No npm.** There is no `package.json`, lockfile or `node_modules`. The scripts are plain files
  that set globals, so a test loads one with `import()` and reads the global.
* **Outside `web/`.** Tests live in `internal/httpserver/webtest/`, so they are never embedded or
  served.
* **One command.** `make test-js` runs them. CI runs them in a `js` job with Node pinned by
  `.node-version`, which Renovate updates.
* **Scope.** Only logic that needs no DOM: the layout, graph walks, parsing. A DOM would need jsdom
  or a browser, which means npm and a much larger toolchain. The drawing and the interaction stay
  checked in a browser.
* **First test.** `tangle.test.mjs` pins `tangle.js` to the coordinates the notebook's original
  function produces for a routing-shaped fixture. It also checks the properties compact mode
  promises.

We considered Vitest and Jest. Both need npm and a lockfile, plus Renovate rules for a dependency
tree that exists only to run a few hundred lines of tests. Testing through Go with an embedded
JavaScript engine would keep one toolchain, but it adds a Go dependency, and its JavaScript is not
the one browsers run.

## Consequences

* A change that moves the layout away from the original, or breaks compact mode, fails CI.
* Contributors need Node only to run `make test-js`. `make test` and `make coverage` stay Go-only.
* The helpers inside `routing.js` (levels from server columns, the upstream/downstream walk) sit in
  a closure, so these tests cannot reach them yet. Testing them means moving them into a file of
  their own that sets a global, as `tangle.js` does.
* JavaScript coverage is not measured and does not count towards the 75% Go coverage gate.
