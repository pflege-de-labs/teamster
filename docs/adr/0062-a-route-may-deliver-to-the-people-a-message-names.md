# 0062. A route may deliver to the people a message names

* Status: Accepted
* Date: 2026-09-30

## Context

IT sends password-expiry reminders: one message per person, each naming who it is for. A route
targets a channel or one linked person ([ADR 0047](0047-a-route-targets-a-channel-or-yourself.md)),
so reaching a thousand people would take a thousand routes. They would have to be written ahead of
time for people who never linked a chat.

The people are known only per message. Routing still has work to do, because it decides which
template renders a reminder and which kinds of message may be sent this way at all. Only the "who"
comes from the message.

## Decision

We will give a route a third kind of target: **the people the message names**.

* `routes.addressed` (`BOOLEAN NOT NULL DEFAULT false`) marks such a route. It is a column rather
  than a sentinel in `recipient_id`, for two reasons. The previous release, which never reads the
  column, then sees a route with no target and delivers nothing through it, rather than failing to
  look up a recipient called `@addressed` on every message. And configuration import keeps refusing
  a real `recipient_id` while an addressed route travels, because it names nobody.
* `ValidateRoute` allows one target per route: a channel, a person, or addressed. The inheritance
  of ADR 0047 covers the third kind too. A route that sets any target replaces an inherited target
  of every kind, so a child that addresses people no longer posts to its parent's channel, and a
  child that names a channel stops addressing people.
* `routing.Plan` stays a function of labels. It emits one `Delivery` of kind `addressed` per route,
  with no person in it. Delivery expands it into one delivery per person, once the message says who.
  `/api/routing/match` and the routing picture use `Plan` unchanged, and the picture draws a single
  "People named in the message" node.
* Pointing a route at the people a message names reaches anyone in the tenant. A new Cedar action,
  `deliverToAddressed`, on the resource `AddressedPeople::"all"`, is permitted to admins by the
  blanket policy and to nobody else. `policies.cedar` shows how to grant it to a custom role such
  as `it-messaging`. The check covers the route a write names and the route already stored, so an
  editor can neither create an addressed route nor edit or delete one.
* The route form offers the target when the bot is configured and the session may use it, and
  keeps it on an edited route that already addresses people.

The routing picture used to inherit each kind of target on its own, so a child naming a person was
drawn with its parent's channel too. That disagreed with routing since ADR 0047. It now follows the
same rule as `routing.collect`.

Alternatives considered:

* **Expanding in the router.** `Plan` would need the store's directory and the message, and the
  match probe and the picture would have to invent people.
* **A route per person, created on demand.** It fills the route table with rows nobody configured,
  and says nothing about which template renders them.
* **Open to editors.** An editor may route only to their own chat. Addressing reaches everybody,
  which is a bigger grant than anything an editor has.

## Consequences

* Until messages can name people, an addressed delivery fails with a clear error: a 502 to the
  sender.
* Admins, or a role an admin grants, create these routes.
* Schema: one additive column, SQLite `0021` and Postgres `0018`. The previous release keeps
  running and delivers nothing through an addressed route.
* Amends ADR 0047: the third target kind follows the same replacement rule.
