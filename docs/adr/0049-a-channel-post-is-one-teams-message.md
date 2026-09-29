# 0049. A channel post is one Teams message

* Status: Accepted
* Date: 2026-09-29

## Context

Channel delivery goes through the bot ([ADR 0045](0045-channel-delivery-through-the-bot.md)). It
posts to `POST {serviceUrl}/v3/conversations` with the message as the first activity. The transport
used to send the title and text as Markdown, plus every card as its own attachment.

When a channel conversation is created, the Bot Connector refuses any activity that Teams would
split into more than one message:

```text
400 BadSyntax: Activity resulted into multiple skype activities
```

Text plus a card is refused, and so are two cards. The route with no template hit this every time:
its built-in default message ([ADR 0039](0039-built-in-default-message.md)) is text plus the "no
template" hint card. Chats are not affected, because a message there goes into a conversation that
already exists.

## Decision

We will send a channel post, and its edits, as exactly one Teams message:

* **No card:** text only.
* **Any card:** one Adaptive Card and nothing beside it.
  * The title leads as a bold `TextBlock`, and the text follows as a Markdown `TextBlock`.
  * The first card's body comes next.
  * Each further card's body follows in a separated `Container`.
  * Every card's actions are kept.
  * The first card's version and other top-level properties are kept; a missing version defaults
    to `1.4`.
* **The "no template" hint:** it is a card only beside a card of the message's own. Next to plain
  text it becomes a line of text, as in a chat. The built-in default message therefore stays
  Markdown and does not move into a `TextBlock`, which renders code blocks poorly.

Alternatives considered:

* **Posting the rest as replies in the thread.** Only the first activity could be edited, so a
  resolve would leave the replies stale.
* **The `carousel` attachment layout.** It still needs the text out of the activity, and it hides
  every card but the first behind arrows.

## Consequences

* The route with no template posts to channels again.
* A template with both text and a card now shows the text inside the card in a channel. In a chat
  it stays above the card.
* No schema change.
