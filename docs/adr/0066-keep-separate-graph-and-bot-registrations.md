# 0066. Keep the Graph and bot registrations separate

* Status: Accepted
* Date: 2026-09-30

## Context

[ADR 0026](0026-alerts-in-a-persons-chat.md) gave the bot its own Entra registration, apart from
the Graph one. Since then Graph has gained install work: install state per team
([ADR 0045](0045-channel-delivery-through-the-bot.md)) and installing the app for every member
([ADR 0059](0059-install-the-teams-app-for-every-member.md)).

Teamster's Teams app belongs to the bot registration. The Graph registration therefore cannot use
the `Self` install permissions, which cover only the caller's own app, and needs the tenant-wide
`TeamsAppInstallation.ReadForTeam.All` and `TeamsAppInstallation.ReadWriteForUser.All`. The
second one can install any catalog app for any user.

Granting the Graph permissions to the bot registration instead would make the `Self` variants
apply.

## Decision

We will keep two registrations, and document the full permission scope and how to reduce it in
[Entra permissions and how to minimise them](../permissions.md).

One registration was rejected:

* It narrows only the two install permissions. `User.Read.All`, the broadest one, is unchanged.
* One secret would read the directory, install the app and post as the bot. Today a leaked Graph
  secret cannot post, and a leaked bot secret cannot read the directory or install anything.
* The secrets could no longer be rotated, revoked or left to expire independently: one lapse would
  stop delivery and the pickers together.
* It would have to be the bot registration, since the bot id keys every stored conversation. That
  means a manifest change, a configuration fallback, and no `bot.tenant-type: multi`.

The same risk is reduced without a merge: install through a Teams app setup policy, or grant the
install permission only while rolling out.

## Consequences

* Nothing changes for existing deployments.
* Operators can choose a smaller permission profile from the permissions page. None of the
  profiles needs a code change.
* Revisiting this needs the steps listed under
  [One registration](../permissions.md#evaluated-alternative-one-registration).
