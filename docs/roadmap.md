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
([ADR 0048](adr/0048-bot-answers-commands-in-the-personal-chat.md)).

### 20.2 Commands in a team channel

Accept `message` activities in team channels. Teams delivers these only when the bot is
@mentioned. This supersedes the "never takes commands" part of
[ADR 0045](adr/0045-channel-delivery-through-the-bot.md), so it needs its own ADR. `/help` and
`/status` in a channel answer for that channel: its destinations, the routes into it, and whether
the team is recorded in `bot_teams`.

### 20.3 `/route key=value,…`

Creates a route from the chat to the chat it was sent in, a channel or the sender's own personal
chat. The selector is parsed into the same `map[string]string` that `parseSelector` produces.

* **Team owners** create the route directly. Ownership is read from the Graph team members
  (`roles` contains `owner`). This needs `TeamMember.Read.All`, and a destination for the channel,
  created on demand.
* **Everyone else** gets a proposal card. Its button links to `/admin/routing` with the selector and
  target filled in. An editor approves it through the normal `formPost` and Cedar path.

### 20.4 More commands

* `/alerts`: the alerts currently active in this chat or channel. Needs a per-recipient query on
  `active_alert_recipients`.
* `/routes`: the routes that reach here, inherited ones included.
* `/whoami`: the AAD object id, tenant and conversation id, for support.
* `/mute <duration> [selector]`: needs a table of its own.
* `/ack`: needs Alertmanager silences.

## Milestone 21 — The Teams app package from teamster

A `teamster manifest` subcommand and a download on `/admin/teams` build the app zip from one shared
builder.

* Inputs: `bot.client-id`, a new `bot.app-id`, the developer fields, and a version derived from the
  release.
* The app ID and bot ID then cannot drift apart from the running configuration, and every release
  is newer than the installed app.
* The download is admin-only through Cedar.
* Needs an ADR, because configuration replaces the hand-edited `manifest/manifest.json`.

## Milestone 22 — Personal routes for others, by grant

Grant a user or a group the right to route to someone else's chat. This extends the Cedar
`deliverToRecipient` policy from ADR 0047 rather than adding Go checks.

## Follow-ups from shipped work

Smaller items left open when a milestone shipped. Each is picked up on its own.

* **Preview from sampled labels.** Build the preview alert from the labels in `alert_samples`, so
  the preview shows what a template renders for the alerts a deployment really gets
  ([ADR 0041](adr/0041-editor-completion-from-sampled-labels.md)).
* **Retire `X-Teamster-Token`.** Remove the header in a breaking release, and decide whether
  `webhook.token` stays as the declarative bootstrap token
  ([ADR 0044](adr/0044-webhook-access-tokens.md)).
* **Scoped webhook tokens.** Limit an access token to one webhook, or to a role's delivery grants.
  Additive.
* **Install the Teams app from Teamster.** Graph allows installing the app into a team; a separate
  ADR would decide whether Teamster should
  ([ADR 0045](adr/0045-channel-delivery-through-the-bot.md)).
* **Verify bot delivery against a real tenant.** Whether an edit through the stored conversation id
  updates the channel post, whether an app upgrade re-sends install events, and whether the bot can
  post to private and shared channels.
* **Where Cedar policies live.** Policies are embedded defaults derived from the three roles. An
  operator file or policies edited in the admin UI are still open
  ([ADR 0012](adr/0012-role-based-authorization.md)).
* **Activity-feed notifications.** Route C from milestone 13: `sendActivityNotification` with
  `TeamsActivity.Send`, a notification rather than a chat message, if a lighter option than the bot
  is wanted ([ADR 0026](adr/0026-alerts-in-a-persons-chat.md)).
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

Follow-ups are unordered and can be pulled in between milestones.

## Open questions

* Should a message that carries only text still be updated in place when an alert resolves, or is
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
