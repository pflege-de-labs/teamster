# 0032. A link can be retired from the chat it belongs to

* Status: Accepted
* Date: 2026-09-18

## Context

Two ways out of a link exist already: a person unlinks themselves at
`/admin/notifications`, and an admin unlinks anyone at `/admin/recipients`. Both need an admin-UI
session. Neither helps the case that actually happens — somebody uninstalls the bot from Teams, or
types "stop" at it, and expects that to mean something.

Today it means nothing. Uninstalling removes the conversation without telling teamster, so the
recipient row survives, every alert routed to that person is attempted, and each one fails
permanently. [ADR 0026](0026-alerts-in-a-persons-chat.md) built the machinery that copes —
`MarkRecipientBlocked` and the flag on `/admin/recipients` — but coping is not the same as the link
being gone. The row stays until an admin notices and removes it by hand.

[ADR 0028](0028-self-service-unlink-and-code-cancellation.md) parked exactly the questions this
raises: *who may run an unlink command, on whose behalf, through what UI*. This record answers them.

## Decision

We will retire a link from the chat side, two ways, sharing one mechanism.

**A chat command.** A message that *is* `unlink`, `stop` or `unsubscribe` — case-insensitive, with
the bot's own mention stripped the way a link code already has it stripped — deletes the recipient
bound to that conversation and confirms in the same chat.

Whole-message, not a search. `unlink` is an ordinary English word, unlike a link code, which is
twelve characters from a 31-symbol alphabet and cannot be typed by accident. "How do I unlink this
chat?" must not unlink it. A message containing both a command word and a valid code links, because
the code was checked first and a code is unambiguous intent.

**`membersRemoved`.** A `conversationUpdate` whose `membersRemoved` names the bot itself retires
the link for that conversation. No reply is sent: the bot has just been removed, so there is
nothing to send to. Any other member leaving is ignored — a personal conversation is 1:1 with the
bot by construction, and something else leaving is not the person saying they are done.

**Controlling the chat is the whole authorization, and that is the asymmetry worth stating.**
[ADR 0026](0026-alerts-in-a-persons-chat.md) refused to bind a recipient off a membership event,
because "nothing in that event ties the resulting recipient to any existing Grant, so it would let
a person who can install a Teams app — a much lower bar than an authenticated admin-UI session —
opt into alerts nobody granted them." That reasoning is about *linking*, and it does not carry over
to unlinking, because the two are not symmetric:

* Linking **grants** — it starts sending somebody alerts, so it must prove an admin-UI session
  authorised it. Hence the code.
* Unlinking only ever **revokes**, and only for the conversation the activity came from. The worst
  a caller can do is stop their own alerts. Microsoft signed the activity and the conversation is
  personal, so whoever sent it holds that chat — and somebody who holds the chat can already stop
  delivery by uninstalling the bot. A command that refused would only be harder to use, not safer.

So there is no new Cedar action, no session, and no id parameter. The conversation the activity
arrived on *is* the identity, and it is the only row that can be touched. This is deliberately not
the "operator-driven unlink of someone else's chat" ADR 0028 said would need its own record and its
own authorization — that remains unbuilt.

**A store lookup by conversation, with a non-unique index.** Both paths know only the conversation.
`GetRecipientByConversation` answers that; migrations `sqlite/0009` and `postgres/0006` add the
index. It is **not** unique: `recipients` is keyed by subject, and one Teams user holding two
admin-UI subjects — a local login and an OIDC one — can redeem a code for each from the same chat.
A unique index would refuse the second and break a legitimate link, so the query orders and takes
one. `refreshRecipientServiceURL`, which used to list every recipient and scan, now asks the same
question directly.

## Consequences

An uninstall now cleans up after itself, which is the case the blocked flag was only ever a
consolation for. The flag stays — it is still what a *transient-looking* permanent failure sets
when the person blocked the bot without uninstalling it — but it should fire far less often.

The bot answers three more words than it used to, and a person who types one loses their alerts
immediately. That is the point, and it is recoverable: the reply says so, and a new code relinks.

Deleting cascades through `store.DeleteRecipient`, so the active-alert rows go with it and nothing
is stranded — the invariant `DeleteActiveAlertRecipientsFor` exists for.

The replies are English string constants in Go beside the ones already there, not catalog entries.
[ADR 0015](0015-ui-text-in-catalogs.md) covers the admin UI; the bot has never been localized, and
doing it for three strings while the rest stay English would be worse than either.

What this does not add: an unlink command for anyone other than the sender, a record of who retired
a link, or handling for a bot removed from a conversation it was never linked to — that resolves to
nothing and is already a no-op.
