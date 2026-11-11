# Destination pages — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give destinations a list page, a read-only record page with edit in place, and a new
page, and build the shared frame the other resources will reuse.

**Architecture:** Server-rendered templ. A new `views/record.templ` holds the frame (`listPage`,
`recordPage`, `newPage`); destinations get their own view structs and handlers instead of the
shared `views.Page`. A small `web/record.js` swaps view and edit parts and fills in Team and channel
names. A failed save renders the page again with status 422 instead of redirecting.

**Tech Stack:** Go 1.26 `net/http` (method + wildcard patterns), templ, Tailwind v4, vanilla JS,
`node:test`.

**Spec:** `docs/superpowers/specs/2026-11-10-resource-pages-design.md`

## Global Constraints

* Edit `.templ`, never `*_templ.go`; run `make generate` after templ/CSS changes and commit the
  output.
* Go tests are table-driven and call `t.Parallel()`.
* New UI strings in `internal/i18n/locales/en.json` and `de.json`, keys sorted as the files are.
* Comments: one line, why not what.
* Old links keep working: `/admin/destinations?edit=<id>` → 301 `/admin/destinations/<id>?edit`.
* A record the viewer may not read, or that does not exist, answers 404.
* Done = `make coverage` ≥ 75 %, `make test-js`, `make lint` clean.
* Commit with `agent-commit` (stage first); Conventional Commits; trailer
  `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never `git stash`.

## Review Focus

1. Someone who only holds a share of one destination opens `/admin/destinations/<that id>` — sees
   it; any other id answers 404, never 403 (which would confirm it exists).
2. A failed save of a **new** destination (no id) renders the new page with the typed values,
   not a record page and not a redirect.
3. `GET /admin/destinations/new` is the new page, not a record with id `new`;
   `GET /admin/destinations/delete` answers 404, not 405 or a panic.
4. A failed delete (e.g. the store refuses) lands back on the record page with the error, not on
   the list.
5. Graph unreachable: the record page still renders immediately with IDs, and the script logs no
   uncaught error.

Each has a test in the task that owns the code.

---

### Task 1: Frame and destination list

**Files:**

* Create: `internal/httpserver/views/record.templ`, `internal/httpserver/views/destinations.templ`
* Create: `internal/httpserver/destinations_page.go`
* Modify: `internal/httpserver/server.go:278` (registration),
  `internal/httpserver/views/admin.templ` (drop the `destinations` case and panel),
  `internal/httpserver/views/views.go` (view structs)
* Test: `internal/httpserver/destinations_page_test.go`

**Interfaces:**

* Produces in `views`:

  ```go
  type DestinationRow struct {
      Destination  models.Destination
      InstallState string // "installed" | "missing" | "unknown", as InstallStates holds today
  }
  type DestinationList struct {
      Viewer         Viewer
      Notice, Error  string
      Rows           []DestinationRow
      CanCreate      bool
  }
  templ listPage(title, newLabel, newHref string, canCreate bool) // children: the rows
  templ DestinationListPage(p DestinationList)
  ```

* Produces in `httpserver`: `func (s *Server) handleDestinationList(w http.ResponseWriter,
  r *http.Request)`, registered as `GET /admin/destinations`; `POST /admin/destinations` stays
  `formPost("destinations", saveDestination)` until Task 3.
* i18n: `destinations.new` "New destination" / German from the existing destinations nouns;
  `page.destinations` already exists.

* [ ] **Step 1: Failing tests** `TestDestinationList` (table): admin sees every destination as a
  link to `/admin/destinations/<id>`, the *global default* badge on the default, *app missing*
  where the install state is missing, a link to `/admin/destinations/new`; viewer sees the rows
  and no New link; share-only user sees only the shared destination; empty store shows the empty
  state. No `<form` inside the rows.
* [ ] **Step 2: Run** `go test ./internal/httpserver -run TestDestinationList -race` — FAIL.
* [ ] **Step 3: Implement.** Rows come from `ListDestinations` → `listDestinations(r, …)` →
  `installStates`, as `adminPage` does today (`admin_ui.go:150-158`); `CanCreate` is
  `page.mayCreate("Destination")`'s source (`fillAccess`). The list renders inside `Layout` with
  the page title `page.destinations`.
* [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race` — PASS (rewrite tests
  that read the destination form from `GET /admin/destinations`; they move to Tasks 2–3 pages).
* [ ] **Step 5: Commit** `feat(ui): list destinations on a page of their own`.

### Task 2: Record page, routing and old links

**Files:**

* Modify: `internal/httpserver/views/record.templ`, `internal/httpserver/views/destinations.templ`,
  `internal/httpserver/destinations_page.go`, `internal/httpserver/server.go`,
  `internal/httpserver/authorize.go:238` (`recordPrefixes`),
  `internal/httpserver/admin_ui.go:63` (`recordEditPath`) and `handleAdminPage`
* Test: `internal/httpserver/destinations_page_test.go`

**Interfaces:**

* Consumes: Task 1's list and view structs.
* Produces in `views`:

  ```go
  type RecordHead struct {
      ListHref, ListLabel, Title string
      Editing                    bool // server-decided: ?edit present or a failed save
  }
  type UsedBy struct{ Href, Label string }
  type DestinationRecord struct {
      Viewer                              Viewer
      Notice, Error                       string
      Destination                         models.Destination // as stored
      Form                                models.Destination // values the form shows
      InstallState                        string
      CanUpdate, CanDelete, CanMakeDefault bool
      Routes, Webhooks                    []UsedBy
      Sharing                             *Sharing
      BrokerAvailable                     bool
      Head                                RecordHead
  }
  templ recordPage(h RecordHead, actions, view, edit templ.Component) // edit may be nil
  templ destinationForm(form models.Destination, brokerAvailable bool, cancelHref string)
  templ DestinationRecordPage(p DestinationRecord)
  ```

  `recordPage` renders `<div data-record>` holding `<div data-mode="view">` and, when `edit` is
  not nil, `<form>`-carrying `<div data-mode="edit">`; the one not shown carries `hidden`.
  Edit is `<a href="?edit" data-record-edit>`; Cancel in the form is
  `<a href="<record href>" data-record-cancel>`.
* Produces in `httpserver`: `func (s *Server) handleDestinationRecord(w http.ResponseWriter,
  r *http.Request)` for `GET /admin/destinations/{id}`;
  `func recordEditPath(section, id string) string` returns `"/admin/destinations/" +
  url.PathEscape(id)` for `section == "destinations"`, the phase-1 form otherwise.
* Routing: `GET /admin/destinations/{id}`, `POST /admin/destinations/delete`,
  `POST /admin/destinations/default`. `GET /admin/destinations?edit=<id>` → 301
  `recordEditPath("destinations", id) + "?edit"`, carrying `notice`/`error`. `handleAdminPage`'s
  `?edit=destinations&id=` target uses `recordEditPath` so it is one hop.
* `recordPrefixes` gains `"/admin/destinations/"`.

* [ ] **Step 1: Failing tests** in `destinations_page_test.go`:
  * `TestDestinationRecord` (table): admin → 200, summary shows name, Team and channel IDs,
    install state, *global default* badge when default; Edit, Make default (not on the default),
    Delete present; `data-mode="edit"` present and `hidden`. With `?edit` the view part is
    `hidden` and the form is not. Viewer → no Edit/Delete/Make default, no `data-mode="edit"`.
    Editor → Edit and Delete, no Make default.
  * `TestDestinationRecordNotFound` (table): missing id, id of a destination the share-only user
    does not hold, `/admin/destinations/delete` (GET) → 404; share-only user on the shared id →
    200 without Make default.
  * `TestDestinationRecordUsedBy`: two routes with this `DestinationID` (one readable, one not for
    a share-only user), a child route that only inherits it, one webhook endpoint → admin sees
    both direct routes and the webhook as links, not the inheriting child; share-only user sees
    only what they may read; a link to `/admin/routing`.
  * `TestDestinationOldEditLink`: `/admin/destinations?edit=abc&notice=x` → 301
    `/admin/destinations/abc?edit&notice=x`; `/admin?edit=destinations&id=abc` → 301 directly to
    `/admin/destinations/abc?edit`.
  * `TestDestinationNewIsNotARecord`: `GET /admin/destinations/new` does not render a record (it
    may 404 until Task 3 adds the page; assert it is not 200 with a record).
* [ ] **Step 2: Run** them — FAIL.
* [ ] **Step 3: Implement.** Read check: `s.can(r, authz.ActionRead, typeDestination, id)`; on
  false or `store` not-found → `http.NotFound`. Rights: `can` with update / delete;
  `CanMakeDefault` = viewer may administer Destination (as `isDefaultDestinationPath` maps it) and
  not already default. Used by: `ListRoutes` filtered to `DestinationID == id` then `readable`;
  label is the route's name, falling back to the matcher text the routes panel shows; endpoints
  from `listEndpoints` with `DestinationID == id`, label `team_slug/channel_slug`. Sharing via
  `sharingFor(r, typeDestination, id, "/admin/destinations/"+id)`.
* [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race` — PASS.
* [ ] **Step 5: Commit** `feat(ui): give each destination a record page`.

### Task 3: New page and saving

**Files:**

* Modify: `internal/httpserver/admin_ui.go:378-412` (`saveDestination` → `storeDestination`),
  `internal/httpserver/destinations_page.go`, `internal/httpserver/server.go`,
  `internal/httpserver/views/destinations.templ`, `internal/httpserver/views/record.templ`
* Test: `internal/httpserver/destinations_page_test.go`

**Interfaces:**

* Consumes: Task 2's `DestinationRecord`, `destinationForm`, `recordPage`.
* Produces:

  ```go
  // views
  type DestinationNew struct {
      Viewer          Viewer
      Error           string
      Form            models.Destination
      BrokerAvailable bool
  }
  templ newPage(listHref, listLabel, title string) // children: the form
  templ DestinationNewPage(p DestinationNew)

  // httpserver
  func (s *Server) storeDestination(r *http.Request) (models.Destination, string, error)
  func (s *Server) handleDestinationNew(w http.ResponseWriter, r *http.Request)  // GET /admin/destinations/new
  func (s *Server) handleDestinationSave(w http.ResponseWriter, r *http.Request) // POST /admin/destinations
  ```

  `storeDestination` is today's `saveDestination` body returning the saved record and the notice;
  the API path keeps its own handler.
* Delete: success → 303 `/admin/destinations` with the notice; failure → 303
  `/admin/destinations/<id>` with the error. Make default: 303 to the record either way.

* [ ] **Step 1: Failing tests:**
  * `TestDestinationNewPage`: editor → 200 with the form and Cancel linking to the list; viewer →
    403 (no create right).
  * `TestDestinationSave` (table): create valid → 303 `/admin/destinations/<new id>`, notice
    carried; update valid → 303 to that record; create refused by `mayDeliverTo` → **422**, body is
    the new page with the typed name and the error; update with a refused channel → 422, record
    page with `data-mode="edit"` visible, typed values in the inputs, error shown; cross-origin
    post → 403 (formGuard).
  * `TestDestinationDelete` (table): success → 303 list; store refusal → 303 record with `error`.
  * `TestDestinationMakeDefault`: admin → 303 record with notice.
* [ ] **Step 2: Run** — FAIL.
* [ ] **Step 3: Implement.** `handleDestinationSave` runs `formGuard`, then `storeDestination`; on
  error builds the new or record page from the posted values (`id` empty → new) with
  `visibleError(ctx, "save destination", err)` and writes 422. Delete and default use
  `formPostTo` with per-request targets (wrap so the target can depend on `id`).
* [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race` — PASS.
* [ ] **Step 5: Commit** `feat(ui): create and edit destinations on their own pages`.

### Task 4: Edit in place and names — `record.js`

**Files:**

* Create: `internal/httpserver/web/record.js`, `internal/httpserver/webtest/record.test.mjs`
* Modify: `internal/httpserver/views/layout.templ` (script tag), `internal/httpserver/server.go`
  (public asset list), `internal/httpserver/views/destinations.templ` (`data-team-id`,
  `data-channel-id` on the summary values)
* Test: `internal/httpserver/webtest/record.test.mjs`, `internal/httpserver/assets_test.go` if it
  lists assets

**Interfaces:**

* Pure helpers exposed like `nav.js` does (IIFE over `global`, early return without `document`):

  ```js
  global.teamsterRecordURL(href, editing) // -> href with ?edit added or removed, other params kept
  global.teamsterNameOf(items, id)        // -> items.find(i => i.id === id)?.displayName ?? null
  ```

* DOM behaviour: click on `[data-record-edit]` → `preventDefault`, hide view, show edit, focus
  first field, `history.replaceState(null, "", teamsterRecordURL(location.href, true))`; click on
  `[data-record-cancel]` → `preventDefault`, `form.reset()`, swap back, URL without `?edit`;
  `beforeunload` prompts while the form differs from its defaults and is not being submitted.
  Names: for `[data-team-id]` fetch `/api/graph/teams`, for `[data-channel-id]`
  `/api/graph/teams/<team>/channels`; on success replace the text with the name and keep the ID
  in a `<span class="text-xs text-slate-500">`; on any failure leave the ID.

* [ ] **Step 1: Failing tests** `record.test.mjs`: `teamsterRecordURL` adds `?edit` to a bare
  URL, keeps `notice`, removes `edit` and leaves other params, is idempotent; `teamsterNameOf`
  finds by id, returns null for missing id and for an empty list.
* [ ] **Step 2: Run** `make test-js` — FAIL.
* [ ] **Step 3: Implement** `record.js`; load it in `layout.templ` after `nav.js`; add `/record.js`
  to the public asset list in `server.go`.
* [ ] **Step 4: Run** `make generate && make test-js && go test ./internal/httpserver -race` —
  PASS. Then in a browser against a local server: Edit/Cancel swap without reload and URL follows;
  reload in edit mode stays in edit mode; typing then navigating away prompts; names appear with
  Graph reachable and IDs stay with Graph unreachable (no console error).
* [ ] **Step 5: Commit** `feat(ui): edit a record in place`.

### Task 5: Documentation

**Files:**

* Create: `docs/adr/0105-give-each-record-a-page-of-its-own.md` (from `0000-template.md`)
* Modify: `docs/adr/README.md`, `docs/architecture.md`, `README.md`, `CHANGELOG.md`,
  `docs/roadmap.md`

* [ ] **Step 1:** ADR 0105: context (stacked form + list + sharing per page), decision (frame,
  edit in place, 422 failed save, 404 for unreadable records, one PR per resource), alternatives
  (single `views.Page` with modes; generic resource interface; separate `/edit` URL), consequences.
* [ ] **Step 2:** architecture.md and README describe destinations' pages and the old-link
  redirect; CHANGELOG `Unreleased` → `Changed`; roadmap gains a milestone "Record pages for every
  resource" listing the remaining resources in the spec's order.
* [ ] **Step 3:** `pre-commit run markdownlint-cli2 --files <changed md>` clean;
  `make lint coverage test-js` pass.
* [ ] **Step 4: Commit** `docs: record the destination pages`.

### Task 6: User documentation and pull requests (controller)

* [ ] Worktree from `origin/pages`; docs-writer then docs-translator agents update the destination
  steps (EN + DE); `hugo --panicOnWarning`, `scripts/check-translations.sh`, markdownlint.
* [ ] Open the docs PR against `pages` and the code PR against `main`, linked both ways.
