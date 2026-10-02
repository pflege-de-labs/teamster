# Roadmap

Planned work that is not implemented yet, in the order we intend to build it. Each feature is
designed before it is implemented: a short design note in its pull request, an [ADR](adr/) when it
changes how components are structured, and one feature per pull request.

This file records intent, not commitments. When a milestone ships it is removed from here and
recorded in the [changelog](../CHANGELOG.md), and its behaviour is documented in the
[README](../README.md). Milestones keep their numbers, because ADRs cite them. The design notes
of milestones 1–19 and 20.1, which ADRs also refer to, are in this file as of commit `241cd4c`.

Every milestone works within the [constraints in the architecture](architecture.md#constraints).

## Milestone 20 — Talking to the bot

Commands in the personal chat shipped in 0.8.0
([ADR 0048](adr/0048-bot-answers-commands-in-the-personal-chat.md)). Commands in team channels
followed ([ADR 0069](adr/0069-the-bot-answers-commands-in-team-channels.md)).

### 20.3 `route key=value,…`

Creates a route from the chat to the chat it was sent in, a channel or the sender's own personal
chat. The selector is parsed into the same `map[string]string` that `parseSelector` produces. Commands
are named without a slash ([ADR 0068](adr/0068-commands-are-words-addressed-to-the-bot.md)), so the
parser has to accept `route` followed by arguments. Today a bare word counts only as the whole
message.

* **Team owners** create the route directly. Ownership is read from the Graph team members
  (`roles` contains `owner`). This needs `TeamMember.Read.All`, and a destination for the channel,
  created on demand.
* **Everyone else** gets a proposal card. Its button links to `/admin/routing` with the selector and
  target filled in. An editor approves it through the normal `formPost` and Cedar path.

### 20.4 More commands

* `alerts`: the alerts currently active in this chat or channel. Needs a per-recipient query on
  `active_alert_recipients`.
* `routes`: the routes that reach here, inherited ones included.
* `whoami`: the AAD object id, tenant and conversation id, for support.
* `mute <duration> [selector]`: needs a table of its own.
* `ack`: needs Alertmanager silences.

## Milestone 21 — The Teams app package from teamster

A `teamster manifest` subcommand and a download on `/admin/teams` build the app zip from one shared
builder.

* Inputs: `bot.client-id`, `bot.app-id` (added for milestone 23), the developer fields, and a
  version derived from the release. With `bot.global-install` on, `unlink` is left out of the
  command list.
* The app ID and bot ID then cannot drift apart from the running configuration, and every release
  is newer than the installed app.
* The download is admin-only through Cedar.
* Needs an ADR, because configuration replaces the hand-edited `manifest/manifest.json`.

## Milestone 22 — Personal routes for others, by grant

Grant a user or a group the right to route to someone else's chat. This is a permission row
`deliverToRecipient` on `User::"<subject>"`
([ADR 0075](adr/0075-own-and-share-records-through-generated-policies.md)). What is left is offering
it on the notifications page, and letting `User` be a permissioned type.

## Milestone 23 — Messages to individual people

Installing the app for everyone, messages addressed to the people they name, and binding a user's
own chat from their sign-in are built
([ADR 0059](adr/0059-install-the-teams-app-for-every-member.md) to
[ADR 0065](adr/0065-your-own-chat-is-found-from-your-sign-in.md)); see the
[changelog](../CHANGELOG.md). What is left:

### 23.4 Idempotent one-shot fan-out

A one-shot event (no `state`) retried after a 502 reaches everyone again. An explicit `key` could
serve as an idempotency key for a short window. Only if senders need it.

### 23.5 Verify against a real tenant

Whether the Bot Connector accepts an Entra object id as the member of a new personal conversation,
whether the public service URL works before any activity has arrived, whether `From.aadObjectId` on
a personal install event names the person when Graph did the install, and how long a first run on a
large tenant takes under Graph's throttling.

### 23.6 A counter for Graph throttling

`teamster.graph.throttled`, from a hook in the Graph client's retry, so a slow run can be told from
a stuck one.

## Milestone 24 — Announcements

Broadcasts to everyone the bot can reach are built: `"broadcast": true`, accepted with 202,
delivered by a leased background run with progress, and their own Cedar action
([ADR 0083](adr/0083-broadcasts-run-in-the-background.md)); see the [changelog](../CHANGELOG.md).
What is left:

### 24.1 Each person exactly once

A takeover repeats the chunk the previous owner was sending, so up to 25 people get a broadcast
twice. Two ways to close it, to be decided in an ADR:

* **Completion tracked in the database.** A row per person and broadcast, claimed before the send
  and completed after it, as `active_event_recipients` does for a tracked event
  ([ADR 0064](adr/0064-chat-claims-for-people-share-the-recipient-table.md)). A takeover skips
  completed rows and waits out claims in flight. It costs two writes per person.
* **NATS JetStream as the queue.** One message per person, published with a `Nats-Msg-Id` of
  broadcast and person, and consumed by a durable work-queue consumer with explicit acks shared by
  every replica. Redelivery replaces the lease and the cursor. It makes NATS a delivery dependency
  rather than an export ([ADR 0071](adr/0071-publish-audit-events-to-nats-jetstream.md)), and the
  chart can declare the stream and consumer
  ([ADR 0079](adr/0079-the-chart-declares-the-audit-stream-through-nack.md)).

Either one should also retry a failed send instead of only counting it.

### 24.2 The rest

* Cancelling a broadcast that is waiting or running.
* An Entra group as the audience instead of everyone.
* Pacing against the Bot Connector's limits, beyond `webhook.fanout-concurrency`.
* An audit event of who broadcast what, and to how many.

## Follow-ups from shipped work

Smaller items left open when a milestone shipped. Each is picked up on its own.

* **Preview from sampled labels.** Build the preview event from the labels in `event_samples`, so
  the preview shows what a template renders for the events a deployment really gets
  ([ADR 0041](adr/0041-editor-completion-from-sampled-labels.md)).
* **Retire `X-Teamster-Token`.** Remove the header in a breaking release, and decide whether
  `webhook.token` stays as the declarative bootstrap token
  ([ADR 0044](adr/0044-webhook-access-tokens.md)).
* **Bearer tokens on `/api`.** Scoped tokens could admit scripts to the admin API, with the
  same double check as the webhooks
  ([ADR 0077](adr/0077-scoped-tokens-answer-to-their-creator.md)). Basic auth with the local
  credentials is the only way in today.
* **Install the Teams app into a team from Teamster.** Graph allows it; a separate ADR would decide
  whether Teamster should ([ADR 0045](adr/0045-channel-delivery-through-the-bot.md)). Installing
  for people is milestone 23.
* **Verify bot delivery against a real tenant.** Whether an edit through the stored conversation id
  updates the channel post, whether an app upgrade re-sends install events, and whether the bot can
  post to private and shared channels.
* **Where Cedar policies live.** Grants are generated policies
  ([ADR 0075](adr/0075-own-and-share-records-through-generated-policies.md)); the role policies
  are still embedded. An operator file of extra policies is still open
  ([ADR 0012](adr/0012-role-based-authorization.md)).
* **Activity-feed notifications.** Route C from milestone 13: `sendActivityNotification` with
  `TeamsActivity.Send`, a notification rather than a chat message, if a lighter option than the bot
  is wanted ([ADR 0026](adr/0026-alerts-in-a-persons-chat.md)).
* **Stop closing per-alert Alertmanager cards.** Remove `closeLegacyAlertCards` once no
  installation can still hold a card posted per alert before
  [ADR 0084](adr/0084-an-alertmanager-notification-is-one-event.md).
* **Visual card designer.** Not planned. [ADR 0014](adr/0014-card-editor.md) records why, and is
  where the case to supersede it would be written.

## Sequencing

| Order | Item | Depends on | Blocked by |
| --- | --- | --- | --- |
| 1 | 20.2 Commands in a team channel | — | — |
| 2 | 20.3 `/route` from a chat | 20.2 | `TeamMember.Read.All` consent |
| 3 | 21 App package from teamster | — | — |
| 4 | 22 Personal routes by grant | — | — |
| 5 | 20.4 More commands | 20.2 | — |
| 6 | 23.4–23.6 Messages to individual people, the rest | 23 | a test tenant for 23.5 |
| 7 | 24.1 Broadcasts exactly once | 24 | — |
| 8 | 24.2 Announcements, the rest | 24 | — |

Follow-ups are unordered and can be pulled in between milestones.

## Open questions

* Should a message that carries only text still be updated in place when an event closes, or is
  editing a plain message in Teams confusing in a way editing a card is not?
* Read replicas. Nothing routes a read anywhere in particular, and `target_session_attrs` is only
  reachable through the connection-URL escape hatch. Worth a first-class setting, or is the read
  load simply too small to care?
* The directory cache is per-process, so every replica warms its own and each pays Graph for it. A
  shared table, or leave it?
* `sweepSessions` runs hourly in every replica against the same tables. It is an idempotent set
  delete with no user-visible effect, so duplicating it is harmless and leader-electing it would
  add a lease, renewal and clock assumptions to protect a `DELETE`. Leave it, or is the waste worth
  removing?
* Nothing stops two routes claiming `is_default`; `selectRoots` takes whichever sorts first. A
  partial unique index would express it, but it is a new rule rather than a race, and a migration
  that fails on an installation which already has two needs its own thought. Destinations already
  have such an index, since the global default was introduced with it
  ([ADR 0038](adr/0038-global-default-destination.md)).
* pgbouncer in transaction-pooling mode: safe, or does the store hold session state? Prepared
  statements and advisory locks are session-scoped, so this needs an answer before it is documented
  as supported.
