# 0087. Anyone may route to the people a message names

* Status: Accepted
* Date: 2026-10-03

## Context

A route can deliver to the people a message names
([ADR 0062](0062-a-route-may-deliver-to-the-people-a-message-names.md)). Only admins could create,
edit or delete such a route. When that was decided, a route of this kind reached anyone in the
tenant, because naming people took no permission of its own.

Since [ADR 0082](0082-naming-people-takes-permission.md), it does. A token's message level decides
whom a message may name: `self` only its creator, `anyone` anyone. It is checked against the
creator's current grant on every request, before routing. Whom a message reaches through an
addressed route is therefore already bounded by the sender, whoever wrote the route.

Routes, on the other hand, need a fixed target. A user who wants to send personal messages has to
ask an admin for the one route that would let them, or route to their own chat only. A template for
personal messages then cannot be shared across users and recipients.

## Decision

We will permit `deliverToAddressed` to every signed-in `User` in the embedded Cedar policies. The
route target is labelled **Any person named in the message**.

* Who may save a route is decided as before. Creating or editing one still takes `edit` on routes,
  and attaching a template still takes `attach` on that template.
* Whom such a route reaches is decided by the sender's token level alone (ADR 0082), at least the
  token's creator.
* The `mayAddress` check stays in the code. A principal that is not a `User` still fails it, and a
  later policy file can narrow it again.

Alternatives considered:

* **Scope a non-admin's route to their own messages.** The server would set a `teamster_sender`
  label, as it sets `teamster_source`, and a non-admin's addressed route would match only the
  messages sent with their tokens. It is safer, but a route then cannot be shared among the users
  it was meant to serve. It was declined in favour of a single shared target.
* **Make `deliverToAddressed` grantable at `/admin/access`.** This keeps every route behind an
  admin, which is the friction this decision removes.

## Consequences

* Anyone who may edit routes can set up personal messages for themselves and others, and one
  template serves every sender.
* **Accepted risk.** An addressed route matches every message its selector matches, whoever sent
  it. A user can write an addressed route with a broad selector, such as `teamster_source=universal`,
  and attach their own template. That route then sends an extra message, rendered from that
  template, to the people *other* senders name. It reaches no one those senders could not already
  message, but the content is the route author's. Routes are visible in the routing picture and
  their changes in the audit trail. Sender scoping (above) is the remedy if this is abused.
* The German label is **Jede in der Nachricht genannte Person**. The German user documentation
  changes with it.
* Nothing changes in the schema or in the configuration.
