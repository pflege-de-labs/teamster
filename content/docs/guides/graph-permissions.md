---
title: Grant Microsoft Graph permissions
weight: 2
---

Teamster talks to Microsoft through two separate Entra app registrations. This guide shows which
permissions each one needs, what each permission reaches beyond Teamster's own use, and how to run
with fewer.

## Know the two registrations

| Identity | Configured by | Token audience | Graph permissions |
| --- | --- | --- | --- |
| Graph registration | `graph.*` | `https://graph.microsoft.com/.default` | application permissions, below |
| Bot registration | `bot.*` | `https://api.botframework.com/.default` | none |
| Keycloak broker token (optional) | `auth.broker.*` | Microsoft Graph | delegated `Team.ReadBasic.All`, `Channel.ReadBasic.All` |

Both registrations sign in with a client secret through the client credentials flow. Certificates
and federated credentials are not supported.

The bot registration holds no Graph permissions. It posts to any team, and messages any person,
that has Teamster's Teams app installed, and edits what it posted. Set it up with
[Set up the Teams bot](../teams-bot/). The Keycloak broker token is the signed-in admin's own
token; see [Configure Keycloak](../keycloak/).

Keep the two registrations apart. Each secret is then revoked and rotated without touching the
other. [ADR 0066](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0066-keep-separate-graph-and-bot-registrations.md)
records why, and the single-registration alternative that was evaluated.

## Grant the application permissions

The Graph registration signs in as the application itself, so it needs **application**
permissions with tenant admin consent. Grant the ones for the features you use:

| Permission | Needed for | Reaches beyond Teamster's use | Without it |
| --- | --- | --- | --- |
| `Team.ReadBasic.All` | Team picker; Team names in the routing graph, export and import | name and description of every team in the tenant | Team ids are typed by hand, and shown instead of names |
| `Channel.ReadBasic.All` | channel picker; channel names in the same places | name and description of every channel in every team | channel ids are typed by hand, and shown instead of names |
| `TeamsAppInstallation.ReadForTeam.All` (optional) | install state of a team the bot has not heard from | the list of apps installed in every team | such a team shows *unknown* |
| `User.Read.All` (with `bot.global-install`) | listing the tenant's members, finding a person by UPN or mail | the full profile of every user | `bot.global-install` cannot run |
| `TeamsAppInstallation.ReadWriteForUser.All` (with `bot.global-install`, optional) | installing Teamster's app for each member | installing, upgrading and removing *any* catalog app for any user; not consenting to that app's resource-specific permissions | nothing is installed; only people who already have the app are found |
| `AppCatalog.Read.All` (with `bot.global-install`, unless `bot.catalog-app-id` is set) | finding Teamster's app in the organization catalog | every app in the organization catalog | set `bot.catalog-app-id` instead |

{{< callout type="warning" >}}
Roles are baked into the access token. After granting or revoking consent, restart Teamster, or it
keeps the old token for up to an hour.
{{< /callout >}}

### Pick the right variant

* `User.ReadBasic.All` is not enough for `bot.global-install`. It cannot read or filter
  `accountEnabled` and `userType`, and Graph answers the member listing with
  `403 Authorization_RequestDenied`.
* Grant the tenant-wide `TeamsAppInstallation` permissions, not the `Self` variants Graph may list
  in a refusal. The `Self` variants cover only a Teams app tied to the Graph registration, and
  Teamster's app belongs to the bot registration.

### Do not grant posting permissions

Graph does not let an application post or edit channel messages, so the bot does both. Do not grant
these for Teamster:

* `Teamwork.Migrate.All` is the only way to post as an application, and it is for importing message
  history.
* `ChannelMessage.UpdatePolicyViolation.All` is the only way to edit as an application, and it may
  change only a message's `policyViolation` field.

## Know what a leaked secret exposes

| Secret | An attacker can |
| --- | --- |
| `graph.client-secret` | read whatever the granted permissions allow; with the install permission, push catalog apps onto users. It cannot post or edit a message. |
| `bot.client-secret` | post and edit as the bot in every team, and message every person, that has the app. It cannot read the directory or install anything. |

## Run with fewer permissions

Each of these works on its own.

1. **Grant the Graph registration nothing.** Teamster still requires `graph.*` to be set, but
   delivery needs no permission: the bot posts everything. Pickers fall back to ids typed by hand,
   and the routing graph, export and import show ids rather than names.
2. **Drop `TeamsAppInstallation.ReadForTeam.All`.** Install state then comes from the bot's own
   install events. A team that installed the app before the bot was listening shows *unknown* until
   it sends the bot an event.
3. **Set `bot.catalog-app-id`** and drop `AppCatalog.Read.All`. Teams admin center shows the
   catalog id, or ask Graph once:
   `GET /appCatalogs/teamsApps?$filter=externalId eq '<manifest-id>'`.
4. **Install through a Teams app setup policy** instead of granting
   `TeamsAppInstallation.ReadWriteForUser.All`. `bot.global-install` then only finds the chats of
   people the policy installed the app for, and `/admin/people` shows the others as failed.
   `User.Read.All` is still required to list members.
5. **Grant the install permission only for the rollout.** Run **Install for all users** once,
   revoke `TeamsAppInstallation.ReadWriteForUser.All`, and restart. Later runs only find chats; new
   members need the setup policy or another temporary grant.
6. **Leave `bot.global-install` off** if people link their chat themselves. `User.Read.All`,
   `TeamsAppInstallation.ReadWriteForUser.All` and `AppCatalog.Read.All` are then unused.
7. **Delegated pickers** through the Keycloak broker add each admin's own teams and channels to the
   pickers. They do not replace the tenant-wide list, so you cannot drop `Team.ReadBasic.All` or
   `Channel.ReadBasic.All` without losing it.
8. **Outside Teamster:** short secret lifetimes, rotating the two secrets on separate schedules,
   and, with Entra Workload ID, conditional access that limits where each registration's tokens may
   be requested from.

### Choose a profile

| Profile | Graph registration grants |
| --- | --- |
| Minimal: channels and linked chats, ids typed by hand | none |
| Channels with pickers | `Team.ReadBasic.All`, `Channel.ReadBasic.All` |
| Global install, apps installed by a setup policy | the above and `User.Read.All`; `AppCatalog.Read.All` only serves an install, so it can go too |
| Maximum | every permission in the table above |
