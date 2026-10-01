# 0048. The bot answers commands in the personal chat

* Status: Accepted, amended by [0068](0068-commands-are-words-addressed-to-the-bot.md): the
  commands are named without a slash
* Date: 2026-09-29

## Context

In the personal chat the bot understands two things: a link code ([ADR 0026](0026-alerts-in-a-persons-chat.md))
and the unlink words `unlink`, `stop` and `unsubscribe` ([ADR 0032](0032-retiring-a-link-from-the-chat.md)).
Anything else gets the "that does not match a link code" reply.

The chat offers no way to answer three simple questions:

* Is this chat linked?
* Which routes deliver here?
* Does delivery work at all?

A bot whose outbound token was misconfigured stayed silent until someone read the server log. The
admin UI showed the code as redeemed, so everything looked fine.

## Decision

We will have the bot answer commands in the personal chat:

| Command | Answer |
| --- | --- |
| `/help` | The command list. |
| `/status` | Whether the chat is linked, to whom and since when, the last delivery failure, and the routes naming this chat with their selectors. |
| `/test` | A test alert, sent through `deliverToRecipientOnce` like any untracked routed alert. On failure, a reply with the request reference (ADR 0046). |
| `/unlink` | The existing unlink, unchanged. |

Parsing coexists with what came before:

* The bot's own mention is stripped first, as for link codes.
* A message whose first word starts with `/` is a command. Words after it are allowed. An unknown
  slash command answers with a pointer to `/help`, so a typo is not read as a wrong link code.
* A bare word is a command only when it is the whole message. `help me please` is not a command,
  for the same reason `how do I unlink this chat?` is not an unlink (ADR 0032). `stop` and
  `unsubscribe` remain synonyms of `unlink`.
* Everything else goes on to link-code detection, as before.

The manifest declares the commands in `bots[].commandLists` for the personal scope, so Teams offers
them as suggestions.

Commands stay in the personal chat. A team channel remains a place the bot posts to, not one it
takes commands from ([ADR 0045](0045-channel-delivery-through-the-bot.md)). Channel commands, and
`/route` for creating routes from a chat, are on the roadmap and need their own ADR.

Alternatives considered:

* **Adaptive Card replies.** They look better, but a plain Markdown reply is readable in every
  client, including notifications. It also needs no card schema kept in step with the text.
* **Commands only with a slash.** Mobile users type `help`, and Teams sometimes drops the slash from
  a picked suggestion. Accepting the whole-message bare word costs nothing.

## Consequences

* A person can check the link and the delivery path from the chat, without an admin.
* `/test` sends a real message and counts under `metrics.DeliveryRecorded` with the route label
  `bot /test`.
* If the Bot Connector rejects the bot's token, neither the test message nor the failure reply
  arrives. The failure is visible only in the log, under `bot test` and `bot reply`.
* The manifest changes, so installed apps need a new version to show the suggestions. Commands work
  without it.
* No schema change.
