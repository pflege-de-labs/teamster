# 0047. A route targets a channel or a person, and a person only themselves

* Status: Accepted
* Date: 2026-09-29

## Context

[ADR 0026](0026-alerts-in-a-persons-chat.md) gave a route two independent targets, `destination_id`
and `recipient_id`, and let any editor point a route at any recipient.

In practice this went wrong in three ways:

* The route form had a select for each target, and neither offered an empty choice. Picking a person
  still saved the first destination, usually the default one, so the alert went to a channel the
  editor never chose.
* A child that named a person still inherited its parent's channel, so "send this to me instead" also
  sent it to the channel.
* An editor could send alerts into anybody's chat. A recipient proves that the chat belongs to that
  person. It does not show that they agreed to receive whatever an editor routes to them.

## Decision

We will make a route target **either** a channel **or** a person.

* `routing.ValidateRoute` refuses a route that names both. This applies to root and child routes.
* A target a route sets replaces the inherited target of **both** kinds. A child naming a person
  delivers only to that person; a child naming a channel delivers only to that channel. A child
  naming neither inherits whatever its parent resolved to.
* The form has one **Delivers to** select, with the values `destination:<id>`, `recipient:<id>` and
  an empty inherit choice. Only one target can be submitted, and no JavaScript is needed. The JSON
  API keeps both fields; validation rejects a body that sets both.

We will let a session route only to **its own** chat, unless the session is an admin.

* A new Cedar action, `deliverToRecipient`, has the recipient's subject as a `User` resource. The
  policy permits it when `principal == resource`. Admins pass through the blanket admin policy.
* The check applies to the target a write names, and to the person a stored route already delivers
  to. An editor can neither repoint someone else's personal route nor edit or delete it.
* The form lists only the chats this session may target, plus the person an edited route already
  names, so the editor can see who it is.
* Grants to users or groups, added later, will widen this policy. They will not be written as Go
  checks.

Alternatives considered:

* **A radio button plus two selects.** This needs JavaScript to disable the hidden select, or a
  server rule for which field wins. One select states the choice directly.
* **Keeping inheritance per kind.** A child naming a person would then still send to its parent's
  channel. That is the surprise this ADR removes.
* **Self-only for admins too.** Admins already configure everything else, and cleaning up after
  someone who left needs this. User and group grants are the planned way to delegate further.

## Consequences

* A route saved before this change that names both targets keeps delivering to both. `/admin` marks
  it with a badge. Editing it shows the channel, and saving it drops the person.
* **Delivery changes for existing trees.** A child that names only a person no longer also sends to
  the channel it inherited. A greedy child that names a person now delivers only to that person.
  Operators relying on the old fan-out add a sibling route for the channel.
* Editors lose the ability to route to other people's chats. Admins keep it.
* No schema change. Configuration import already refuses routes that name a person.
* Amends [ADR 0026](0026-alerts-in-a-persons-chat.md): the "two independent targets" and "any
  editor may target any recipient" decisions.
