# Permission management UI — design

* Date: 2026-11-18
* Status: Agreed in conversation; written for review
* Builds on: [ADR 0074](../../adr/0074-local-groups-and-provider-groups.md) (groups), [ADR 0075](../../adr/0075-own-and-share-records-through-generated-policies.md) (sharing),
  [ADR 0076](../../adr/0076-webhook-permissions-and-access-overviews.md) (access overview), [ADR 0082](../../adr/0082-naming-people-takes-permission.md) (message levels)

## Why

Managing access needs internal ids nobody has at hand, and the screens do not say what they show.

* "Who can?" takes the sign-in subject (the IdP `sub` UUID) in a free-text field. The group page
  shows `Group::"<uuid>"`, so that is what an admin pastes — and the check treats it as an unknown
  user and answers for no roles and no groups. Groups and roles cannot be checked at all.
* The action and resource lists combine freely (`broadcast` on a webhook), so "Refused" means
  nothing.
* The same free-text id appears in "Add member", the webhook levels and the message levels; the
  member suggestions are UUIDs.
* On a group page, "Who may do what" is the generic sharing panel: who may manage *the group
  record*. Readers take it for the group's members or for what the group may do. The panel stacks
  an in-place edit row (Save and Revoke one above the other) above an almost identical form for a
  new grant.

Real questions it must answer: "may a member of it-admins create broadcast tokens?", "what is
it-admins granted overall?", "who may change this template?".

## Goals

1. One **principal picker** wherever a principal is named, with a type badge (User, Group,
   Provider group, Role) and search; no UUIDs typed by hand.
2. A **compact grant list** on every record's sharing panel: one line per principal with an
   `ls -l`-style action reference, editing collapsed per row.
3. The **group page** separates three things: its members, what the group may do, who may manage
   the group.
4. The **access check** answers two questions: *Who can …?* and *What may … do?*

## Non-goals

* Changing the authorization model, Cedar policies, the grants table or any endpoint's semantics.
* Bulk edit of several principals on one record (the hub's batch over records stays as it is).
* Listing people who have never signed in in *Who can …?*.
* The JSON API and MCP tools.

## Anti-goals

* A picker that only works with JavaScript. The server-rendered `<select>` must keep working.
* A second implementation of the policy rules in Go to compute who holds what. Answers come from
  the authorizer snapshot.
* Any change that grants or revokes something the current forms would not.

## Approaches considered

**Picker.** (a) Plain `<select>` with optgroups — no script, smallest, but unusable with hundreds
of users. (b) **Searchable**: the server renders the same `<select>`; a script in `web/` turns it
into a filter box listing entries with type badges. (c) Today's `<input list=…>` — rejected: shows
ids, no badges, submits whatever was typed. **Chosen: (b).**

**Access check.** (A) "Can X do this?" for one picked principal. (B) "Who can do this?" listing
every allowed person with the reason. **Chosen: both**, as two modes — B for one record and action,
A widened to an overview ("What may … do?") because the need is "what is this group granted
overall", not a single yes/no.

**Who can, computation.** Ask the authorizer about every signed-in user and map each deciding
policy to its reason. Building the answer from the grants table was rejected: it would duplicate
role, nesting and provider-group logic and drift from the engine.

## Design

### 1. Principal picker

* One templ component, replacing `principalSelect` and every free-text principal field:
  sharing panel, hub, group "Add member", webhook levels, message levels, and both check modes.
* Server renders `<select name=… data-principal-picker>` with optgroups User, Group, Provider
  group, Role. Option values keep today's `type:id` form; option text is the display name (user:
  `displayName`, group: name, provider group and role: name).
* Each use passes which types it accepts (group members: user, group, provider group; no roles).
* "Someone not listed" stays, for a person or provider group that has not signed in: a type and a
  name field, collapsed, as today.
* `web/principal-picker.js` enhances every `select[data-principal-picker]`: a text box that
  filters by name, email and id; results grouped by type with a badge; keyboard (arrows, Enter,
  Escape); writes the choice back to the `<select>`, which stays the submitted value. Without the
  script the plain select works.
* Pure filtering and grouping logic is tested with `node:test` under
  `internal/httpserver/webtest/` (ADR 0098).
* Forms that take `principal_type` + `principal_id` today (webhook and message levels, group
  members) accept the picker's `type:id` value; the handlers parse it with the existing
  `principalFromForm`.

### 2. Compact grant list (every sharing panel)

```
Who may manage this group ──────────────────────────────────
 [User]  jens.hausherr@pflege.de   owner       ▸ edit
 [Role]  editor                    r w - - -   ▸ edit
 ▸ Give someone access
 r read · w change · d delete · a use in routes and webhooks · s share
```

* One row per principal: type badge, name, action reference.
* Reference: fixed positions `r w d a s` (read, update, delete, attach, share), `-` for not held.
  A principal holding `own` shows **owner** instead, since it covers everything. Full action names
  in a `title`. One legend line under the list.
* **▸ edit** is a `<details>` per row holding today's checkbox form with Save and Revoke side by
  side. Rows the viewer may not change show no edit control.
* **▸ Give someone access** is a collapsed form: picker + checkboxes + Save, with the existing
  "saving replaces" hint.
* Applies to every record page that renders `sharingPanel` (templates, destinations, webhooks,
  routes, groups) and the hub.
* Heading per resource kind reads "Who may manage this <kind>" rather than "Who may do what".

### 3. Group page: three sections

```
Members ─────────────────────────────────────
 [User]  bader.aladel@pflege.de          [Remove]
 [Who ▾ search…]                         [Add member]

What this group may do ──────────────────────
 Template  Alertmanager card     r w - a -   direct            ▸ edit
 Route     prod → #alerts        r - - - -   via group it-admins
 Webhooks  Universal             send to     direct
 Messages                        anyone      direct
 ▸ Give this group access to more records…  (→ hub, group preselected)

Who may manage this group ───────────────────
 (compact grant list, section 2)
```

* **Members** uses the picker; the member's id is no longer shown beside the name (it is in the
  picker's search and the option's `title`). The `Cedar: Group::"…"` line moves into a collapsed
  "Technical details".
* **What this group may do** lists every grant to this group and to every group containing it,
  with its reason (`direct` or `via group X`), plus its webhook and message levels. Rows are
  editable in place when the viewer may share that record, otherwise read-only with a link to the
  record. The link to the hub preselects the group as principal.
* **Who may manage this group** is the compact grant list on the group record.

### 4. Access check

One section, two modes switched by a tab (existing `tabs.js`), each a GET form so answers are
linkable.

**Who can …?** — action, resource, optional "Narrow to" picker.

```
Action [read ▾]   Resource [Templates › Alertmanager card ▾]   Narrow to [anyone ▾]  [Ask]
Allowed (2)
 [User] bader.aladel@…    via role editor
 [User] jens.hausherr@…   via group on-call (owner)
 People who have not signed in yet are not listed.
```

* The action list offers only actions valid for the chosen resource type (a small map per type,
  used by the template and, through a data attribute, by a script that filters the select when
  the resource changes; without the script an invalid pair is answered with "not an action on
  this resource").
* For each signed-in user (narrowed by the picker: a user, or the members of a group / provider
  group / role) the authorizer's `Explain` is asked; deciding policies map to reasons: role,
  group (through which group), provider group, direct grant, admin.
* The raw policy text stays available under a collapsed "Policies" per person.

**What may … do?** — one picker (any type).

* Lists every record the principal may act on, with the reference and the reason, plus webhook
  and message levels and what its roles allow on whole collections.
* For a user this is `/admin/me` computed for someone else (reuse `grantedTo`).
* For a group: grants to it and to every group containing it (an ancestor walk like
  `GroupsOf`, starting from the group). For a provider group: grants to it and to local groups it
  is a member of. For a role: its built-in collection rights and grants to the role.
* It answers the motivating question directly: *What may Group · it-admins do?* shows the message
  level `anyone, and broadcast to everyone` or not.

Both modes remain admin-only, as today.

**Settled after review** (designer's calls unless noted):

* *Users asked about* are the `users` table: roles and provider groups as of each user's last
  sign-in, disabled users left out. N users × `Explain` per request is fine at this scale; no
  paging. "Narrow to" a role means users holding it; a provider group, users whose stored
  provider groups contain it; a group, its members however deep.
* *Reasons:* `Explain` returns every satisfied policy (`diagnostic.Reasons`); show all of them.
* *What may … do?* is computed by the authorizer, not from the grants table: enumerate every
  record in `recordIndex` plus the webhook and People resources, and ask `AllowFor` per action,
  `Explain` only for what is allowed. A group, provider group or role is asked about as a
  synthetic principal that is a member of it and nothing else — a small `internal/authz`
  addition (a principal entity whose parents are the given group / provider group / role).
  Records × actions evaluations per request is acceptable.
* *Resource select* in "Who can …?" offers what it offers today: every record, the webhooks and
  People. Actions per type: records — read, update, delete, attach, share, own; Webhook — use,
  administer; People — message, broadcast.
* *Picker form values:* `principalFromForm` already reads `principal=type:id` and falls back to
  `other_type`/`other_id`; the webhook-level, message-level and group-member handlers switch to
  it.
* *Hub preselect:* PR 2 adds `?principal=type:id` to the hub, used by "Give this group access…".
* *Group page levels:* webhook and message levels on "What this group may do" are editable by
  admins through the existing `/admin/access/webhooks` and `/admin/access/messages` endpoints;
  inherited rows (via group X) are read-only.
* Each PR leaves every page working on its own.

## Delivery

Three pull requests, one feature each, each with ADR / docs / changelog per the definition of done:

1. **Principal picker** — component, script, node tests, every principal field switched.
2. **Compact grant list and group page** — sections 2 and 3.
3. **Access check** — section 4.

Each user-visible PR gets its `pages` docs PR (English and German).

The group page is restructured within today's `/admin/groups?id=` page; the milestone 26 groups
record page later moves the three sections as they are.

## Left to the builder

* Exact markup and Tailwind classes, following the existing views.
* The per-resource-type action map's location (likely next to `checkActions`).
* How deciding policy ids map to reasons (generated policies carry their grant; base policies
  name their role).
* Whether one ADR covers the picker and check, or one per PR.
