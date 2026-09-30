# 0061. Nobody opts out while the app is installed for everyone

* Status: Accepted
* Date: 2026-09-30

## Context

With `bot.global-install` on, the bot's Teams app is installed for every member so that IT can
reach anyone ([ADR 0059](0059-install-the-teams-app-for-every-member.md)). A person can still leave
in three ways built for the opt-in model:

* send `/unlink`, `stop` or `unsubscribe` in the chat
  ([ADR 0032](0032-retiring-a-link-from-the-chat.md))
* press Unlink on `/admin/notifications`
  ([ADR 0028](0028-self-service-unlink-and-code-cancellation.md))
* remove the app, which retires the link ([ADR 0032](0032-retiring-a-link-from-the-chat.md))

A password-expiry reminder that someone can switch off does not do its job, and IT asked for
exactly this: when the app is installed for everyone, the opt-out goes.

The bot also never heard about personal installs it did not start. `/bot/messages` refused personal
`installationUpdate` activities, and a `conversationUpdate` adding the bot only answered with the
link instructions.

## Decision

We will turn off every way out while `bot.global-install` is on:

* `/unlink` and its bare-word synonyms answer that IT manages the chat, and delete nothing. `/help`
  leaves them out.
* `/admin/notifications` hides Unlink, and a post to it redirects with `error=managed`.
* Removing the app marks the person `removed` on their directory row. The next run reinstalls it.
  The linked recipient, if any, is kept.

An admin can still delete a recipient on `/admin/recipients`. That is administering somebody else's
link, not opting out.

Link codes keep working until the admin-UI user's own chat is bound from sign-in claims, which a
later ADR decides.

We will also record personal installs and removals whatever the setting:

* `/bot/messages` admits personal `installationUpdate`.
* An install (`installationUpdate` add, or the bot in `membersAdded`) stores the conversation and
  service URL on the sender's directory row, creating a bare row if no run has listed them yet.
* Any later personal activity refreshes that row, as `refreshRecipientServiceURL` does for
  recipients.
* A removal marks the row `removed`.

Only the `conversationUpdate` greets, because both events arrive for one install. With global
install on, the greeting is `bot.welcome-message`, and nothing when that is empty. Otherwise it is
the link instructions, as before.

Recording a directory row off an install event does not contradict
[ADR 0026](0026-alerts-in-a-persons-chat.md), which refused to bind a recipient from one. A
recipient grants alerts to whoever holds the chat. A directory row grants nothing by itself: only a
route an admin created delivers to it.

Alternatives considered:

* **Keep the opt-out and let IT live with it.** That was the ask to remove.
* **Hide the commands but still retire the link on uninstall.** A person could still opt out by
  uninstalling, and the next run would reinstall anyway, so the link would flap.

## Consequences

* A person who removes the app gets it back within `bot.reconcile-interval`. The README says so.
  Teams setup policies can also stop people removing an app.
* The manifest still lists `/unlink` in `commandLists`; the bot answers it with the refusal.
  Milestone 21's generated manifest leaves it out when global install is on.
* Amends ADR 0028 and ADR 0032 for deployments with global install on. Neither changes when it is
  off.
* No schema change.
