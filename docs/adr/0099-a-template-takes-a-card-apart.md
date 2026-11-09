# 0099. Let a template take a sender's card apart

* Status: Accepted
* Date: 2026-11-09

## Context

A Teams V2 sender posts its own Adaptive Card, and teamster passes it through
([ADR 0030](0030-teams-v2-compatible-webhooks.md)). Some of those cards render
badly in Teams: a FactSet whose values are long squeezes its title column to one letter a line.
Operators want to keep the content and change the layout — another card shape, or a text message
with the facts in a code block — per endpoint or as the Teams V2 default.

Templates already see the card (`.Event.Card`) and the raw body (`.Payload`)
([ADR 0040](0040-teams-v2-endpoint-templates.md)), and the endpoint template and the
per-webhook default ([ADR 0055](0055-each-webhook-has-a-default-template.md)) already choose where
a template applies. What is missing is a practical way to read a card: its elements nest in
containers, columns and tables, which text/template cannot walk without recursion.

## Decision

We will add a template function, `cardParts`, that takes an Adaptive Card and returns its content
as flat lists: `Texts` (TextBlocks and RichTextBlocks, in document order), `Facts` (`Title`,
`Value`) and `Actions` (`Title`, `URL` of every `Action.OpenUrl`). It descends into `items`,
`columns`, `rows` and `cells`, and returns empty lists for a message without a card. A MessageCard
is converted to an Adaptive Card before templates see it, so one function covers both.

Two presets use it — the card as text with the facts in a code block, and a card with every fact
stacked as a heading over its value. They are offered in the editor, not seeded as defaults; the
Teams V2 default still passes the card through. The preview gains a Teams V2 Adaptive Card sample
with long facts.

We considered a separate, stored extraction step on a route — a transform producing JSON that a
second template renders. It would need schema changes in both dialects, a place in the transfer
bundle and its own editor, and its output would be the same data a function hands the template
directly.

## Consequences

* A badly rendered card is fixed by choosing a template, in the editor and preview operators
  already use. No schema change.
* Texts and facts lose their interleaving. A sender whose layout depends on that order needs an
  ordered list of blocks, which can be added to `CardParts` later.
* Images, inputs and actions other than `Action.OpenUrl` are not extracted.
