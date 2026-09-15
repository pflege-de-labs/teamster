# 0024. Alerts reach a person's chat through a Bot Framework bot

* Status: Proposed
* Date: 2026-09-15

## Context

An alert reaches a channel today. Somebody on call at three in the morning is not watching a
channel; they want the alert in front of them. The ask is an opt-in: a person says "send my alerts
to me," and they arrive as a chat message rather than only in a Teams channel they happen to watch.
[Milestone 13 of the roadmap](../roadmap.md) worked through why this is not a small addition to the
existing Graph client and laid out three routes. This ADR records the choice.

Teamster authenticates to Microsoft Graph with **client credentials** — an application identity, no
user attached. That is exactly what lets it post to a channel unattended, and exactly what
**cannot** send a `chatMessage`: that has always required a **delegated** permission, a token
obtained on behalf of a signed-in person.

**A. A delegated token per person.** A second authorization-code flow against Microsoft, each
person consenting once; Teamster keeps a refresh token and uses it when an alert fires. The message
is then sent **as that person** — not a message from Teamster to Alice, but a message Alice appears
to have written to herself. Workable as a notification, strange as a conversation. It also changes
what the database is: today it holds no credentials at all, which is why a configuration bundle can
be kept in a repository ([ADR 0013](0013-configuration-transfer.md)). A refresh token is a
credential — it needs encrypting at rest, must never reach an export, and losing the database
becomes an incident rather than an inconvenience. Consent is revocable and conditional access can
invalidate a token at any time, so delivery would have to degrade when a token stops working rather
than dropping the alert.

**B. A Teams app with a bot, Bot Framework, sending as itself.** The message comes from a bot, which
is what a person expects an alert to look like. The bot must be installed for each recipient, and
Teamster stores a conversation reference per person rather than a per-person credential. This is the
largest of the three: a Teams app package, a bot registration, an inbound endpoint Microsoft can
call, and an install story.

**C. An Activity Feed notification.** `POST /users/{id}/teamwork/sendActivityNotification` with the
`TeamsActivity.Send` application permission — the credential model Teamster already has. It puts an
entry in the person's Activity feed linking somewhere, rather than a message in a chat. Cheapest by
a distance, and less than what was asked for: a notification, not a message.

Two independent codebase explorations confirmed the same structural fact the roadmap already
states: `routing.Delivery` and `models.Route` resolve to exactly one `DestinationID` — a team and a
channel — and a person is neither.

## Decision

We will build route B: a real Teams bot, registered separately, sending as itself.

Route A is rejected because a message a person appears to have sent to themselves is not the thing
that was asked for, and because it turns the database into something holding refresh tokens, a
different risk profile than it has today. Route C is rejected because it is a notification, not a
message — explicitly less than the ask. B is the only one of the three that produces an actual chat
message from a bot, which is what "send my alerts to me" means to the person asking for it.

**A second Entra app registration**, separate from the existing Graph app. A single registration
could hold both application and delegated-adjacent bot permissions, but two is the better answer:
the channel-posting identity keeps its narrow permissions and its existing secret untouched, and
the chat identity is consented to separately, revoked separately, and absent entirely from a
deployment that does not want the feature.

**Name and icon are configurable through a Teams app manifest** (`manifest.json` plus two PNG
icons), not through `config.example.yaml`. This is packaging metadata for the Teams app catalog —
what a person sees when they install the bot — not runtime configuration Teamster reads at
startup.

**Proving "this Teams account is mine" is a one-time code.** An authenticated admin-UI session
generates a code; the person types it to the bot in a Teams chat. This binds the resulting
recipient to whatever Grant or role that admin-UI session already has, so a recipient only ever
gets alerts they were already permitted to see through the session that created it. The rejected
alternative is binding straight off `conversationUpdate`'s AAD object id when the bot is added:
nothing in that event ties the resulting recipient to any existing Grant, so it would let a person
who can install a Teams app — a much lower bar than an authenticated admin-UI session — opt into
alerts nobody granted them. The code-based flow reuses `models.LoginFlow`'s exact shape: single
use, deleted on read, expiring.

**The routing model gains a second delivery target alongside the existing one, not a replacement
for it.** `Route` gains a nullable `RecipientID` beside `DestinationID`. `routing.Delivery` gains a
`Kind` discriminator (`channel` / `recipient`). A route already fans out to more than one delivery
today — nested routes ([ADR 0011](0011-nested-routes.md)) — so a route naming both a destination
and a recipient is the same fan-out, not a new concept: `collect()` emits up to two `Delivery`
entries per route, both suppressed together by a greedy child, exactly as sibling deliveries are
suppressed today.

**Active-recipient state lives in a parallel table, `active_alert_recipients`, keyed
`(fingerprint, recipient_id)` — not a widened key on `active_alerts`.** `active_alerts.team_id` and
`.channel_id` are `NOT NULL` under an existing `CHECK` from the claim protocol
([ADR 0021](0021-claim-a-card-before-posting.md)), and a person has neither a team nor a channel.
A same-shaped sibling table lets the claim/complete/release/touch state machine be reused verbatim,
rather than risking the table and code that already works by widening its key and its `CHECK` to
admit a case it was never built for.

**Inbound JWT validation is not decided here.** The PR that adds the inbound `/bot/messages`
endpoint opens with a timeboxed spike into a maintained Bot Framework Go SDK. If that spike finds
nothing suitable within its timebox, the fallback — recorded here so it is not lost — is
hand-rolled validation on `coreos/go-oidc`'s lower-level `NewRemoteKeySet`, already a dependency and
the same pattern `internal/httpserver/oidc.go` already uses for the admin login flow. Either way,
this is signature validation on an endpoint reachable from the internet before any authorization
runs, which is why it is validated by a spike rather than assumed in advance.

**A recipient is excluded from the export bundle** ([`internal/transfer/bundle.go`](../../internal/transfer/bundle.go)),
the same bucket as sessions, login flows and active alerts. It is a binding to one specific
admin-UI session in one specific deployment's identity provider — runtime state, not something an
operator configured. An imported route naming a `RecipientID` the target deployment does not have
is rejected outright, not silently imported inert: an import that leaned on state that happened to
already be in the target database would behave differently depending on what that database already
held, which is the same reasoning the bundle format already applies to every other field it
carries.

**The self-service opt-in UI ships in this milestone**, not deferred to a later one. A bare API
endpoint that only an operator can drive on somebody's behalf is not an opt-in; the roadmap's own
framing is "in the admin UI," under the person's own account.

**Any admin or editor with route-edit permission may target any existing recipient with a route.**
The self-service boundary this milestone adds is "who may create a recipient for themselves" (the
linking flow above) — it is not "who may point a route at one," which mirrors how creating a
`Destination` and linking a route to it are already two separately-authorized steps today.

## Consequences

A person can prove, from an authenticated admin-UI session, that they control a Teams account; that
binds a Bot Framework conversation reference to them; a route can then target them directly, and
delivery reuses the same render pipeline and the same claim-before-post atomicity that channel
delivery already has.

This is a second service-facing integration, not an addition to the Graph client: a second Entra
app registration, a second token flow, a second outbound client (`internal/bot`), and a new inbound
HTTP surface authenticated by someone else's signature rather than teamster's own shared-token or
OIDC flow. A deployment that does not configure the bot sees none of this — `config.Validate` gates
the whole feature on all three of tenant ID, client ID and client secret being set together or
all being empty, mirroring how metrics are gated off today.

The schema grows two tables (`recipients`, `link_flows`) plus one parallel claim-protocol table
(`active_alert_recipients`), all additive migrations. No existing table's key or `CHECK` changes,
so the schema-compatibility rule this repository already follows — migrations run before the new
pods do, so within a release only additive changes are safe — is not tested by this milestone in a
new way.

Two things are deliberately left open rather than guessed at, and are flagged here rather than
resolved:

* **What happens when a recipient's conversation reference goes stale** — the person uninstalled
  the bot, or Bot Framework returns an error indicating the conversation no longer exists. Candidate
  answers include silent failure (matching how a broken `Destination` already just fails at render
  time today) or some hybrid fallback to an Activity Feed notification (route C, repurposed as a
  degradation path rather than the primary mechanism). Not decided now; the PR implementing delivery
  to a recipient must pick one and either update this ADR's successor or add a follow-up ADR if the
  choice changes how a component is structured.
* **Whether a plain-text chat message updates in place the way a channel card does**, or whether a
  resolved alert instead sends a new message. The roadmap flags the same unresolved question for
  chat delivery generally; it is inherited here rather than answered.

The ADR number for this document is not stable: the next free number on `main` at the time of
writing is also claimed by `feat/shell-completion`, an open PR for an unrelated decision. Whichever
of the two merges second is responsible for renumbering its own file and its entry in the index;
nothing outside this file and [the index](README.md) should hardcode the number `0024` as a
cross-reference.
