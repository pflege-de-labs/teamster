# Admin navigation and resource pages — design

* Date: 2026-11-10
* Status: Phase 1 approved in conversation; phase 2 outlined, designed per resource later

## Goal

The admin sidebar is one flat list of up to 14 links, and `/admin` ("Configuration") is a single page
with five tabs, each stacking a create/edit form, the list of records and a sharing panel. An admin
cannot tell configuration from permission management or global administration, and cannot link to a
record.

Success:

* The sidebar has two levels and says which items configure delivery, which manage access and which
  administer the installation.
* Every configuration resource is a sidebar item with its own URL.
* (Phase 2) Every resource has a list page, a page per record and a separate page to create one.
  Editing a record switches its page between viewing and editing without leaving it.

Out of scope: the JSON API (`/api/...`), the routing graph, config import/export (API only, no UI
today), any change to what a role may do.

## Phases

1. **Navigation** (one PR): grouped sidebar, the five tabs become five pages, old URLs redirect.
   Pages keep today's panel content.
2. **Resource pages** (one PR per resource, templates first): list / view / new, edit in place.

The nav progress bar for slow pages ships separately (`feat/nav-progress`) and is not part of this
design.

## Phase 1 — navigation

### Sidebar

Four groups, each a `<details>` element with a heading:

| Group | Items (visible when) |
|---|---|
| Configuration | Routing `/admin/routing`, Routes `/admin/routes`, Templates `/admin/templates`, Destinations `/admin/destinations`, Webhooks `/admin/webhooks` |
| Delivery | Teams `/admin/teams`, Recipients `/admin/recipients`, Broadcasts `/admin/broadcasts` (CanBroadcast) |
| Access | Access overview `/admin/access` (CanShare), Delivery permissions `/admin/grants` (CanManage), Channel scopes `/admin/permissions` (CanManage), Groups `/admin/groups`, Users `/admin/users` (CanManage), Access tokens `/admin/tokens` (CanTokens) |
| Administration | People `/admin/people` (CanManage && PeopleEnabled), Audit trail `/admin/audit` (CanAudit) |

* A group with no visible item is not rendered.
* **Notifications** (`/admin/notifications`, personal chat link) moves to the user menu as
  "My notifications", beside "My access"; shown when NotificationsEnabled.
* The current item is marked server-side with `aria-current="page"` by **path prefix**
  (`/admin/templates/abc` marks Templates), replacing nav.js's exact-path match. The handler passes
  the current path through `Viewer` (or context) to the sidebar.
* Every group renders `open`. A group the user closed is remembered in `localStorage`
  (`teamster.nav.<group>` = `closed`) and closed again by an inline script directly after the
  sidebar's `<nav>` — the `<head>` script of ADR 0054 runs before the `<details>` exist. The group
  holding the current item is never closed. nav.js stores the state on `toggle`. Without storage,
  every group is open.
* Labels: new keys `nav.group.configuration|delivery|access|administration`, `nav.routes`,
  `nav.templates`, `nav.destinations`, `nav.webhooks`, `nav.grants` ("Delivery permissions"),
  `nav.my_notifications`; `nav.permissions` becomes "Channel scopes". English and German.
  `nav.configuration` is removed.

### Pages

| GET | Renders | Singletons kept on the page |
|---|---|---|
| `/admin/routes` | routes panel | global default route |
| `/admin/templates` | templates panel | webhook source defaults |
| `/admin/destinations` | destinations panel | default destination |
| `/admin/webhooks` | webhooks panel | — |
| `/admin/grants` | grants panel | — |

* Laziest form: keep `views.Page` and the existing panel components. `Admin(page)` renders the one
  panel `page.Tab` names; `tabBar` and `tabPanel` go. `tabs.js` stays — the routing page uses it.
* `?edit=<id>` on a resource page opens that record in the panel's form, as `?edit=<tab>&id=<id>`
  does today. Phase 2 replaces it.
* The webhook secret reveal (`revealWebhookURL`) renders the webhooks page.
* The sharing panel's return path becomes `/admin/<resource>?edit=<id>`.

### Routing and methods

Go 1.26 `ServeMux` method patterns. Each resource path gets `GET /admin/<resource>` (page) and keeps
its form post as `POST /admin/<resource>`. Literal sub-paths (`/delete`, `/rotate`, `/default`,
`/global-default`, `/source-defaults`) are unaffected. Any other method answers 405 with `Allow`.
`/admin/webhooks` keeps `handleWebhookForm` for POST.

### Redirects

| From | To | Status |
|---|---|---|
| `GET /admin` | `/admin/routing` when the viewer may `view` `Page`, else `/admin/templates` (someone who only holds shared records) | 302 |
| `GET /admin?tab=<t>` | `/admin/<t>` | 301 |
| `GET /admin?edit=<t>&id=<id>` | `/admin/<t>?edit=<id>` | 301 |
| `?notice=` / `?error=` on any of the above | carried over | — |

`/admin?edit=webhooks&id=…` links are already posted into Teams by the hint cards
(`teamsv2.go`, `webhooks.go`), so these redirects are permanent. Every link builder switches to the
new form: `sharing_hub.go` `recordSections` paths, `admin_ui.go` sharing return, `teamsv2.go` hint
card, `webhooks.go` delivery hint and `templatesPanelPath`, `components.templ` `editLink` and
`cancelEdit`, `admin.templ` route and webhook cross-links. `#section` anchors are dropped.

### Form posts

`formPost(tab, …)` and `adminTabPath(tab)` redirect to `/admin/<tab>` instead of `/admin?tab=<tab>`.
Errors still travel as `?error=` and the form's input is still lost on a failed save, as today.

### Authorization

* `recordPaths` already lists `/admin/templates` etc. for the POSTs; the GET pages share those paths,
  so someone a record was shared with reaches the page and its handler filters the list, as `/admin`
  does today. `/admin/grants` stays under `ActionAdminister` on `Grant` via `requestAuthorization`.
* `requestAuthorization` already maps `GET /admin/<resource>` to `view` on that resource's type
  (prefix cases for `/admin/templates` etc.); no change. `/admin` itself stays `view` on `Page`.
* Tests pin, per role (viewer, editor, admin, record-shared user, no role): GET of each page →
  200 / 403 / no-access page, and that a viewer still gets 403 on each POST.

### Tests

Table-driven, parallel, in `internal/httpserver`:

* each new GET path renders its panel and only its panel;
* redirect table above, including notice/error carry-over and unknown tab → `/admin`'s target;
* sidebar: groups per role, empty group omitted, `aria-current` by prefix, current group `open`;
* existing tests that assert `GET /admin/templates` → 405 or `Location: /admin?tab=…` are rewritten;
* `make test-js` for the nav.js group-state logic if it grows beyond reading and writing one key.

### Documentation

* ADR 0103 "Group the admin sidebar and give each resource a page" (no earlier ADR covers the tabs;
  it amends ADR 0042's sidebar).
* README and `docs/architecture.md`: drop `admin?tab=` URLs.
* CHANGELOG `Unreleased` / Changed.
* `pages` PR, English and German: navigation description, renamed labels ("Channel scopes",
  "Delivery permissions", "My notifications").

## Phase 2 — resource pages (outline)

Applies to:

| Resource | List | View | New | Edit in place |
|---|---|---|---|---|
| Templates, Destinations, Webhooks, Routes, Groups | ✓ | ✓ | ✓ | ✓ |
| Access tokens | ✓ | ✓ | ✓ (secret shown once) | — |
| Recipients, Users | ✓ | ✓ | — | — (unlink / disable only) |
| Delivery permissions (grants) | ✓ | — | ✓ | — |

URLs: `/admin/<resource>` list, `/admin/<resource>/new` create, `/admin/<resource>/{id}` view.
Routes stay a tree in the list; `new` takes `?parent=<id>`.

The view page renders both a read-only summary and the form. An **Edit** button swaps them by
script without reloading; **Cancel** swaps back and resets the form. Without script, Edit is a link
to `?edit` that renders the form. Save posts and redirects to the view page; a failed save renders
the view page in edit mode with the error. The view page carries the sharing panel and, where it
applies, what uses the record (e.g. routes using a template) and the template's rendered preview.

Each resource's PR settles its own detail — what the summary shows, what "used by" lists — in its
design note.
