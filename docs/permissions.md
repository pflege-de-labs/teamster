# Entra permissions and how to minimise them

Teamster talks to Microsoft through two Entra app registrations, and optionally a third, delegated
identity. This page lists everything each of them can be granted, what that grant reaches beyond
Teamster's own use, and how to run with less. Why there are two registrations and not one is
recorded in [ADR 0026](adr/0026-alerts-in-a-persons-chat.md) and
[ADR 0066](adr/0066-keep-separate-graph-and-bot-registrations.md).

## Identities

| Identity | Configured by | Credential | Token audience | Graph permissions |
| --- | --- | --- | --- | --- |
| Graph registration | `graph.*` | client secret, client credentials flow | `https://graph.microsoft.com/.default` | application permissions, see below |
| Bot registration | `bot.*` | client secret, client credentials flow | `https://api.botframework.com/.default` | none |
| Keycloak broker token (optional) | `auth.broker.*` | the signed-in admin's Entra token, stored by Keycloak | Microsoft Graph | delegated `Team.ReadBasic.All`, `Channel.ReadBasic.All` — see [Configuring Keycloak](keycloak.md#delegated-teams-and-channels) |

The bot registration holds no Graph permissions. What it can do comes from the Bot Framework: it
can post to any team, and message any person, that has Teamster's Teams app installed, and edit
what it posted. The Teams app belongs to this registration (`bots[0].botId` in the manifest).

Teamster supports only client secrets for both registrations, not certificates or federated
credentials.

## Maximum scope

Every application permission the Graph registration may be granted, with admin consent:

| Permission | Needed for | Reaches beyond Teamster's use | Without it |
| --- | --- | --- | --- |
| `Team.ReadBasic.All` | Team picker; Team names in the routing graph, export and import | name and description of every team in the tenant | Team ids are typed by hand, and shown instead of names |
| `Channel.ReadBasic.All` | channel picker; channel names in the same places | name and description of every channel in every team | channel ids are typed by hand, and shown instead of names |
| `TeamsAppInstallation.ReadForTeam.All` (optional) | install state of a team the bot has not heard from | the list of apps installed in every team | such a team shows *unknown* |
| `User.Read.All` (with `bot.global-install`) | listing the tenant's members, finding a person by UPN or mail | the full profile of every user | `bot.global-install` cannot run |
| `TeamsAppInstallation.ReadWriteForUser.All` (with `bot.global-install`, optional) | installing Teamster's app for each member | installing, upgrading and removing *any* catalog app for any user. It cannot consent to that app's resource-specific permissions | nothing is installed; only people who already have the app are found |
| `AppCatalog.Read.All` (with `bot.global-install`, unless `bot.catalog-app-id` is set) | finding Teamster's app in the organization catalog | every app in the organization catalog | set `bot.catalog-app-id` instead |

The two `TeamsAppInstallation` permissions are the tenant-wide variants. Graph also offers `Self`
variants that cover only the calling registration's own Teams app, but Teamster's app belongs to
the bot registration, not the Graph one, so they do not apply. See
[One registration](#evaluated-alternative-one-registration).

Roles are baked into the access token. After granting or revoking consent, restart Teamster, or it
keeps using the old token for up to an hour.

## What a leaked secret exposes

| Secret | An attacker can |
| --- | --- |
| `graph.client-secret` | read whatever the granted permissions above allow; with the install permission, push catalog apps onto users. It cannot post or edit a message. |
| `bot.client-secret` | post and edit as the bot in every team, and message every person, that has the app. It cannot read the directory or install anything. |

Each secret is revoked and rotated on its own without touching the other. That separation is the
main reason the registrations stay apart.

## Ways to minimise, keeping both registrations

Each can be applied on its own.

1. **Grant the Graph registration nothing.** `config.Validate` still requires `graph.*` to be set,
   but no permission is needed for delivery: the bot posts everything. Pickers fall back to ids
   typed by hand, and the routing graph, export and import show ids rather than names.
2. **Drop `TeamsAppInstallation.ReadForTeam.All`.** Install state comes from the bot's own install
   events (`bot_teams`) instead. A team the app was installed in before the bot was listening shows
   *unknown* until an event names it.
3. **Set `bot.catalog-app-id`** and drop `AppCatalog.Read.All`. The catalog id is shown in Teams
   admin center, or once by `GET /appCatalogs/teamsApps?$filter=externalId eq '<manifest id>'`.
4. **Install through a Teams app setup policy** instead of granting
   `TeamsAppInstallation.ReadWriteForUser.All`. `bot.global-install` then only finds the chats of
   people the policy installed the app for, and `/admin/people` shows the others as failed.
   `User.Read.All` is still required to list members.
5. **Grant the install permission only for the rollout.** Run **Install for all users** once,
   revoke `TeamsAppInstallation.ReadWriteForUser.All`, and restart. Later runs only find chats;
   new members need the setup policy or another temporary grant.
6. **Leave `bot.global-install` off** if people link their chat themselves. `User.Read.All`,
   `TeamsAppInstallation.ReadWriteForUser.All` and `AppCatalog.Read.All` are then all unused.
7. **Delegated pickers** through the Keycloak broker add each admin's own teams and channels to the
   pickers. They do not replace the tenant-wide list, so they do not let you drop
   `Team.ReadBasic.All` or `Channel.ReadBasic.All` without losing the tenant-wide list.
8. **Operational measures** outside Teamster: short secret lifetimes, rotating the two secrets on
   separate schedules, and, with Entra Workload ID, conditional access that limits where each
   registration's tokens may be requested from.

### Profiles

| Profile | Graph registration grants |
| --- | --- |
| Minimal: channels and linked chats, ids typed by hand | none |
| Channels with pickers | `Team.ReadBasic.All`, `Channel.ReadBasic.All` |
| Global install, apps installed by a setup policy | the above and `User.Read.All`; the catalog lookup only serves an install, so `AppCatalog.Read.All` can go too |
| Maximum | every permission in [Maximum scope](#maximum-scope) |

## Evaluated alternative: one registration

Not implemented; recorded in [ADR 0066](adr/0066-keep-separate-graph-and-bot-registrations.md) so
it can be revisited.

If the Graph permissions were granted to the bot registration, the Teams app would belong to the
caller and the `Self` variants would apply:

| Today | With one registration |
| --- | --- |
| `TeamsAppInstallation.ReadForTeam.All` | `TeamsAppInstallation.ReadSelfForTeam.All` |
| `TeamsAppInstallation.ReadWriteForUser.All` | `TeamsAppInstallation.ReadWriteSelfForUser.All` |

`Team.ReadBasic.All`, `Channel.ReadBasic.All`, `User.Read.All` and `AppCatalog.Read.All` stay as
they are.

What it would take:

* Merge into the **bot** registration, not the Graph one. The bot id keys every stored
  conversation, `bot_teams` row and linked chat, and a different bot cannot edit cards the old one
  posted. Moving the bot would mean a new Teams app, installed again everywhere.
* Grant and consent the Graph permissions on the bot registration, then restart.
* Link the Teams app to the registration in the manifest, most likely `webApplicationInfo.id` set
  to the bot's client id — confirm against Microsoft's documentation for the `Self` permissions.
  That is a new manifest version and a catalog update.
* A configuration fallback so Graph reuses `bot.tenant-id`, `bot.client-id` and
  `bot.client-secret` when `graph.*` is empty. It needs a tenant id, so it rules out
  `bot.tenant-type: multi`.

What it costs:

* One secret reads the directory, installs the app and posts as the bot. A leak exposes all of it.
* Rotating, revoking or letting the secret expire stops delivery and the pickers together.
* The bot registration's owners become owners of the directory permissions too.
* The broadest permission, `User.Read.All`, is untouched.

The gain is two install permissions narrowed to Teamster's own app. The same risk is reduced
further by [options 4 and 5](#ways-to-minimise-keeping-both-registrations), without a merge.
