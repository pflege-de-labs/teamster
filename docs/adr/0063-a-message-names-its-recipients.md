# 0063. A message names its recipients, and the answer says who was not reached

* Status: Accepted
* Date: 2026-09-30

## Context

An addressed route delivers to the people a message names
([ADR 0062](0062-a-route-may-deliver-to-the-people-a-message-names.md)). The message has to say who
they are, in a form an IT sender has: a UPN or a mail address, sometimes an Entra object id.
Alertmanager has no field for it, only labels.

One message now becomes many deliveries, and some of them fail for good. An address can be a typo,
a guest, someone who has left, or someone without the app. One status code for the whole request
cannot say that. A 502 would make the sender retry what can never succeed, and re-send to everybody
who already got it. A 200 would hide the failures.

## Decision

**Naming people.** The universal format gains a top-level `recipients` array, carried as
`UniversalEvent.Recipients`. Any source may set the label `teamster_recipient` instead, with
addresses separated by commas. `models.AddressesOf` reads the list when there is one and the label
otherwise, trims and deduplicates without regard to case. Like `AttributesOf`, it is a function so
templates cannot reach it. A key derived for a message without one includes the sorted recipients,
but only when there are any, so every existing derived key stays the same. The label is already
hashed with the labels.

**Expanding.** After `Plan`, `processEvent` replaces each addressed delivery with one per person:

1. `people.Resolver` finds the directory user.
2. `people.Installer.Ensure` makes sure the bot has a chat with them. With global install on, it
   installs the app for at most `bot.inline-install-budget` people per request.
3. Each person's delivery carries `PersonID` and renders with `.Recipient` set to them.
4. People are sent to in parallel, at most `webhook.fanout-concurrency` at a time. Deliveries to
   channels and linked chats stay sequential, as before.

A closed event is not expanded; it walks the claim rows the open left
([ADR 0064](0064-chat-claims-for-people-share-the-recipient-table.md)). A message naming more than
`webhook.max-recipients` people is refused with 400 before anything is sent.

**Answering.** A failure is **permanent** when sending the same message again cannot change it:
`invalid-address`, `unknown-recipient`, `ineligible`, `not-installed`, `no-recipient` (the route
addresses people and the message named none), or `blocked`. Anything else is **transient**: Graph
or the Bot Connector down, a store error, a claim in flight.

| Outcome | Answer |
| --- | --- |
| Everything delivered | `200 {"status":"ok"}`, as before |
| Some permanent failures, at least one delivery | `200 {"status":"partial","delivered":n,"undelivered":[{"recipient":…,"reason":…}]}` |
| Only permanent failures | `422`, same body |
| Any transient failure | `502`, as before |

An Alertmanager batch adds up its alerts. Each permanent reason is counted in
`teamster.deliveries` as that route's outcome, never per person.

Alternatives considered:

* **A 207 Multi-Status body.** No sender handles it, and Alertmanager reads anything 2xx as done.
* **502 for any failure.** Retries what cannot succeed, and resends to people who already have it.
* **Recipients only as a label.** A label is one string. A list is what an IT script has.

## Consequences

* A one-shot message (no `state`, [ADR 0035](0035-a-message-without-a-status-is-delivered-once.md))
  retried after a 502 reaches everyone again. The README says to use `state` `open` and a `key`
  when that matters. An idempotency key is milestone 23.4.
* A person can receive the same message twice, through an addressed route and a route to their
  linked chat. This is not deduplicated.
* `webhook.max-recipients`, `webhook.fanout-concurrency`, `bot.inline-install-budget` and
  `bot.directory-ttl` take effect.
* Refines ADR 0035: a one-shot message to several people answers per person as above.
