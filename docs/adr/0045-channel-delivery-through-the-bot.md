# 0045. Deliver channel messages through the Bot Framework bot

* Status: Proposed
* Date: 2026-09-28

## Context

Channel delivery posts a card with Microsoft Graph, `POST /teams/{team}/channels/{channel}/messages`,
and edits it with `PATCH …/messages/{id}`, signed in as the application (client credentials). ADR
0026 and milestone 13 assumed that identity may post to a channel. It may not:

* Posting as an application is limited to `Teamwork.Migrate.All`, which is for importing history.
* Editing as an application is limited to `ChannelMessage.UpdatePolicyViolation.All`, which
  changes only the `policyViolation` field.

A real tenant answers every channel post with `403 Forbidden`. The Teams V2 endpoints (ADR 0030)
post the same way. Nothing caught this, because tests and local runs talk to a fake Graph.

The resource-specific consent permission `ChannelMessage.Send.Group` restores posting, but not
editing. Without editing, the card cannot be updated when an alert changes or resolves, and that
update is what `active_alerts` and the claim protocol (ADR 0021) exist for.

The bot from ADR 0026 is a Bot Framework bot on its own Entra registration. The Bot Connector lets
it do both halves as itself:

* It creates a post in a channel with `POST {serviceUrl}/v3/conversations`, passing `isGroup`,
  `channelData.channel.id`, the tenant and the activity. The answer holds a conversation id and an
  activity id.
* It edits its own message with `PUT {serviceUrl}/v3/conversations/{id}/activities/{activityId}`.
  `bot.Client` already makes this call for chats.

Two constraints come with it:

* **The Teams app must be installed in the team.** Microsoft refuses a proactive message to a team
  without it.
* **The service URL is regional** and only arrives with an activity from Teams. Microsoft
  documents `https://smba.trafficmanager.net/teams/` as the public-cloud fallback for proactive
  messages when none has been received.

When the bot is added to a team, it receives:

* an `installationUpdate` with `action: add`, or a `conversationUpdate` whose `membersAdded`
  includes the bot
* in both, `serviceUrl`, `channelData.tenant.id` and `channelData.team.aadGroupId`, which is the
  Graph team id that a Destination already stores

Removal arrives as `installationUpdate` with `action: remove`, or as `membersRemoved` or
`teamDeleted`.

Today `/bot/messages` drops every activity whose conversation is not `personal`. The manifest
declares only the `personal` scope, to keep anyone from linking alerts into a group chat.

## Decision

We will deliver channel messages through the bot, and stop posting or editing channel messages
through Graph.

**Posting and editing.**

* A channel delivery creates a conversation in the destination's channel with
  `POST {serviceUrl}/v3/conversations`. `channelData.channel.id` is the destination's channel id
  and the tenant is the bot's.
* `active_alerts` gains `conversation_id TEXT NOT NULL DEFAULT ''`. The activity id goes into
  `message_id`, as now.
* A re-fire or a resolve edits the card with `PUT …/conversations/{conversation_id}/activities/{message_id}`.
  Channels keep their semantics: a resolve edits the card, unlike a chat, which gets a new message
  (ADR 0026).
* The claim protocol is unchanged. Its lease is derived from `bot.timeout-sec` instead of
  `graph.timeout-sec`.

**Knowing the team.**

* A new table, `bot_teams`, records each team the bot is installed in: the Graph team id
  (`aadGroupId`), the tenant, the service URL, and when the row was written.
* Only activities that pass the existing inbound authentication write to it:
  * an install event (`installationUpdate` add, or the bot in `membersAdded`) inserts or updates
    the row
  * a removal event (`installationUpdate` remove, the bot in `membersRemoved`, or `teamDeleted`)
    deletes it
  * any other verified team-scope activity refreshes the service URL, as
    `refreshRecipientServiceURL` does for chats
* The inbound check already requires the token's `serviceurl` claim to match the activity, and a
  forged service URL would redirect the bot's credentials, so the service URL is taken from
  nowhere else.
* A delivery uses the team's stored service URL. Without a row it uses `bot.service-url`,
  defaulting to Microsoft's public fallback. Teams where the app was installed before this
  release sent their install event when nothing recorded it, and the fallback covers them.
* If the app is not installed in the team, the Connector refuses the post. The delivery then fails
  with an error that names the team and says to install the app, instead of a bare `403`.

**Inbound activities.**

* `/bot/messages` accepts `channel` conversations for `conversationUpdate` and `installationUpdate`
  only, and acts only on the team events above.
* A message in a channel, such as an @mention, is answered `200` and ignored, so the link commands
  stay personal-only.
* `groupChat` stays refused.

**Manifest.** `bots[0].scopes` becomes `["personal", "team"]`. `groupChat` stays out, for the
reason the manifest README gives today.

**Messages.**

* `bot.Message` carries several cards, because a Teams V2 message appends its hint card (ADR 0040).
* A channel card's title becomes the bold first line of the text, as it already is in a chat.
  The Bot Connector has no equivalent of Graph's `subject`.

**Graph** stays for reading only: the Team and channel pickers, and naming Teams and channels in
the routing graph, export and import. `graph.PostMessage` and `graph.UpdateMessage` leave the
`messenger` interface. Channel delivery requires the bot to be configured. Without it, startup
logs that channel deliveries will fail, and each one fails with that reason.

**Teams V2 endpoints** post through the bot as well, fire-and-forget as before.

Alternatives we rejected:

* **RSC `ChannelMessage.Send.Group` through Graph.** Smallest change, but a card could never be
  updated, which gives up update and resolve.
* **`Teamwork.Migrate.All`.** It is for importing history into a channel in migration mode, not
  for live messages.
* **A delegated service account posting with `ChannelMessage.Send`.** Messages would appear to come
  from a person, the account's refresh token becomes a credential to keep alive, and the account
  needs a license.
* **Keeping Graph posting as a fallback when the bot is not configured.** It answers `403` wherever
  Microsoft runs Teams, so the fallback only hides the misconfiguration.
* **A separate bot for channels.** It is one more registration and secret for no gain. The bot
  that sends to chats may post to channels.

## Consequences

* Channel delivery works against a real tenant, and a card is edited when its alert changes.
* Every team a route posts to needs the Teams app installed. The app has to be in the tenant's
  app catalog, or uploaded to each team where custom app upload is allowed. A team owner can
  install it, or an admin through an app setup policy. Installing it through Graph is possible but
  out of scope here.
* The bot becomes required for any channel delivery, not an option for chats. Its registration and
  secret now carry all delivery. The Graph registration keeps only its read permissions
  (`Team.ReadBasic.All`, `Channel.ReadBasic.All`).
* Channel cards come from the bot's name and icon, from the manifest.
* The schema changes are additive: one new table and one column with a default. The previous
  release ignores both.
* Existing `active_alerts` rows have an empty `conversation_id`. Against a real tenant there are
  none, because no post ever succeeded; against a fake Graph there may be. Such a row is treated
  as a card that cannot be edited: a re-fire posts a new card and replaces the row, and a resolve
  forgets it.
* Local runs and tests need a fake Bot Connector instead of a fake Graph for delivery. The
  service URL is per team, so a test points `bot.service-url` at its own server.
* The Connector throttles per conversation. The `429` and `Retry-After` it returns are already
  parsed by `bot.APIError`, so a throttled delivery fails with `502` and the sender retries.
* Follow-up: mark a destination whose team has no `bot_teams` row in the admin UI, so a missing
  install shows up before an alert does.
* To confirm while implementing: that updating against the conversation id returned by
  `POST /v3/conversations` edits the channel post, and whether an app upgrade re-sends install
  events for teams that installed it earlier.
