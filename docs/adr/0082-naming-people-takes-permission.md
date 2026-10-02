# 0082. Naming people in a message takes permission

* Status: Accepted
* Date: 2026-10-02

## Context

A message names its recipients in `recipients` or the `teamster_recipient` label
([ADR 0063](0063-a-message-names-its-recipients.md)). Any route that delivers to the people a
message names then sends each of them a chat message from the bot. Only admins may create such a
route ([ADR 0062](0062-a-route-may-deliver-to-the-people-a-message-names.md)), but once it exists,
whoever holds a token for the webhook decides who is messaged. Since 0.11 every user may mint a
token ([ADR 0077](0077-scoped-tokens-answer-to-their-creator.md)), so every user could message the
whole tenant in the name of the bot.

What senders need differs:

* someone automating a reminder to themselves;
* office administrators sending notices to colleagues;
* infrastructure that sends with `webhook.token`, which has no creator to ask.

## Decision

* **Two actions on `People::"*"`.** `messageSelf` names only the sender, and `message` names
  anyone. `messageSelf` is part of `message` in the action hierarchy, so a grant of `message`
  covers both.
* **Self is everyone's.** A base policy permits `messageSelf` to every `User`. `message` comes from
  a grant to a user, group, provider group or role, set at `/admin/access` or with
  `PUT /api/access/messages`. Admins hold it through the admin policy.
* **A token is scoped to a message level.** It is none, `self` or `anyone`, set at minting and never
  above the creator's level. It is stored in `access_tokens.message_scope` and rendered as a second
  token policy. Every use asks both the token's policy and the creator's current permissions, as
  for webhooks: a creator who loses `message` falls back to self.
* **Self is the creator's object id.** For `self`, every address must resolve, through the
  directory, to the creator's Entra object id. That id is recorded at sign-in in `users.object_id`
  from `auth.object-id-claim`, or taken from the chat they linked. Unknown means refused.
* **No creator, no recipients.** `webhook.token` and tokens from before scopes cannot name people.
  This is a breaking change.
* **Before anything is delivered.** The check runs on the whole request before routing, so a
  refused Alertmanager batch delivers none of its alerts. A refusal is a `403` counted as
  `forbidden`.

Alternatives considered:

* **Guarding only addressed routes.** The recipients would still be accepted and silently ignored
  wherever no addressed route matched, and a route added later would start messaging them.
* **Keeping `webhook.token` trusted.** It is the one credential nobody answers for, and one token
  for every sender. The user asked for recipients to need a grant.
* **A flag on `webhook.token`.** It keeps the creatorless path alive for every deployment that
  never turns it off.
* **Comparing addresses with the creator's mail address.** The address in `users` is what the
  provider claimed, unverified. The object id is what the directory resolves to.

## Consequences

* Deployments that send recipients with `webhook.token` or an old token get `403` until they mint
  a token with a message level. The changelog marks it breaking.
* A self-only token of a user Teamster has no object id for is refused until they sign in with the
  claim or link their chat.
* The self check resolves each address, which may ask Graph. The answer is cached in the directory,
  so the delivery that follows does not ask again.
* Broadcasting to everyone builds on this: a third level and action above `message`.
* Additive migration: `users.object_id` and `access_tokens.message_scope`, both defaulting to
  empty. The previous release ignores them.
