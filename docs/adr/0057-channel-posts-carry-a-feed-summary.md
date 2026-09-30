# 0057. Send the title as the activity summary

* Status: Accepted
* Date: 2026-09-30

## Context

A template's title exists so that the Teams activity feed shows something readable. Under Graph,
the title was the post's subject. Since channel delivery moved to the bot
([ADR 0045](0045-channel-delivery-through-the-bot.md)), any post with a card has been sent as one
card and nothing else ([ADR 0049](0049-a-channel-post-is-one-teams-message.md)). The title went
into the card, and the feed showed `Card` again.

The Connector refuses a new channel post that carries both text and a card, so the title cannot sit
beside the card in `text`. The Bot Framework activity has a `summary` field. Teams documents it as
the text a notification shows in the feed.

## Decision

We will set `summary` on every activity the bot sends, both channel posts and chat messages, and on
their edits.

* The summary is the title with its whitespace collapsed.
* A message without a title uses the first non-blank line of its sanitized text instead, as plain
  text.
* A summary is at most 150 characters; a longer one is cut and ends in `…`.
* In a channel post, the title still leads the card, so people reading the post see it as well.

Alternatives considered:

* **Take the title out of the card.** The feed would be fixed, but the post itself would lose its
  heading.
* **Title as `text` beside the card.** The Connector refuses this when it creates a conversation
  (ADR 0049).

## Consequences

* The feed and notifications show the title for a card-only post, rather than `Card`.
* Reports say Graph ignores `summary` on card messages. The Bot Connector path is a different API,
  but it has to be confirmed in a live tenant.
* No schema change. Cards already posted keep their old preview until they are edited.
