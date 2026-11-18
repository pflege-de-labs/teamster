# Permission management UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Name principals with one searchable picker, show grants compactly, split the group page
into members / what it may do / who manages it, and answer "Who can …?" and "What may … do?".

**Architecture:** Server-rendered templ views stay the source of truth; a progressive-enhancement
script turns the picker `<select>` into a filter box. Every access answer comes from the
authorizer snapshot (`AllowFor` / `Explain`), with a synthetic principal for groups, provider
groups and roles; no Go re-implementation of the policy rules.

**Tech Stack:** Go, templ, Tailwind (generated, committed), vanilla JS in `internal/httpserver/web/`,
`node:test`, cedar-go.

**Spec:** `docs/superpowers/specs/2026-11-18-permission-ui-design.md`

## Global Constraints

* Three pull requests, stacked: PR 1 `feat/permission-ui` (Tasks 1–3, carries spec and plan),
  PR 2 `feat/group-access` (Tasks 4–7), PR 3 `feat/access-check` (Tasks 8–10). Use the `gh-stack`
  skill; commit with `agent-commit`, never `git commit`.
* PR 1 starts after #327 is merged: rebase onto `origin/main` first (it adds `levelRemove` and the
  unknown-group check in `access_page.go`).
* Edit `.templ`, never `*_templ.go`; run `make generate` and commit the generated Go and CSS.
* Every new UI string goes into both `internal/i18n/locales/en.json` and `de.json`.
* Tests are table-driven and call `t.Parallel()`. Browser logic is tested under
  `internal/httpserver/webtest/` with `node:test`, never inside `web/`.
* Each PR: `make lint`, `make coverage` (≥ 75 %), `make test-js` pass; `docs/architecture.md`,
  ADR, `README.md` and an `Unreleased` entry in `CHANGELOG.md`; a docs PR on `pages` (English and
  German, `docs-writer` then `docs-translator`, opened with `--base pages`), linked both ways.
* No change to the grants table, Cedar base policies or any endpoint's semantics. Without
  JavaScript every form still works.
* Action reference positions are fixed: `r w d a s` = read, update, delete, attach, share; `-` when
  not held; a principal holding `own` shows `owner`.

## Review Focus

1. A principal id containing `:` (provider group names may) — `principalFromForm` cuts at the first
   `:` only; picker values must round-trip (Task 1 test).
2. A grant row whose principal no longer exists (deleted group, stale `it-admins` row) must still
   render in the compact list with its raw id and stay revocable (Task 4 test).
3. A group nested in itself through another group (`store.ErrGroupCycle` prevents writes, but the
   ancestor walk must terminate) — the synthetic group principal must not loop (Task 5 test).
4. More users than the check's user limit — the page must say the answer covers only the first N
   (Task 9 test).
5. The check asked with an action that does not fit the resource (`broadcast` on a webhook) must
   say so instead of "Refused" (Task 9 test).

---

## PR 1 — Principal picker

### Task 1: Picker component, wired into the sharing panel and the hub

**Files:**
- Modify: `internal/httpserver/views/sharing.templ` (replace `principalSelect`)
- Modify: `internal/httpserver/views/hub.templ:51`
- Modify: `internal/httpserver/sharing.go` (`principalOptionsFor`)
- Test: `internal/httpserver/sharing_test.go` (or the existing sharing test file)

**Interfaces:**
- Produces: `templ principalPicker(p PickerConfig)` with
  `type PickerConfig struct { Name string; Options PrincipalOptions; Kinds []string; Selected string; AllowOther bool; Required bool }`.
  `Kinds` ⊆ `user`, `group`, `idp_group`, `role`, in that order; empty means all.
  Renders `<select name={Name} data-principal-picker>` with one `<optgroup>` per kind, option
  `value="kind:id"`, text = display name, `data-search` = name, email and id joined by spaces,
  `title` = id. With `AllowOther`, the existing collapsed "Someone not listed" block
  (`other_type` / `other_id`) follows, offering only the allowed kinds except `group`.
- `PrincipalOptions` gains nothing; `models.User` already carries `Email`.

- [ ] **Step 1: Write the failing test** `TestPrincipalPickerRendersKinds`: render the sharing panel
  of a template for an admin with one user (`Subject: "s-1", Name: "Bob", Email: "bob@x"`), one
  group (`g-1`, `Ops`), one provider group `a:b` (contains a colon) and assert the body contains
  `data-principal-picker`, `value="user:s-1"`, `data-search="Bob bob@x s-1"`, `value="group:g-1"`,
  `value="idp_group:a:b"`, `value="role:editor"`. Then post `/admin/sharing/grant` with
  `principal=idp_group:a:b` and assert the stored row has `PrincipalID == "a:b"`.
- [ ] **Step 2: Run** `go test ./internal/httpserver/ -run TestPrincipalPickerRendersKinds` — FAIL
  (no `data-principal-picker`).
- [ ] **Step 3: Implement** `principalPicker` and `PickerConfig` in `sharing.templ`; replace both
  `@principalSelect(...)` calls with `@principalPicker(PickerConfig{Name: "principal", Options: …, AllowOther: true})`.
  Delete `principalSelect`.
- [ ] **Step 4: Run** `make generate && go test ./internal/httpserver/` — PASS.
- [ ] **Step 5: Commit** `feat(ui): one principal picker for the sharing forms`.

### Task 2: The search script

**Files:**
- Create: `internal/httpserver/web/principal-picker.js`
- Create: `internal/httpserver/webtest/principal-picker.test.mjs`
- Modify: `internal/httpserver/views/layout.templ` (add `<script src="/principal-picker.js"></script>` after `pickers.js`)

**Interfaces:**
- Produces: `globalThis.teamsterFilterPrincipals(entries, query, limit) -> entries`, where an entry
  is `{ value, label, kind, search }`. Case-insensitive substring match of every whitespace-separated
  query word against `label + " " + search`; result ordered by kind (`user`, `group`, `idp_group`,
  `role`) then label (locale compare); at most `limit` (default 50). Empty query returns the first
  `limit` in that order.
- The DOM part enhances every `select[data-principal-picker]`: a text input above it, a listbox
  (`role="listbox"`, options `role="option"`) grouped by kind with the optgroup label as badge;
  ArrowUp/ArrowDown move, Enter picks, Escape closes; picking sets `select.value` and dispatches
  `change`. The `<select>` is hidden (`hidden` attribute) only after the input exists.
  Badge texts come from the optgroup labels, so no strings live in the script.

- [ ] **Step 1: Write the failing test** `principal-picker.test.mjs` (import pattern of
  `nav.test.mjs`): table cases — `"bob"` matches label `Bob`; `"x.de"` matches via `search`;
  `"ops bob"` needs both words; ordering puts a `group` after a `user` with a later label; `limit`
  2 of 3 returns 2; empty query returns all in kind order.
- [ ] **Step 2: Run** `make test-js` — FAIL (`teamsterFilterPrincipals` undefined).
- [ ] **Step 3: Implement** `principal-picker.js` as an IIFE over `window`/`globalThis` like
  `nav.js`, exporting the filter on `globalThis`, DOM enhancement guarded by `typeof document`.
- [ ] **Step 4: Run** `make test-js` — PASS; `make generate`; open `/admin/access` in the running
  app (skill `run`) and pick a user with the keyboard.
- [ ] **Step 5: Commit** `feat(ui): search the principal picker`.

### Task 3: Every free-text principal field uses the picker

**Files:**
- Modify: `internal/httpserver/views/access.templ` (webhook and message level forms)
- Modify: `internal/httpserver/views/groups.templ` (Add member form; drop the `datalist`)
- Modify: `internal/httpserver/access_page.go` (`webhookLevelForm`, `messageLevelForm`)
- Modify: `internal/httpserver/groups_page.go` (`handleGroupMemberForm`, `groupDetail`, `handleAccessPage` options)
- Modify: `internal/httpserver/views/access.templ` `Access` struct: add `Principals PrincipalOptions`
- Modify: `internal/httpserver/views/groups.templ` `GroupDetail`: replace `Users`, `Others`, `IdPGroups` with `Principals PrincipalOptions`
- Create: `docs/adr/NNNN-name-principals-with-one-searchable-picker.md` (next free number)
- Modify: `docs/architecture.md`, `CHANGELOG.md`, `README.md` where it describes these forms
- Test: `internal/httpserver/access_page_test.go`, `internal/httpserver/groups_page_test.go`

**Interfaces:**
- Consumes: `principalPicker`, `PickerConfig` (Task 1); `principalFromForm` (exists, reads
  `principal`, then `other_*`, then `principal_type`/`principal_id`).
- Level forms post `principal` (picker, kinds all, `AllowOther: true`); handlers call
  `principalFromForm(r)`. The JSON APIs and `levelRemove` keep `principal_type`/`principal_id`.
- Member form posts `principal` (kinds `user`, `group`, `idp_group`, `AllowOther: true`; the
  group itself is left out of `Options.Groups`); handler passes `string(kind), id` from
  `principalFromForm` to `memberFrom`.

- [ ] **Step 1: Write the failing tests**: `TestWebhookLevels` and `TestMessageLevels` post
  `principal=group:g-ops` (and one case with `other_type=user, other_id=new@x`) and assert the
  same rows as today; `TestGroupMemberAddByPicker` posts `principal=user:s-1` to
  `/admin/groups/members/add` and asserts the member; the groups page body contains
  `data-principal-picker` and no `<datalist`.
- [ ] **Step 2: Run** `go test ./internal/httpserver/ -run 'Level|GroupMember'` — FAIL.
- [ ] **Step 3: Implement** the template and handler changes listed above.
- [ ] **Step 4: Run** `make generate && make lint && make coverage && make test-js` — PASS.
- [ ] **Step 5: Write** the ADR (context: ids typed by hand caused the `it-admins` grant that
  matched nobody, #327; decision: one server-rendered select enhanced by a script; consequences),
  architecture and changelog entries.
- [ ] **Step 6: Commit** `feat(ui): pick principals instead of typing ids`; open PR 1; open the
  `pages` docs PR and link both ways.

## PR 2 — Compact grant list and group page

### Task 4: Compact grant list

**Files:**
- Modify: `internal/httpserver/views/sharing.templ` (`sharingPanel`, `sharedRow`)
- Create: `internal/httpserver/views/actionref.go`, `internal/httpserver/views/actionref_test.go`
- Modify: `internal/i18n/locales/en.json`, `de.json`

**Interfaces:**
- Produces: `func ActionRef(actions []string) string` — `"owner"` when `own` is held, else five
  characters for read, update, delete, attach, share, `-` where missing (`[]string{"read","attach"}`
  → `"r--a-"`).
- `sharingPanel` renders a `<ul>` of rows: type badge, label, `<code title="read, attach">r--a-</code>`,
  and for rows the viewer may change a `<details>` "edit" holding the existing checkbox form with
  Save and Revoke in one `flex` row. The new-grant form sits in a `<details>` "Give someone access".
  Legend line `sharing.legend`.
- `Sharing` gains nothing; the heading key becomes `"sharing.heading." + ResourceType`.
- New keys (en / de): `sharing.heading.Template` "Who may manage this template" / "Wer diese
  Vorlage verwalten darf"; likewise `Destination` (Ziel, dieses), `Route` (Route, diese),
  `WebhookEndpoint` (Webhook, diesen), `Group` (Gruppe, diese); `sharing.edit` "edit" /
  "bearbeiten"; `sharing.add` "Give someone access" / "Jemandem Zugriff geben";
  `sharing.legend` "r read · w change · d delete · a use in routes and webhooks · s share" /
  German with the `sharing.action.*` labels from `de.json`. Remove `sharing.heading`.

- [ ] **Step 1: Write the failing tests**: `TestActionRef` table — nil → `"-----"`;
  read+attach → `"r--a-"`; all five → `"rwdas"`; own+read → `"owner"`; unknown action ignored.
  `TestSharingPanelCompact` (httpserver): a template with a grant to a deleted group `gone` and a
  user owner renders `r--a-` / `owner`, the raw id `gone`, one `<details` per changeable row, and
  revoking the `gone` row via `/admin/sharing/revoke` removes it.
- [ ] **Step 2: Run** `go test ./internal/httpserver/...` — FAIL.
- [ ] **Step 3: Implement** `ActionRef` and the template changes.
- [ ] **Step 4: Run** `make generate && go test ./internal/httpserver/...` — PASS.
- [ ] **Step 5: Commit** `feat(ui): list grants compactly with an action reference`.

### Task 5: Synthetic principals and reasons in `internal/authz`

**Files:**
- Modify: `internal/authz/principal.go`, `internal/authz/render.go`
- Test: `internal/authz/principal_test.go`, `internal/authz/render_test.go`

**Interfaces:**
- Produces: `Principal.Groups []string` — local group ids the principal is a direct member of
  beyond `userGroups[Subject]`. `principalEntity` adds them as parents; `GroupsOf` starts from them
  too. An empty `Subject` with `Groups`, `IdPGroups` or `Roles` set is "a member of that and
  nothing else".
- Produces: `ExplainedPolicy.Grant *Grant` (set for `perm:` policies, looked up from
  `a.generated`) and `ExplainedPolicy.Role string` (for base policies: the role in
  `principal in Role::"<name>"`, extracted with a regexp from the policy text; empty otherwise).

- [ ] **Step 1: Write the failing tests**: `TestSyntheticGroupPrincipal` — group `a` is a member of
  group `b`; a grant of `read` on `Template t1` to `b` makes
  `AllowFor(Principal{Groups: []string{"a"}}, "read", t1)` true, and `GroupsOf` returns `a` and `b`.
  A second case builds entities where `a` and `b` are each other's parent and asserts `GroupsOf`
  returns. `TestExplainReasons` — an editor reading a template yields a policy with
  `Role == "editor"`; a direct grant yields `Grant.PrincipalID == "alice"`.
- [ ] **Step 2: Run** `go test ./internal/authz/` — FAIL.
- [ ] **Step 3: Implement** the two fields.
- [ ] **Step 4: Run** `go test ./internal/authz/` — PASS.
- [ ] **Step 5: Commit** `feat(authz): ask about a group, provider group or role as a member`.

### Task 6: `whatMay` — what a principal may do, from the authorizer

**Files:**
- Create: `internal/httpserver/what_may.go`, `internal/httpserver/what_may_test.go`
- Modify: `internal/httpserver/views/access.templ` (new view types)

**Interfaces:**
- Consumes: `Principal.Groups`, `ExplainedPolicy.Grant/Role` (Task 5); `recordIndex`, `labelOf`,
  `principalLabels`, `recordActions`.
- Produces:
  `func (s *Server) whatMay(r *http.Request, snapshot *authz.Authorizer, p authz.Principal, self models.PrincipalType, selfID string) (views.WhatMay, error)` and
  `type WhatMay struct { Records []MayRow; Roles []RoleSummary; Webhooks string; Messages string }`,
  `type MayRow struct { Type, ID, Name, Href string; Actions []string; Via []string; Direct *models.Permission; CanShare bool }`.
  For every record in `recordIndex` and every action in `recordActions`, `AllowFor`; for allowed
  ones, `Explain` and turn each policy into a reason: grant naming `self`/`selfID` → `access.via_direct` (new key: "direct" / "direkt");
  grant to a group → "group " + label; provider group → its name; role grant or base role → "role "
  + name. A record whose every reason is a base role policy is left out (the role shows in
  `Roles`, computed as in `grantedTo`). `Direct` is the grant row naming `self:selfID` on that
  record, if any; `CanShare` is `s.can(r, share, type, id)`. `Webhooks` uses `levelOf` semantics
  over `AllowFor` on the webhook resources; `Messages` is `snapshot.MessageLevel(p)`.

- [ ] **Step 1: Write the failing test** `TestWhatMay` table: group `g-ops` granted `read` on `t1`
  directly and parent group `g-all` granted `attach` on `t2` → rows `t1 [read] [direct]`,
  `t2 [attach] [group All]`; a role principal `editor` → no record rows, `Roles` lists `Template`
  with `read update delete attach`; message level `everyone` granted to `g-all` → `Messages ==
  "everyone"` for `g-ops`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** — PASS.
- [ ] **Step 5: Commit** `feat(access): work out what a principal may do from the authorizer`.

### Task 7: Group page in three sections, hub preselect

**Files:**
- Modify: `internal/httpserver/views/groups.templ`, `internal/httpserver/groups_page.go`
- Modify: `internal/httpserver/sharing_hub.go` (`hubFor`), `internal/httpserver/views/hub.templ`
- Modify: `internal/i18n/locales/en.json`, `de.json`
- Create: ADR `NNNN-answer-access-questions-with-the-authorizer.md` (synthetic principals,
  reasons, enumeration cost); modify `docs/architecture.md`, `CHANGELOG.md`, `README.md`
- Test: `internal/httpserver/groups_page_test.go`, `internal/httpserver/sharing_hub_test.go`

**Interfaces:**
- Consumes: `whatMay` with `authz.Principal{Groups: []string{id}}`, `self = group`, `selfID = id`;
  `ActionRef`; `levelRemove` and the level endpoints for the group's own levels.
- `GroupDetail` gains `May views.WhatMay` and `Admin bool`. Sections in order: Members (picker,
  remove buttons, no raw id beside a resolved name), What this group may do (rows: kind, linked
  name, `ActionRef`, via; `Direct != nil && CanShare` → `<details>` edit posting to
  `/admin/sharing/grant` with `principal=group:<id>` and `return` = this page; webhook and message
  level rows with `levelRemove` when `Admin`; link `/admin/access?principal=group:<id>` "Give this
  group access to more records…"), Who may manage this group (`sharingPanel`). The `Cedar:` line
  moves into `<details>` "Technical details".
- `hubFor` reads `principal` from the query; `Hub` gains `Principal string`, passed as the hub
  picker's `Selected`.
- New keys: `groups.may` "What this group may do" / "Was diese Gruppe darf"; `groups.give_more`
  "Give this group access to more records…" / "Dieser Gruppe Zugriff auf weitere Einträge geben…";
  `groups.technical` "Technical details" / "Technische Details"; `groups.via_parent` "via group
  {0}" / "über Gruppe {0}".

- [ ] **Step 1: Write the failing tests**: `TestGroupPageSections` — the page for `g-ops` shows the
  three headings in order, a direct grant row with `<details`, an inherited row without, the
  message level row; `/admin/access?principal=group:g-ops` renders the hub picker with
  `value="group:g-ops" selected`.
- [ ] **Step 2: Run** — FAIL. **Step 3: Implement.** **Step 4: Run** `make generate && make lint &&
  make coverage && make test-js` — PASS.
- [ ] **Step 5: Write** ADR, architecture, changelog, README.
- [ ] **Step 6: Commit** `feat(ui): separate a group's members, rights and managers`; open PR 2
  stacked on PR 1; docs PR on `pages`, linked both ways.

## PR 3 — Access check

### Task 8: "What may … do?"

**Files:**
- Modify: `internal/httpserver/views/access.templ`, `internal/httpserver/access_page.go`
- Modify: `internal/i18n/locales/en.json`, `de.json`
- Test: `internal/httpserver/access_page_test.go`

**Interfaces:**
- Consumes: `whatMay` (Task 6), `principalPicker`.
- The check section becomes a tab bar (`data-admin-tabs`, `data-tab="who"` / `"what"`,
  panels `data-tab-panel`); the server opens the tab named by `?tab=`, default `who`.
- `GET /admin/access?tab=what&principal=kind:id` → `Access.May *views.WhatMay`,
  `Access.MayPrincipal string`. The principal for a `user` is built as in `checkAccess` (stored
  roles and provider groups; unknown → `access.unknown_user`); `group` → `Principal{Groups}`;
  `idp_group` → `Principal{IdPGroups}`; `role` → `Principal{Roles}`.
- Rendering reuses the "What this group may do" rows from Task 7 read-only (extract a
  `templ mayList(m WhatMay)` component in Task 7 so both use it).
- Keys: `access.check_who` "Who can …?" / "Wer darf …?"; `access.check_what` "What may … do?" /
  "Was darf …?"; `access.show` "Show" / "Anzeigen".

- [ ] **Step 1: Write the failing test** `TestWhatMayCheck` table over the four kinds — group
  `g-ops` with message level `everyone` shows "broadcast to everyone"; role `viewer` shows its
  collection rights; an unknown user shows the unknown-user note.
- [ ] **Step 2–4:** Run (FAIL), implement, run (PASS).
- [ ] **Step 5: Commit** `feat(access): show what any principal may do`.

### Task 9: "Who can …?"

**Files:**
- Create: `internal/httpserver/who_can.go`, `internal/httpserver/who_can_test.go`
- Create: `internal/httpserver/web/access-check.js` (add to `layout.templ`)
- Modify: `internal/httpserver/views/access.templ`, `internal/httpserver/access_page.go`
- Modify: `internal/i18n/locales/en.json`, `de.json`

**Interfaces:**
- Produces: `var resourceActions = map[string][]string{"Webhook": {use, administer}, "People":
  {message, broadcast}}`, records default to `recordActions`; replaces `checkActions`.
- Produces: `func (s *Server) whoCan(r *http.Request, snapshot *authz.Authorizer, action string, res authz.Resource, narrow string) (views.WhoCan, error)`;
  `type WhoCan struct { Allowed []WhoRow; Truncated bool; WrongAction bool }`,
  `type WhoRow struct { Kind, ID, Label string; Via []string; Policies []authz.ExplainedPolicy }`.
  Users come from `ListUsers(ctx, "", checkUsersLimit)` with `const checkUsersLimit = 5000`,
  disabled users left out, `Truncated` when the limit is reached. `narrow` (`kind:id` or empty):
  user → that user; group → users whose `GroupsOf` contains it; provider group → users whose stored
  `IdPGroups` contain it; role → users holding it. An action not in the resource's list →
  `WrongAction`, nobody asked. Reasons as in Task 6; each row's `Policies` render under a collapsed "Policies".
- Form: action select options carry nothing; resource options carry
  `data-actions="read update …"`; `access-check.js` hides action options not in the selected
  resource's list (DOM only, no node test). GET params `tab=who&action=&resource=&principal=`.
  The old `subject` param maps to `principal=user:<subject>` so old links keep working.
- Keys: `access.allowed_count` "Allowed ({0})" / "Erlaubt ({0})"; `access.nobody` "Nobody
  signed in may." / "Niemand, der sich angemeldet hat, darf das."; `access.not_signed_in` "People
  who have not signed in yet are not listed." / "Wer sich noch nicht angemeldet hat, fehlt hier.";
  `access.truncated` "Only the first {0} users were asked." / "Nur die ersten {0} Benutzer wurden
  geprüft."; `access.wrong_action` "{0} is not an action on this resource." / "{0} ist keine Aktion
  auf dieser Ressource."; `access.narrow` "Narrow to" / "Eingrenzen auf".

- [ ] **Step 1: Write the failing test** `TestWhoCan` table: editor user and viewer user, group
  `g-ops` with member `s-bob` granted `update` on `t1` → `update t1` lists editor (via role editor)
  and bob (via group Ops); narrowed to `group:g-ops` lists only bob; `broadcast` on
  `Webhook:universal` → `WrongAction`; a fake store with `checkUsersLimit` users → `Truncated`
  (inject the limit as a `Server` field `checkUsers int` defaulting to the constant, so the test
  sets 2); disabled user never listed; `?subject=s-bob` still answers for bob.
- [ ] **Step 2–4:** Run (FAIL), implement, run (PASS).
- [ ] **Step 5: Commit** `feat(access): list who can do something and why`.

### Task 10: Finish PR 3

**Files:** `docs/architecture.md`, `CHANGELOG.md`, `README.md`, ADR from Task 7 (amend only if it is
not yet merged; otherwise a new ADR for the two-mode check).

- [ ] **Step 1: Run** `make generate && make lint && make coverage && make test-js` — PASS; check
  both tabs in the running app (skill `run`), with and without JavaScript.
- [ ] **Step 2: Commit** `docs: the access check's two questions`; open PR 3 stacked on PR 2; docs
  PR on `pages`, linked both ways.
