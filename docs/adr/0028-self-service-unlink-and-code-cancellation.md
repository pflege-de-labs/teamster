# 0028. Self-service unlink resolves from the session, and a code can be cancelled

* Status: Accepted
* Date: 2026-09-16

## Context

[ADR 0027](0027-notify-on-link-displacement.md) shipped the notice a displaced conversation gets when
a link code redeems over an existing recipient, and explicitly parked the questions an unlink command
would have to answer: "who may run it, on whose behalf, through what UI". This change is that command.

A link code is a bearer credential: whoever types it into the bot chat becomes the recipient for that
subject, replacing any existing binding in place. That framing decides both questions ADR 0027 parked,
and a third one an adversarial review of this change's first draft raised: once a code exists, is
there any way to kill it before its ten-minute TTL runs out, short of waiting?

## Decision

**Who may unlink whose chat.** Unlinking is self-service, exactly like minting a code
([ADR 0026](0026-alerts-in-a-persons-chat.md)): `unlinkNotifications` resolves the row to delete from
`currentSession(r).Subject` alone, the same way `notificationsPage` and `handleMintLink` resolve whose
recipient to show or replace. Nothing it reads — not a form field, not the principal a middleware
already attached to the request — can name a different subject's row. There is no operator-driven
"unlink this other person" action; if that is ever needed, it is a distinct, separately-authorized
action (`administer` on `Recipient`, most likely), not a variant of this one with an id parameter
added.

**What the chat is told.** The unlink itself is authoritative the moment `DeleteRecipient` commits;
telling the conversation is best-effort afterward, the same fire-and-forget shape
`notifyConversationDisplaced` already uses for a displaced link — logged on failure, never allowed to
undo the unlink. The message (`linkRemovedReply`) says the link was removed deliberately from the
Notifications page and how to get alerts again, so nobody watching a chat that suddenly goes quiet has
to guess whether that was intended.

**A code can be cancelled, not only superseded.** Before this change, the only way to invalidate a
live code was to mint a replacement — `createLinkFlow` already retires a subject's outstanding code
the moment a new one is drawn — which does nothing for the case that actually needs a kill switch: a
code pasted into the wrong window, where nobody wants a replacement, only for the old one to stop
working. `cancelLink` calls the store's existing `DeleteLinkFlowsForSubject(session.Subject)` directly,
with no code or replacement involved, and is offered on the page whether or not the caller is
currently linked — a code can be outstanding either way, and there is no reason to make cancelling
one depend on the unrelated question of whether a chat is already linked.

Both actions share the plumbing minting already uses: resolution from the session, `formPostTo`'s
same-origin check, and (new in this change) a closed set of `?notice=`/`?error=` values translated
server-side rather than free text round-tripped through the query string — the page's whole purpose
is instructing someone to type a credential into a chat, which is exactly the shape a phishing link
takes, so nothing arriving on the query string is ever rendered verbatim.

## Consequences

A person can now fully self-manage their own binding — mint, cancel, unlink — without an
administrator's involvement, matching the self-service framing ADR 0026 already established for
minting. `/admin/notifications`, its two write actions and the nav link to it are registered only when
the bot is configured (`config.BotConfig.Configured()`), consistent with `POST /bot/messages`: a code
minted on an unconfigured deployment could never be redeemed, so offering the page at all would be
telling someone to talk to a bot that does not exist.

What this does not add: an administrator-initiated unlink of someone else's chat, or a record of who
held a code before it was cancelled or superseded. Both remain parked, the same way ADR 0027 parked
this change; either is worth its own ADR if a future review or a real incident needs it.
