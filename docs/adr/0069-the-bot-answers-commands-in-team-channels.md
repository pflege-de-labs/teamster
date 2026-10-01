# 0069. The bot answers commands in team channels

* Status: Accepted
* Date: 2026-10-01

## Context

[ADR 0045](0045-channel-delivery-through-the-bot.md) installs the bot in teams so that it can post
to channels. It also refused every channel `message`, so commands stayed in the personal chat
([ADR 0048](0048-bot-answers-commands-in-the-personal-chat.md)).

The people who own a channel's alerts had no way to check the channel from the channel itself:
which routes post there, and whether a post would arrive at all. Answering that took the admin UI
or the server log.

Teams sends a channel message to a bot only when the message @mentions it, so a message that
reaches the bot was meant for it.

## Decision

We will accept `message` activities in `channel` conversations and answer commands there:

| Command | Answer |
| --- | --- |
| `help` | The channel commands. |
| `status` | The destinations naming this channel (team `aadGroupId` and `channelData.channel.id`), whether one is the global default, and the routes naming one of them directly. |
| `test` | A test alert posted to the first of those destinations by name, through `deliverToChannelOnce`. On failure, a reply with the request reference ([ADR 0046](0046-structured-logging-and-error-boundary.md)). |
| `unlink` | A pointer to the personal chat. |

* Parsing is the personal chat's `parseBotCommand`, after the mention is stripped. A message that
  is not a command gets a pointer to `help`.
* A link code is never read in a channel. Linking a channel would broadcast one person's alerts to
  everyone in it, the reason `groupChat` stays out of the manifest.
* The reply goes into the thread of the mention.
* The activity still records the team first, so the service URL stays fresh.
* The manifest gets a second `commandLists` entry for the `team` scope.

Anyone who can post in the channel can run these commands. `status` shows them the names and
selectors of the routes posting there, which describe what they already see arrive. `test` posts
only into the channel it was asked in.

Alternatives considered:

* **Restrict commands to team owners.** It needs `TeamMember.Read.All` and a Graph lookup per
  message. That is worth it for `route` (roadmap 20.3), which changes configuration, but not for
  commands that only read or post to the channel itself.
* **Group chats too.** They are left out for the privacy reason above.

This supersedes the part of ADR 0045 that says a channel message is ignored.

## Consequences

* A channel's members can check and test its delivery without an admin.
* A channel `test` counts under `metrics.DeliveryRecorded` with the route label `bot /test`, like
  the personal one.
* `status` lists only routes that name a destination directly. Inherited targets wait for `routes`
  (roadmap 20.4).
* Installed apps need a new manifest `version` to show the channel command menu. The commands work
  without it.
* No schema change.
