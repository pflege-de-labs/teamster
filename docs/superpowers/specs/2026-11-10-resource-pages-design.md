# Resource pages: list, record and new — design

* Date: 2026-11-10
* Status: Approved in conversation; written for review
* Builds on: [Admin navigation, phase 1](2026-11-10-admin-navigation-design.md) and
  [ADR 0103](../../adr/0103-group-the-admin-sidebar-and-give-each-resource-a-page.md)

## Goal

Phase 1 gave every configuration resource its own page, but each page still stacks a create/edit
form, the full list and a sharing panel. An admin cannot look at one record without a form in the
way, and editing jumps to a different state of a crowded page.

Success:

* Every resource has a **list** page, a **record** page per record and a **new** page.
* The record page shows the record read-only. **Edit** switches the same page to the form without
  a reload; **Cancel** switches back. Saving returns to the read-only record.
* A failed save keeps what was typed and shows the error in the form.
* Links already posted into Teams keep working.

Out of scope: the JSON API, the routing graph, what a role may do, search or filtering on lists.

## Delivery

One pull request per resource, each with a short design note. The first builds the shared frame
together with **Destinations**; then, in order: Templates, Webhooks, Routes, Groups, Access tokens,
Recipients, Users, Delivery permissions.

| Resource | List | Record | New | Edit in place |
| --- | --- | --- | --- | --- |
| Destinations, Templates, Webhooks, Routes, Groups | ✓ | ✓ | ✓ | ✓ |
| Access tokens | ✓ | ✓ | ✓ (secret shown once) | — |
| Recipients, Users | ✓ | ✓ | — | — (unlink / disable only) |
| Delivery permissions | ✓ | — | ✓ | — |

Routes stay a tree in their list; their **new** page takes `?parent=<id>`. Page-wide settings move
in their resource's own PR: the global default route (Routes) and webhook source defaults
(Templates).

## Approach

Shared templ frame components and a small view model per resource. Rejected:

* Extending the single `views.Page` with mode fields — it already carries every list of five
  resources, so every page would keep loading everything.
* A generic Go resource interface driving shared handlers — the resources differ too much
  (preview, one-time secrets, no create for users) for the abstraction to pay off.

## URLs and routing

For a converted resource `<r>`:

| Request | Answer |
| --- | --- |
| `GET /admin/<r>` | list |
| `GET /admin/<r>/new` | new form |
| `GET /admin/<r>/{id}` | record, read-only; `?edit` renders it in edit mode |
| `GET /admin/<r>?edit=<id>` | **301** → `/admin/<r>/<id>?edit` |
| `POST /admin/<r>` | save (existing handler) |
| `POST /admin/<r>/delete` etc. | unchanged, registered with the `POST` method |

* The `?edit=<id>` redirect is permanent: phase 1 posts `/admin/webhooks?edit=<id>` into Teams
  cards, and the phase-1 redirects of `/admin?edit=…` land there.
* Literal sub-paths (`/delete`, `/default`, `/rotate`, …) are registered as `POST /admin/<r>/…`,
  which is what lets `GET /admin/<r>/{id}` coexist with them in `ServeMux`. `new` is a literal
  `GET` pattern and wins over `{id}`.
* `recordEditPath(section, id)` returns `/admin/<r>/<id>` for converted resources and the phase-1
  `?edit=` form for the rest, until every resource is converted.

### Authorization

* `requestAuthorization` already maps every `/admin/<r>…` path to its record type; no change.
* `recordPrefixes` gains `/admin/<r>/`, so someone who only holds shared records reaches the
  handler, which checks `read` on the id.
* A record the viewer may not read, or that does not exist, answers **404** — the page does not
  confirm that a record exists.
* The record page offers Edit only with `update`, Delete only with `delete`; the edit part is not
  rendered without `update`. The POST handlers keep their own checks.

## Pages

New file `internal/httpserver/views/record.templ`:

* `listPage` — header with the title and a **New …** button (only with `create`), then rows or the
  empty state. Each row is one link to the record: name, a secondary line, badges. No buttons on
  rows.
* `recordPage` — breadcrumb (`Destinations › <name>`), action buttons, then a container with
  `<div data-mode="view">` (summary) and `<div data-mode="edit">` (the existing form). One of
  them carries `hidden`, decided by the server: edit when `?edit` is present or a save failed.
* `newPage` — breadcrumb and the form; Cancel returns to the list.

Each resource has its own view structs (for Destinations: `DestinationList`, `DestinationRecord`,
`DestinationNew`) and handlers; the existing form markup is moved, not rewritten.

### Edit in place — `web/record.js`

* **Edit** hides the view part, shows the form, focuses its first field and sets `?edit` with
  `history.replaceState`, so a reload stays in edit mode.
* **Cancel** calls `form.reset()`, swaps back and removes `?edit`.
* Leaving with unsaved changes asks for confirmation (`beforeunload` while the form is dirty).
* Without script, Edit is a link to `?edit` and Cancel a link to the record.

### Saving

* Success: 303 to `/admin/<r>/<id>` with the notice. Delete: 303 to the list.
* Failure: the save handler renders the record page (or the new page, for a record without an id)
  in edit mode with the error and the posted values, status **422**, no redirect. Reloading would
  re-post; the browser warns. Only converted resources take this path; `formPost` stays for the
  rest.

## Destinations

* **List**: name; Team › channel; badges *global default* and *app missing*.
* **Record** summary: name; Team and channel; app install state (from the same lookup the list uses
  today); global default badge.
* **Names**: the page renders the IDs. After load, script resolves them through the cached
  `/api/graph/teams` and `/api/graph/teams/{id}/channels` endpoints and shows the names with the ID
  in small type. On any Graph error the IDs stay.
* **Actions**: Edit, **Make default** (admins, not on the current default), Delete.
* **Used by**: routes whose own destination is this one (with their matchers) and webhook endpoints
  that deliver here, each linking to its record and filtered to what the viewer may read. Routes
  that only inherit the destination are not listed; the section links to the routing graph.
* **Sharing panel** on the record page, return path `/admin/destinations/<id>`.

## Templates

* **List**: name, *Default for …* badge; what it sends, its sources, last update. The **Default
  template per webhook** form stays below the list: it is page-wide, not part of a template, and
  still posts to `/admin/templates/source-defaults`, which is not a record path — no grant reaches
  it.
* **Record** summary: name and badge, sources, what it sends, last update; the title, text and card
  source in a collapsed block.
* **Preview**: an aside beside the record, outside its view and edit parts, so both modes show one
  rendering. It renders once on load. It reads `#template-form` — the edit form, or for someone
  without `update` a hidden form of the stored fields. The preview API answers roles, not grants, so
  someone who only holds a grant gets no preview.
* **Actions**: Edit, Delete.
* **Used by**: routes whose own template this is and webhook endpoints that render with it, each
  filtered to what the viewer may read; the webhooks it is the default for; the global default
  route.
* **New**: the form with presets, card palette and editors, and the preview beside it.
* The CodeMirror editors are built while the edit part is hidden and measure themselves when shown.

## Webhooks

* **List**: `/teamsv2/<team>/<channel>`; the destination and template names.
* **Record** summary: the URL path without its token; the destination and the template, each linking
  to its record when the viewer may read it; the route Teamster keeps for the endpoint. Nothing else
  points at an endpoint, so there is no Used by.
* **Actions**: **New token** and Edit (`update`), Delete (`delete`).
* **New**: team slug, channel slug, destination, template.
* **The token is shown once.** Creating an endpoint or replacing its token answers with the record
  page itself, the URL in the amber banner, not with a redirect that would put the token in the
  browser history and the access log. The page marks itself `data-record-replace`, and `record.js`
  replaces the address with the record's own, so a reload shows the record instead of posting again
  — which, for a create, would make a second endpoint.
* A failed save — a bad slug, a destination the viewer may not deliver to, a template they may not
  use or that does not handle Teams V2 — is the 422 re-render. A failed token replacement redirects
  to the record with the error.

## Tests

Table-driven, `t.Parallel()`, in `internal/httpserver`:

* list, new, record and record `?edit` render the parts above and nothing else;
* unreadable or missing record → 404; share-only user sees the shared record;
* `/admin/destinations?edit=<id>` → 301;
* failed save → 422 with the posted values and the error, edit part visible;
* successful save → 303 to the record; delete → 303 to the list;
* Used by lists only readable routes and webhooks;
* Edit, Make default and Delete appear only with the matching right.

`node:test` under `internal/httpserver/webtest/` for `record.js`'s pure helpers (the `?edit` URL,
matching IDs to names). Browser check: Edit/Cancel swap, the unsaved-changes prompt, names filling
in, old `?edit=` links.

## Documentation

* ADR 0105 "Give each record a page of its own": the frame, edit in place, the 422 failed-save
  path, 404 for unreadable records.
* `docs/architecture.md`, `README.md`, `CHANGELOG.md`.
* `docs/roadmap.md`: the remaining resources as one milestone, removed item by item.
* `pages` pull request in English and German.
