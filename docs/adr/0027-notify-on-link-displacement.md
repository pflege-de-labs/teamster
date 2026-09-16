# 0027. A displaced recipient conversation is notified, not left silent

* Status: Accepted
* Date: 2026-09-16

## Context

An adversarial review of the inbound bot endpoint ([ADR 0026](0026-alerts-in-a-persons-chat.md))
found that redeeming a link code for a subject who already has a recipient overwrites that
recipient's conversation reference in place: `UpdateRecipient` replaces the old chat with the new
one, and nothing tells the old chat its alerts just stopped. A link code that leaks — read over a
shoulder, pasted into the wrong window — lets whoever redeems it silently take over the victim's
alert stream. The victim finds out only when alerts stop arriving, if they notice at all.

The obvious fix, an unlink command, is out of scope here: it is a new piece of product surface (who
may run it, on whose behalf, through what UI) that this security pass is not the place to design.
What this pass can and must do is stop the takeover from being silent.

## Decision

We will notify the **previous** conversation, once a redemption that changes `ConversationID`
commits, via the same bot client a normal reply uses. The notice is best-effort — logged, not
retried — the same reasoning `replyText` already documents for a reply that races the inbound
activity's own 200. No unlink command is added; that remains parked for a future change.

Two related correctness fixes ride along, on the same code path: `channelData.tenant.id` is now
compared against `bot-tenant-id` for a single-tenant registration, and an activity that omits
`channelData` (Teams does, for some shapes) no longer blanks out a recipient's previously-known
tenant on re-link.

## Consequences

A displaced conversation learns it lost its alert stream at the moment it happens rather than
sometime later when nothing arrives, at the cost of one more outbound message and one more
`ConversationReference` carried alongside the winning `Recipient` through `redeemLinkCode`. A person
who wants to reverse an unwanted link-code redemption still needs an admin's help — minting a fresh
code re-links them exactly as it does today — until an unlink command exists.
