# Teams V2 Routing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development
> (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Route `/teamsv2/...` messages through label routing, with one linked, locked route per
endpoint, so further routes can deliver the same message elsewhere.

**Architecture:** `routes.webhook_endpoint_id` links a route to its endpoint. Endpoint writes keep
the linked route in sync inside the same transaction; route writes refuse to change what the
endpoint owns. `handleTeamsV2` builds a `models.Event` and calls `processEvent`, falling back to
the old direct post when an endpoint has no linked route. A startup reconcile backfills and repairs.

**Tech Stack:** Go, goose migrations, sqlc (SQLite + Postgres), templ, kong.

**Spec:** `docs/superpowers/specs/2026-11-05-teamsv2-routing-design.md`

## Global Constraints

* Labels: `teamster_team`, `teamster_channel`; values are the URL slugs.
* Linked route: name `webhook <team>/<channel>`, priority 0, not greedy, not default, selector
  `teamster_source=teamsv2, teamster_team=<team>, teamster_channel=<channel>`.
* Column: `routes.webhook_endpoint_id TEXT NOT NULL DEFAULT ''`, partial unique index
  `WHERE webhook_endpoint_id <> ''`. No foreign key.
* Locked on a linked route: selector, destination, recipient, addressed, template, parent, default.
  Editable: name, priority, greedy.
* Route delete refusal text: `managed by webhook <team>/<channel>; delete the webhook instead`.
* Migrations are additive; an accepted migration is never edited; edit `.sql`/`.templ`, then
  `make generate`, never the generated Go.
* Tests are table-driven and call `t.Parallel()`.
* Done means `make coverage` (≥ 75 %) and `make lint` pass.
* Commit with `git add` + `agent-commit -m "<conventional subject>" -m "Co-Authored-By: Claude Opus
  5.5 <noreply@anthropic.com>"`, never `git commit`.

## Review Focus

1. A universal sender posting `teamster_team`/`teamster_channel` labels must not reach a linked
   route — Task 2 test `TestWithSourceLabelStripsEndpointLabels`, Task 3 test case
   `forged labels on universal`.
2. Renaming an endpoint's slugs must move its route's selector, so the new URL still delivers —
   Task 4 case `update rewrites selector`.
3. An endpoint whose linked route vanished (previous release) must still post to its destination,
   not the global default — Task 3 case `fallback without linked route`.
4. A second reconcile run (another replica, next start) must change nothing — Task 6 case
   `second run is a no-op`.
5. An API `PUT` that resends a linked route unchanged except priority must succeed — Task 5 case
   `priority change accepted`.

---

### Task 1: Schema and store

**Files:**

* Create: `internal/store/migrations/sqlite/0032_route_webhook_endpoint.sql`,
  `internal/store/migrations/postgres/0029_route_webhook_endpoint.sql`
* Modify: `internal/store/queries/{sqlite,postgres}/routes.sql`, `internal/models/models.go`
  (`Route`), `internal/store/adapter.go`, `internal/store/pgqueries.go`, `internal/store/store.go`,
  `internal/audit/store.go`, `internal/httpserver/fakes_test.go`
* Test: `internal/store/conformance_test.go`

**Interfaces:**

* Produces: `models.Route.WebhookEndpointID string` with tag `json:"webhook_endpoint_id,omitempty"`;
  `store.Store.UnlinkRoute(ctx context.Context, id string) error` (sets the column to `''`).
  `CreateRoute` writes the column; `UpdateRoute` never touches it; `ListRoutes`/`GetRoute` read it.

* [ ] **Step 1:** Add `TestConformanceRouteWebhookEndpoint` (via `eachBackend`): create a route with
  `WebhookEndpointID: "hook"`, `GetRoute` returns `"hook"`; `UpdateRoute` with
  `WebhookEndpointID: ""` leaves `"hook"`; a second `CreateRoute` with `"hook"` returns an error;
  `UnlinkRoute` makes it `""`; two routes with `""` coexist.
* [ ] **Step 2:** `go test ./internal/store -run TestConformanceRouteWebhookEndpoint` — FAIL
  (field undefined).
* [ ] **Step 3:** Write both migrations (comment: the previous release ignores the column), add the
  column to the `ListRoutes`/`GetRoute` selects and `CreateRoute` insert, add
  `-- name: UnlinkRoute :exec`, run `make generate`, then wire model, both adapters, the interface,
  the audit wrapper (audited like `UpdateRoute`) and `fakeStore` (fake enforces the uniqueness).
* [ ] **Step 4:** `go test ./internal/store ./internal/audit ./internal/httpserver` — PASS.
* [ ] **Step 5:** Commit `feat(store): link a route to a webhook endpoint`.

### Task 2: Event carries Teams V2 data; labels are trusted; rendering keeps payload and cards

**Files:**

* Modify: `internal/models/models.go` (`Event`, `WithSourceLabel`), `internal/templates/render.go`
  (`Message`), `internal/routing/router.go` (`Delivery`, `collect`),
  `internal/httpserver/webhooks.go` (`renderMessageFor`, `directMessage`, `channelMessage`)
* Test: `internal/models/models_test.go`, `internal/routing/router_test.go`,
  `internal/httpserver/webhooks_test.go` (or the file holding `directMessage` tests)

**Interfaces:**

* Produces:
  * `models.TeamLabel = "teamster_team"`, `models.ChannelLabel = "teamster_channel"`.
  * `models.TeamsV2Event{Payload any; Cards []json.RawMessage}`; `Event.TeamsV2 *TeamsV2Event`
    (`json:"teamsv2,omitempty"`).
  * `WithSourceLabel` deletes `TeamLabel`/`ChannelLabel` unless `source == SourceTeamsV2`.
  * `templates.Message.More []json.RawMessage` — cards after `Card`; `channelMessage` appends them
    before `Notice`; chat ignores them.
  * `routing.Delivery.WebhookEndpointID string`, copied from the route in `collect`.

* [ ] **Step 1:** Write tests:
  `TestWithSourceLabelStripsEndpointLabels` — `universal` with `teamster_team=x` → key absent;
  `teamsv2` keeps both; `teamster_source` always overwritten.
  `TestPlanCarriesWebhookEndpointID` — a route with `WebhookEndpointID: "hook"` yields a delivery
  with `"hook"`.
  `TestRenderPassesTeamsV2Payload` — template text `{{ .Payload.themeColor }}`, event
  `TeamsV2.Payload = {"themeColor":"FF0000"}` → text contains `FF0000`.
  `TestDirectMessageForwardsEveryCard` — event with `Card = c1`, `TeamsV2.Cards = [c1, c2]` →
  `channelMessage` gives `Cards == [c1, c2, hint]`.
  `TestHintLinksTheEndpoint` — delivery with `WebhookEndpointID: "hook"` and no template → notice
  contains `/admin?edit=webhooks&id=hook`; without it → `templatesPanelPath`.
* [ ] **Step 2:** Run `go test ./internal/models ./internal/routing ./internal/httpserver -run
  'StripsEndpointLabels|CarriesWebhookEndpointID|PassesTeamsV2Payload|ForwardsEveryCard|HintLinks'`
  — FAIL.
* [ ] **Step 3:** Implement. `renderMessageFor` sets `RenderData.Payload` from `ev.TeamsV2` when
  non-nil; `directMessage` sets `More = Cards[1:]`; hint path is
  `/admin?edit=webhooks&id=<url.QueryEscape(id)>#webhooks` when `delivery.WebhookEndpointID != ""`.
  `channelMessage`'s text-only hint keeps `templatesPanelPath`
  (`shortcut: text-only hint links the templates panel, pass the path when a teamsv2 sender needs it`).
* [ ] **Step 4:** Same command — PASS; `go test ./...` — PASS.
* [ ] **Step 5:** Commit `feat: carry Teams V2 payload and cards through routed delivery`.

### Task 3: Route Teams V2 messages

**Files:**

* Modify: `internal/httpserver/teamsv2.go`
* Test: `internal/httpserver/teamsv2_test.go`

**Interfaces:**

* Consumes: Task 1 `WebhookEndpointID`; Task 2 labels and `Event.TeamsV2`.
* Produces: `(s *Server) linkedRoute(ctx, endpointID string) (models.Route, bool, error)` — scans
  `ListRoutes`.

* [ ] **Step 1:** Add `teamsV2RoutedServer` (like `teamsV2Server`, plus linked route `"route-hook"`
  to `"dest"`) and table test `TestTeamsV2IsRouted` with cases:
  `linked route only` → one post to `team/channel`, body is `writeReport`'s;
  `independent route on teamster_team=platform` to `dest2` → posts to both;
  `child route under linked route` with `teamster_channel=alerts` and own destination → both;
  `fallback without linked route` (plain `teamsV2Server`) → one post to `team/channel`, no
  global default used even when one is set;
  `forged labels on universal` — universal event with `teamster_team=platform,
  teamster_channel=alerts` does not post to `dest`.
  Keep every existing `TestTeamsV2*` test passing unchanged except the success-body assertion.
* [ ] **Step 2:** `go test ./internal/httpserver -run TestTeamsV2` — FAIL.
* [ ] **Step 3:** After parse and the accepted metric: `linkedRoute`; when absent, keep today's
  direct post (`teamsV2Message` + `PostToChannel`) unchanged; when present, build
  `models.Event{Source: teamsV2Source, State: models.StateNone, Labels: {TeamLabel, ChannelLabel},
  Title, Text, Card: Cards[0], TeamsV2: &TeamsV2Event{payload, msg.Cards}}`, call
  `processEvent`, answer with `writeReport`. Update the `handleTeamsV2` doc comment to cite
  ADR 0094.
* [ ] **Step 4:** `go test ./internal/httpserver` — PASS.
* [ ] **Step 5:** Commit `feat(teamsv2): route messages through the linked route`.

### Task 4: Endpoint writes keep the linked route in sync

**Files:**

* Create: `internal/httpserver/endpoint_routes.go`
* Modify: `internal/httpserver/access.go` (`createEndpoint`, `deleteEndpoint`, new
  `updateEndpoint`), `internal/httpserver/webhooks_admin.go`, `internal/httpserver/webhooks_api.go`
* Test: `internal/httpserver/endpoint_routes_test.go`

**Interfaces:**

* Produces:
  * `endpointRoute(e models.WebhookEndpoint) models.Route` — the Global Constraints shape.
  * `syncEndpointRoute(ctx, tx store.Store, r *http.Request, e models.WebhookEndpoint) error`
    — creates (and `grantOwner`s) or updates the linked route.
  * `(s *Server) updateEndpoint(r *http.Request, e models.WebhookEndpoint)
    (models.WebhookEndpoint, error)` — used by the form and the API `PUT` instead of
    `store.UpdateWebhookEndpoint`.
  * `errEndpointRouteHasChildren` wrapping `routing.ValidateDelete`'s error.

* [ ] **Step 1:** Table test `TestEndpointRouteSync` over form and API:
  `create makes owned linked route` (selector, name, destination, template, owner permission);
  `update rewrites selector` (slugs `platform/alerts` → `ops/pager`);
  `update changes destination and template`;
  `update recreates a missing route`;
  `delete removes route and its permissions`;
  `delete refused while route has children` (endpoint and route still present).
* [ ] **Step 2:** `go test ./internal/httpserver -run TestEndpointRouteSync` — FAIL.
* [ ] **Step 3:** Implement; create, update and delete each run in one `WithTx`. The selector
  change must not go through `saveRouteChecked` (Task 5's lock would refuse it).
* [ ] **Step 4:** `go test ./internal/httpserver` — PASS.
* [ ] **Step 5:** Commit `feat(webhooks): keep each endpoint's route in sync`.

### Task 5: Linked routes are locked

**Files:**

* Modify: `internal/httpserver/admin_ui.go` (`saveRouteChecked`, `deleteRouteChecked`),
  `internal/httpserver/views/admin.templ` (route form, endpoint form),
  `internal/httpserver/views/views.go`, `internal/i18n/locales/{en,de}.json`
* Test: `internal/httpserver/routing_api_test.go`, `internal/httpserver/admin_ui_test.go`

**Interfaces:**

* Consumes: Task 4 `endpointRoute`.
* Produces: `errRouteManaged` (`invalidRoute`), `lockedFieldsChanged(old, new models.Route) bool`.

* [ ] **Step 1:** Table test `TestLinkedRouteIsLocked` over API `PUT`, form save and delete:
  one case per locked field → refused, route unchanged;
  `priority change accepted`, `name change accepted`, `greedy change accepted`;
  `delete refused` with text `managed by webhook platform/alerts; delete the webhook instead`;
  `create cannot set webhook_endpoint_id` (stored route has `""`);
  `child under linked route accepted`.
  Admin UI test: route form for a linked route renders locked fields disabled and a link to
  `/admin?edit=webhooks&id=hook#webhooks`; endpoint form links to its route.
* [ ] **Step 2:** `go test ./internal/httpserver -run 'LinkedRouteIsLocked|Admin'` — FAIL.
* [ ] **Step 3:** In `saveRouteChecked`: clear `WebhookEndpointID` on create; on update, look up
  the stored route in `existing` and refuse when linked and `lockedFieldsChanged`. In
  `deleteRouteChecked`: refuse a linked route. Templ: disabled inputs plus a hidden copy of each
  locked value so the form posts them unchanged; add i18n keys `routes.managed_by`,
  `webhooks.route_link` in both catalogs (German uses existing UI terms from `de.json`). Run
  `make generate`.
* [ ] **Step 4:** `go test ./internal/httpserver` — PASS.
* [ ] **Step 5:** Commit `feat(routes): lock the route a webhook manages`.

### Task 6: Startup reconcile

**Files:**

* Modify: `internal/httpserver/endpoint_routes.go`, `internal/cli/serve.go` (after `seedPresets`)
* Test: `internal/httpserver/endpoint_routes_test.go`

**Interfaces:**

* Produces: `ReconcileEndpointRoutes(ctx context.Context, st store.Store) (Reconciled, error)`,
  `type Reconciled struct{ Created, Deleted, Unlinked int }`.

* [ ] **Step 1:** Table test `TestReconcileEndpointRoutes`:
  `backfill copies owners` (endpoint with two `own` permissions, no route → route created, both
  principals own it);
  `repair after route deleted`;
  `orphan deleted` (linked route, no endpoint → route and its permissions gone);
  `orphan with children unlinked` (`WebhookEndpointID == ""`, route kept);
  `second run is a no-op` (`Reconciled{}`).
* [ ] **Step 2:** `go test ./internal/httpserver -run TestReconcileEndpointRoutes` — FAIL.
* [ ] **Step 3:** Implement in one `WithTx`; each create uses `endpointRoute`. In `serve.go` call it
  with `auditedStore`; log `Info` with counts when non-zero, `Warn` on error and keep starting
  (the Task 3 fallback still delivers).
* [ ] **Step 4:** `go test ./internal/httpserver ./internal/cli` — PASS.
* [ ] **Step 5:** Commit `feat: reconcile webhook routes at startup`.

### Task 7: Export and import leave linked routes alone

**Files:**

* Modify: `internal/transfer/bundle.go` (`Export`), `internal/transfer/import.go`
* Test: `internal/transfer/*_test.go`

**Interfaces:**

* Produces: `managedRouteIDs(routes []models.Route) map[string]bool` — linked routes and all their
  descendants.

* [ ] **Step 1:** Table test `TestTransferSkipsManagedRoutes`:
  `export omits linked route and its child`;
  `replace import keeps linked route and its child`;
  `import refuses a bundle route whose id is a linked route`;
  `imported route never carries a link` (bundle route with `webhook_endpoint_id` → stored `""`).
* [ ] **Step 2:** `go test ./internal/transfer -run TestTransferSkipsManagedRoutes` — FAIL.
* [ ] **Step 3:** Implement.
* [ ] **Step 4:** `go test ./internal/transfer` — PASS.
* [ ] **Step 5:** Commit `feat(transfer): leave webhook-managed routes out of bundles`.

### Task 8: Documentation and definition of done

**Files:**

* Create: `docs/adr/0094-a-teams-v2-message-is-routed.md` (from `0000-template.md`)
* Modify: status lines of `docs/adr/0030-…`, `0052-…`, `0055-…` ("amended by 0094"),
  `docs/adr/README.md`, `docs/architecture.md`, `README.md`, `CHANGELOG.md` (`Unreleased`),
  `docs/roadmap.md` if it mentions this

* [ ] **Step 1:** Write ADR 0094 from the spec: decision, rejected approaches, the five behaviour
  changes, export trade-off, schema compatibility.
* [ ] **Step 2:** Update the other files; CHANGELOG entry under `Changed` naming the fan-out,
  response body and metric label changes.
* [ ] **Step 3:** `make coverage && make lint` — both pass, coverage ≥ 75 %.
* [ ] **Step 4:** Commit `docs: record routing of Teams V2 messages (ADR 0094)`.
* [ ] **Step 5:** Open the code PR with `agent-gh pr create` (PR template checklist).
* [ ] **Step 6:** Worktree `../teamster-pages-teamsv2-routing` from `origin/pages`; docs-writer
  agent for English, docs-translator for German (`*.de.md`, labels from `de.json`); open the
  `pages` PR and link both PRs to each other.
