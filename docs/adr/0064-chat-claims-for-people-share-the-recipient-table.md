# 0064. Chat claims for addressed people share the recipient claim table

* Status: Accepted
* Date: 2026-09-30

## Context

A tracked event (`state` `open`) sent to a person has to be edited on a repeat and closed later,
exactly like a message to a linked recipient. `active_event_recipients` already runs that claim
protocol ([ADR 0021](0021-claim-a-card-before-posting.md),
[ADR 0026](0026-alerts-in-a-persons-chat.md)). It is keyed by `(event_key, recipient_id)`, and an
addressed person is not a recipient.

## Decision

We will store a person's claim rows in `active_event_recipients` with
`recipient_id = "aad:" + <Entra object id>`.

* Recipient ids are UUIDs, so the prefix cannot collide with one.
* Delivery runs on a `chatTarget` built from the directory user: the key, the conversation
  reference, and callbacks that keep the blocked flag on `directory_users`. `sendToChat` is the same
  code a linked recipient's message goes through.
* `closeEvent` finds the chat again by the prefix (`chatForKey`), and renders the close with the
  first addressed delivery the plan still has, for that person.

A separate `active_event_people` table was not chosen. It would copy the whole claim state machine
into two sqlc dialects and gain no behaviour.

## Consequences

* During a rolling upgrade, a replica running the previous release that closes such an event finds
  no recipient called `aad:…`. It forgets the row without sending the close. The window lasts as long
  as the rollout, and the next release has nothing more to do about it.
* `DeleteRecipient`'s cascade never touches these rows, because no recipient has these ids.
* No schema change.
