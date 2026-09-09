# 0008. The admin UI is server-rendered with templ and Tailwind

* Status: Accepted
* Date: 2026-09-09

## Context

The admin UI was a static `index.html` plus 187 lines of JavaScript that fetched the three admin
endpoints and rebuilt the lists in the browser. Two consequences followed.

Everything the operator saw was an identifier. A route stores a destination id and a template id,
and the browser had no cheap way to resolve them, so the form asked for UUIDs to be typed in and
the list printed them back. [The roadmap](../roadmap.md) had "selectable destinations and
templates" and "fuller lists" as its first milestone for exactly that reason.

Rendering also existed twice: markup in `index.html`, and markup assembled from strings in
`app.js`. Any change to how a list looked had to be made in both, and the JavaScript half was
outside the reach of the coverage gate.

## Decision

The UI is rendered on the server with [templ](https://github.com/a-h/templ), and styled with
Tailwind CSS.

* `internal/httpserver/views` holds `.templ` components. templ compiles them to Go, so the
  compiler checks the markup and the handler tests exercise it.
* The page is rendered by `handleAdminPage`; forms post to `/admin/{templates,destinations,routes}`
  and their `/delete` variants, and answer `303 See Other` back to `/admin` with the outcome as a
  query parameter. `app.js` and `index.html` are deleted.
* Because the handler already has the lists, destination and template become `select` elements and
  routes list resolved names, delivering roadmap items 1.1 and most of 1.2 as a side effect.
* The JSON API under `/api` is untouched. It remains the scriptable interface, and the UI does not
  depend on it.
* Generated output is committed: `*_templ.go` and the compiled `web/styles.css`. A plain
  `go build`, the CI jobs and the Docker build therefore need neither templ nor Tailwind.
  `make generate` refreshes them, and `go:generate` directives in `views.go` mean `go generate ./...`
  does the same.
* templ is a `tool` dependency in `go.mod`, so its version is pinned and Renovate updates it like
  any other module. Tailwind ships as a standalone binary, fetched by `make tools` at a pinned
  version — no Node, no `package.json`.
* `.air.toml` builds with `make generate && go build`, watches `.templ` and `.css`, and ignores
  `_templ.go` so regeneration does not retrigger itself.

### Cross-site form posts

Form posts change state, and basic auth credentials are attached by the browser to a cross-site
post as readily as to a same-origin one. There is no session to carry a CSRF token, so
`formPost` requires the request to prove its origin: `Sec-Fetch-Site` must be `same-origin` or
`none`, or `Origin` must match the host. A request with neither header is not a browser form post
and is left to the auth layer, which keeps `curl` against the API working.

## Consequences

* Markup lives in one place and is type-checked. Coverage counts it, which is why `make coverage`
  now uses `-coverpkg` — code exercised through another package's tests was previously reported as
  0%. Generated files are filtered out of the profile, because generated code is not ours to test.
* Changing the UI requires the two generators. Not changing it requires nothing, which is the
  point of committing their output.
* Committed generated files appear in review diffs. `*_templ.go` carries the standard generated
  header, so golangci-lint skips it.
* The roadmap's "no build step, vendor everything" constraint is amended: a generator whose output
  is committed is allowed. A runtime dependency on a CDN still is not.
* An operator with JavaScript disabled can now use the UI, which was not previously true.
