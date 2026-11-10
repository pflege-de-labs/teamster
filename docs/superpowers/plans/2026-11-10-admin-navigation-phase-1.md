# Admin navigation, phase 1 — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group the admin sidebar into Configuration / Delivery / Access / Administration and turn the five `/admin` tabs into five pages with their own URLs, redirecting every old URL.

**Architecture:** Server-rendered templ, as today. Each resource path gets a `GET` page handler next to its existing `POST` form handler (Go 1.26 method patterns). `views.Admin` renders one panel, chosen by `Page.Tab`. The sidebar is built from a list of groups computed from `Viewer`, which now carries the request path for prefix-based `aria-current`.

**Tech Stack:** Go 1.26 `net/http`, templ, Tailwind v4, vanilla JS, `node:test`.

**Spec:** `docs/superpowers/specs/2026-11-10-admin-navigation-design.md` (phase 1 section).

## Global Constraints

* Edit `.templ`, never `*_templ.go`; run `make generate` after every templ or CSS change and commit the output.
* Tests are table-driven and call `t.Parallel()`.
* Every new UI string exists in `internal/i18n/locales/en.json` and `de.json`; keys sorted as the files are.
* Comments: one line, why not what.
* Old URLs redirect permanently (301); `/admin` itself redirects 302 (its target depends on the viewer).
* `notice` and `error` query values survive every redirect.
* `tabs.js` stays (the routing page uses it).
* Done = `make coverage` ≥ 75 %, `make test-js`, `make lint` clean.
* Commit with `agent-commit` (stage with `git add` first); Conventional Commits; trailer `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. A user with no role who only holds shared records opens `/admin` (bookmark, login landing) — must land on `/admin/templates`, which lets them through, not on the no-access page.
2. A Teams hint card link `/admin?edit=webhooks&id=<id>` (already posted in chats) — must reach `/admin/webhooks?edit=<id>` with the form filled.
3. `/admin?tab=bogus` or `/admin?edit=bogus&id=x` — must redirect to `/admin`'s normal target, never loop or 404.
4. `/admin/routes` must not mark **Routing** (`/admin/routing`) current, and `/admin/templates/x` must mark **Templates** — prefix match is `path == href || HasPrefix(path, href+"/")`.
5. `localStorage` throwing (private mode) — every group stays open, nothing breaks.

Each has a test in the task that owns the code.

---

### Task 1: One page per resource

**Files:**
- Modify: `internal/httpserver/server.go:272-284` (registrations)
- Modify: `internal/httpserver/admin_ui.go` (`handleAdminPage`, new `handleResourcePage`, `adminTabPath`, `loadForEditing` call)
- Modify: `internal/httpserver/webhooks_admin.go:148-153` (`revealWebhookURL` unchanged except it already sets `Tab`)
- Modify: `internal/httpserver/views/admin.templ:9-58` (drop `tabBar`, `tabPanel`; render one panel)
- Modify: `internal/httpserver/views/views.go:161-180` (drop `tabs`, `activeTab`)
- Modify: `internal/i18n/locales/{en,de}.json` (page titles)
- Test: `internal/httpserver/admin_ui_test.go`

**Interfaces:**
- Produces: `func (s *Server) handleResourcePage(tab string) http.HandlerFunc` — renders `views.Admin` with `page.Tab = tab`; `?edit=<id>` loads that record via `s.loadForEditing(r, &page, tab, id)`.
- Produces: `adminTabPath(tab string) string` returns `"/admin/" + tab` (all `formPost` redirects follow).
- Produces: i18n keys `page.routes`, `page.templates`, `page.destinations`, `page.webhooks`, `page.grants` (`"Routes — Teamster"` … `"Delivery permissions — Teamster"`; German: `"Routen — Teamster"`, `"Vorlagen — Teamster"`, `"Ziele — Teamster"`, `"Webhooks — Teamster"`, `"Zustellrechte — Teamster"` — check `de.json`'s existing panel labels and use the same nouns).

- [ ] **Step 1: Write the failing tests** in `admin_ui_test.go`:
  * `TestResourcePages`: table over `templates, destinations, webhooks, routes, grants` as admin → `GET /admin/<t>` is 200, body contains `id="<t>"` (the panel's section id) and none of the other four section ids, and no `role="tablist"`.
  * `TestResourcePageEdit`: `GET /admin/templates?edit=<seeded template id>` → body contains `name="id" value="<id>"`.
  * `TestResourcePageMethods`: `PUT /admin/templates` → 405 with `Allow` containing `GET` and `POST`.
  * `TestFormPostLandsOnResourcePage`: `POST /admin/templates` (valid form, same-origin) → 303, `Location` path `/admin/templates`.
  * `TestResourcePageAccess` (table): `GET` each of the five pages as viewer, editor, admin, a no-role user holding one template share, and a no-role user with nothing → viewer/editor 200 except `/admin/grants` 403; admin 200 everywhere; shared-record user 200 on `/admin/templates` showing only the shared template, 403 on `/admin/grants`; nothing → the no-access page. A viewer's `POST` to each still answers 403.
  * Rewrite the existing cases that assert `GET` on these paths is 405, or `Location: /admin?tab=…`, to the new behaviour.
- [ ] **Step 2: Run** `go test ./internal/httpserver -run 'ResourcePage|FormPostLands' -race` — expect FAIL (405 / old Location).
- [ ] **Step 3: Implement.** Register `GET /admin/<t>` → `api.handleResourcePage("<t>")` and `POST /admin/<t>` → the existing handler for each of the five (`/admin/webhooks` POST stays `handleWebhookForm`). `views.Admin` takes its title from `"page."+page.Tab` and renders the one matching panel component with a `switch`. `handleAdminPage` keeps rendering for now, defaulting `page.Tab` to `"templates"` when neither `tab` nor `edit` names one, so old-URL tests keep passing until Task 2.
- [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race` — expect PASS.
- [ ] **Step 5: Commit** `feat(ui): give each configuration resource its own page`.

### Task 2: Redirect old URLs and move every link builder

**Files:**
- Modify: `internal/httpserver/admin_ui.go` (`handleAdminPage`, sharing return path at ~194)
- Modify: `internal/httpserver/sharing_hub.go:38` (`recordHref`)
- Modify: `internal/httpserver/teamsv2.go:143`, `internal/httpserver/webhooks.go:740,813`
- Modify: `internal/httpserver/views/components.templ:97-109` (`editLink`, `cancelEdit`), `internal/httpserver/views/admin.templ:316,399`
- Test: `internal/httpserver/admin_ui_test.go`, plus the old-URL assertions in `webhooks_test.go`, `route_targets_test.go`, `linked_route_test.go`, `permissions_records_test.go`, `sharing_hub_test.go`

**Interfaces:**
- Consumes: Task 1's pages and `adminTabPath`.
- Produces: `func recordEditPath(section, id string) string` → `"/admin/" + section + "?edit=" + url.QueryEscape(id)`; used by `recordHref`, the sharing return path, the Teams hint card and the delivery hint. `editLink(section, id, name)` and `cancelEdit(tab)` emit the same shape (`/admin/<section>?edit=<id>`, `/admin/<tab>`), no `#fragment`.

- [ ] **Step 1: Write the failing test** `TestAdminRedirects` (table; role admin unless noted):

  | request | status | Location |
  |---|---|---|
  | `/admin` | 302 | `/admin/routing` |
  | `/admin` as a no-role user holding one template share | 302 | `/admin/templates` |
  | `/admin?tab=webhooks` | 301 | `/admin/webhooks` |
  | `/admin?tab=grants&notice=Saved.` | 301 | `/admin/grants?notice=Saved.` |
  | `/admin?edit=webhooks&id=abc` | 301 | `/admin/webhooks?edit=abc` |
  | `/admin?edit=routes&id=a%2Fb&error=x` | 301 | `/admin/routes?edit=a%2Fb&error=x` |
  | `/admin?tab=bogus` | 302 | `/admin/routing` |
  | `/admin?edit=bogus&id=x` | 302 | `/admin/routing` |

  Also `TestHintCardLinksToWebhookPage`: the Teams hint card built in `teamsv2.go` contains `/admin/webhooks?edit=<id>`.
- [ ] **Step 2: Run** `go test ./internal/httpserver -run 'AdminRedirects|HintCard' -race` — expect FAIL.
- [ ] **Step 3: Implement** `handleAdminPage`: known sections are the five tab names; `tab` or `edit` naming one → 301 to `/admin/<t>` (+ `?edit=<id>` when `edit`), carrying `notice`/`error`; otherwise 302 to `/admin/routing` if `s.allow(r, authz.ActionView, authz.Resource{Type: "Page"})`, else `/admin/templates`. Switch every link builder listed above to `recordEditPath` / the new shapes.
- [ ] **Step 4: Update** the old-URL assertions in the five other test files to the new URLs, then run `make generate && go test ./internal/httpserver -race` — expect PASS. `rtk proxy grep -rn "admin?tab=\|admin?edit=" internal --include='*.go' --include='*.templ' --include='*.js' | grep -v _templ.go` prints only `handleAdminPage` and `TestAdminRedirects`.
- [ ] **Step 5: Commit** `feat(ui): redirect the old configuration URLs`.

### Task 3: Grouped sidebar and user-menu notifications

**Files:**
- Modify: `internal/httpserver/views/views.go` (`Viewer.Path`, nav model)
- Modify: `internal/httpserver/authorize.go:59` (`viewerFor` sets `Path`)
- Modify: `internal/httpserver/views/usermenu.templ:55-99` (`sidebar`, `navLink`, user menu entry)
- Modify: `internal/httpserver/web/nav.js` (drop the exact-path `aria-current` loop)
- Modify: `internal/i18n/locales/{en,de}.json`
- Test: `internal/httpserver/header_test.go`

**Interfaces:**
- Produces in `views`:
  ```go
  // Viewer gains:
  Path string // request path, for marking the current nav item

  type navItem struct{ Href, Key string }
  type navGroup struct{ Key string; Items []navItem }
  func (v Viewer) navGroups() []navGroup        // groups and items per the spec table, empty groups dropped
  func (v Viewer) current(href string) bool     // v.Path == href || strings.HasPrefix(v.Path, href+"/")
  ```
- Group keys (also the `data-group` attribute and storage suffix): `configuration`, `delivery`, `access`, `administration`.
- i18n (en / de): `nav.group.configuration` Configuration / Konfiguration; `nav.group.delivery` Delivery / Zustellung; `nav.group.access` Access / Zugriff; `nav.group.administration` Administration / Verwaltung; `nav.routes` Routes / Routen; `nav.templates` Templates / Vorlagen; `nav.destinations` Destinations / Ziele; `nav.webhooks` Webhooks / Webhooks; `nav.grants` Delivery permissions / Zustellrechte; `nav.my_notifications` My notifications / Meine Benachrichtigungen; `nav.permissions` becomes Channel scopes / Kanalbereiche. Remove `nav.configuration` and `nav.notifications` if nothing else uses them. Where `de.json` already names one of these things (panel headings), reuse its noun instead of the one here.

- [ ] **Step 1: Write the failing tests** in `header_test.go`:
  * `TestSidebarGroups` (table by role): admin with bot + global install sees all four groups, in order, with the spec's items in order; viewer sees no Administration group and no **Delivery permissions**/**Channel scopes**/**Users**; no group renders without items (assert by `data-group=` count).
  * `TestSidebarCurrent` (table): path `/admin/routes` → only the Routes link has `aria-current="page"` (Routing does not); `/admin/templates` → Templates; `/admin/routing` → Routing.
  * `TestNotificationsInUserMenu`: with the bot configured, `/admin/notifications` is linked inside `id="user-menu"` and not inside `id="sidebar"`.
- [ ] **Step 2: Run** `go test ./internal/httpserver -run 'Sidebar|NotificationsInUserMenu' -race` — expect FAIL.
- [ ] **Step 3: Implement.** `sidebar` loops `viewer.navGroups()`; each group is `<details open data-group={key}>` with a `<summary>` heading (small uppercase, `text-xs font-semibold text-slate-500`) and indented `navLink`s; `navLink` takes `current bool` and sets `aria-current="page"` when true. User menu gains "My notifications" after "My access" when `NotificationsEnabled`. Remove the `aria-current` loop from nav.js.
- [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race && make test-js` — expect PASS.
- [ ] **Step 5: Commit** `feat(ui): group the admin sidebar`.

### Task 4: Remember closed groups

**Files:**
- Modify: `internal/httpserver/views/usermenu.templ` (inline script after `</nav>`)
- Modify: `internal/httpserver/web/nav.js` (store on `toggle`)
- Test: `internal/httpserver/header_test.go`

**Interfaces:**
- Consumes: Task 3's `<details data-group>` markup; the current group carries `data-current`.
- Storage: key `"teamster.nav." + group`, value `"closed"` or removed when opened.

- [ ] **Step 1: Write the failing test** `TestSidebarAppliesGroupStateBeforePaint`: the served sidebar contains a `<script>` between `</nav>` and `</aside>` that reads `teamster.nav.`, and the current group's `<details>` carries `data-current`.
- [ ] **Step 2: Run** it — expect FAIL.
- [ ] **Step 3: Implement.** Inline script, wrapped in `try {} catch {}`: for each `#sidebar details[data-group]:not([data-current])`, `open = localStorage.getItem("teamster.nav." + group) !== "closed"`. nav.js: on each group's `toggle`, `setItem(key, "closed")` when closed, `removeItem(key)` when opened, inside `try`.
- [ ] **Step 4: Run** `make generate && go test ./internal/httpserver -race`, then in a browser against a local server: close Delivery, reload → still closed; navigate to `/admin/teams` → Delivery open; private window → all open.
- [ ] **Step 5: Commit** `feat(ui): remember which sidebar groups are closed`.

### Task 5: Documentation

**Files:**
- Create: `docs/adr/0103-group-the-admin-sidebar-and-give-each-resource-a-page.md` (from `0000-template.md`; amends ADR 0042, cites ADR 0054)
- Modify: `README.md`, `docs/architecture.md` (every `admin?tab=` / `admin?edit=` URL and the "Configuration page with tabs" description)
- Modify: `CHANGELOG.md` (`Unreleased` → `### Changed`: grouped sidebar, one page per resource, notifications in the user menu, renamed labels, old links redirect)

- [ ] **Step 1:** Write the ADR: context (flat nav, tabbed page, links posted into Teams), decision (four groups, page per resource, 301s, details state), alternatives (static headings; keeping tabs with sub-nav), consequences (phase 2 builds on per-resource URLs).
- [ ] **Step 2:** `rtk proxy grep -rn "admin?tab=\|admin?edit=\|tab=" README.md docs/architecture.md` returns nothing after the edits.
- [ ] **Step 3:** Run `make lint coverage test-js` — all pass, coverage ≥ 75 %.
- [ ] **Step 4: Commit** `docs: record the grouped admin navigation`.

### Task 6: User documentation PR on `pages`

- [ ] **Step 1:** `git worktree add ../teamster-pages-admin-nav -b docs/admin-nav origin/pages`.
- [ ] **Step 2:** Spawn a general-purpose agent with `.claude/agents/docs-writer.md` from that worktree as its instructions to update the English pages for the grouped sidebar, the per-resource pages, the moved notifications entry and the renamed labels; then one with `docs-translator.md` for the German pages, using the labels from `internal/i18n/locales/de.json` on `feat/admin-nav-groups`.
- [ ] **Step 3:** Commit, open the PR against `pages` (`agent-gh pr create --base pages`), and link it from the code PR and back.

### Task 7: Pull request

- [ ] Open the PR from `feat/admin-nav-groups` against `main` with the template's checklist filled, the spec as the design note, and the docs PR linked.
