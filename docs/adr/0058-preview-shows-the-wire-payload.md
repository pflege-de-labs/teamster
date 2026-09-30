# 0058. The preview shows the payload delivery sends

* Status: Accepted
* Date: 2026-09-30

## Context

The template preview draws the title, text and card that a template renders. That is not what
Teams receives. Delivery reshapes the message on its way to the Bot Connector:

* A channel post is one card, with the title and text folded into it
  ([ADR 0049](0049-a-channel-post-is-one-teams-message.md)).
* The title becomes the activity's `summary` ([ADR 0057](0057-channel-posts-carry-a-feed-summary.md)).
* The text is converted from HTML to Markdown.
* A chat keeps the text beside the card.

An operator debugging a card has so far had no way to see the body that was actually sent.

## Decision

We will return, next to the rendered message, the request bodies for a channel post
(`POST /v3/conversations`) and a chat message (the bare activity). They are built by the functions
delivery calls: `channelMessage`, `botChannelMessage` and `chatMessage`, then
`bot.ChannelPostBody` and `bot.ActivityBody`. `PostToChannel`, `SendMessage` and `UpdateMessage`
send those same values, and a test checks that the bytes match. The tenant and channel IDs are
placeholders, because a template has no destination.

The preview gets three views: Rendered, Channel JSON and Chat JSON. Switching views reuses the last
answer rather than asking the server again.

Alternatives considered:

* **Serialise the payload in the browser.** It would copy the folding and Markdown rules into
  JavaScript, and the copy would drift from the Go code that actually delivers.
* **A separate endpoint.** It would cost a second request per keystroke to answer the same render.

## Consequences

* The preview shows a change to how delivery shapes a message as soon as that change is made.
* The `bot` package exports two body builders. They are read-only views of what its client already
  sends.
* No schema change. The preview response gains a `payloads` field; older clients ignore it.
