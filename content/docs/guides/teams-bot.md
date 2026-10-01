---
title: Set up the Teams bot
weight: 3
---

The Teams bot posts and edits every channel card, and sends alerts to a person's own chat. Without
it, nothing reaches Teams: startup logs that, and every channel delivery fails with `502`. This
guide sets the bot up, links people's chats, and installs the bot for everyone.

The bot is a second Entra registration, separate from the `graph` one. Revoking or rotating one
never touches the other
([ADR 0045](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0045-channel-delivery-through-the-bot.md)).

## Configure the bot

{{% steps %}}

### Register the bot

Create a Bot Framework bot with the Microsoft Teams channel enabled, and set its messaging endpoint
to `https://<external-host>/bot/messages`. An Entra app registration alone is not a bot.
[Registering the bot](https://github.com/pflege-de-labs/teamster/blob/main/manifest/README.md#registering-the-bot)
walks through the Teams Developer Portal and the Azure Bot alternative.

### Give Teamster the credentials

```yaml
bot:
  tenant-id: "<tenant-id>"
  client-id: "<bot-app-id>"
  tenant-type: "single"
```

Deliver the secret as `TEAMSTER_BOT_CLIENT_SECRET`. With the Helm chart, put the settings under
`config.settings.bot` and the secret in `credentials.botClientSecret`.

Set `bot.tenant-id`, `bot.client-id` and `bot.client-secret` together: setting only some of them
is rejected at startup. `bot.tenant-id` may stay empty when `bot.tenant-type` is `multi`.

### Match the tenant type

`bot.tenant-type` must match the registration's "Supported account types" in Entra:

| Value | Registration accepts |
| --- | --- |
| `single` (default) | this tenant's users only |
| `multi` | any tenant's users, authenticated through a shared Microsoft endpoint |

A mismatch lets the Teams app install and then fails every token request or inbound activity.

### Make `/bot/messages` reachable from Microsoft

Turning the bot on registers `POST /bot/messages`, where Teams delivers activities. Teams must reach
it from the internet. With the Helm chart, expose it on the Ingress or on `httpRoute.external`,
never only on `httpRoute.internal`.

The endpoint carries no Teamster credential. Each request is checked against Microsoft's signature,
using the Bot Framework metadata document at `bot.metadata-url`. Keep that URL `https`; Teamster
refuses to start with it empty or plain `http`.

### Package and install the Teams app

Build the app package from
[`manifest/`](https://github.com/pflege-de-labs/teamster/blob/main/manifest/README.md), replace its
placeholders, and upload it to Teams admin center. Then a team owner or Teams admin adds the app to
**every team a route posts to**. Teamster cannot install the app into a team itself.

{{% /steps %}}

Graph finds the app by its bot, so it matches `bot.client-id`; the manifest's own `id` does not
matter here.

## Check where the app is installed

**Teams** (`/admin/teams`) lists every team, its destinations and the routes that use them. Teams
that destinations use but that lack the app come first, with the steps to install it. The Team
picker, the destinations list and the routing graph also warn about a team without the app.

| State | Meaning |
| --- | --- |
| installed | the bot has heard from the team, or Graph confirms the app is there |
| missing | Graph says the app is not there |
| unknown | neither can tell |

Without `TeamsAppInstallation.ReadForTeam.All`, every team that installed the app before Teamster
was listening shows *unknown*. See [Grant Microsoft Graph permissions](../graph-permissions/).

When the app is added to a team, the bot records that team's regional Bot Connector endpoint. A
team the bot has not heard from yet is reached through `bot.service-url` (default
`https://smba.trafficmanager.net/teams/`). A post to a team without the app fails with a message
asking whether it is installed.

{{< callout type="info" >}}
Not yet confirmed against a real tenant: posting to private and shared channels, editing a card
through its stored conversation id, and whether an app upgrade re-sends install events.
{{< /callout >}}

## Link a person's chat

A route can deliver to one person's chat instead of a channel. That person links their chat first.

With [`bot.global-install`](#install-the-bot-for-everyone) on, skip this: signing in finds your chat
from your Entra account, and **Notifications** says whether it did. If the bot has no chat with you
yet, **Set up my chat now** installs it
([ADR 0065](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0065-your-own-chat-is-found-from-your-sign-in.md)).
Behind Keycloak, the user's Entra object id has to reach Teamster; see
[Configure Keycloak](../keycloak/).

Otherwise:

{{% steps %}}

### Get a link code

Sign in to the admin UI with any role, `viewer` and up, and open **Notifications**
(`/admin/notifications`). Ask for a code.

A script gets one from `POST /api/recipients/link` with a signed-in session:

```json
{ "code": "AB3D-EFGH-J2MN", "expires_at": "2026-09-16T10:30:00Z" }
```

Basic auth is refused on both, because it would bind every caller to the shared admin login.

### Send the code to the bot

Open a 1:1 chat with the bot in Teams, adding the app first if needed, and send the code. Pasting
it after an `@`-mention of the bot works.

### Wait for the confirmation

The bot confirms in the same chat. A code is single-use and expires after ten minutes. An expired
or used code gets a reply that does not say which.

### Point a route at the chat

In the route's **Delivers to**, pick the person. An editor may pick only their own chat, an admin
anyone's. A route delivers to one channel or one person, never both.

{{% /steps %}}

Redeeming a code for someone who is already linked moves their alerts to the new chat and tells the
old chat it was displaced.

In a person's chat, a repeated open event edits the message already there, and a close arrives as a
new message.

**Notifications** shows whether your chat is linked, the display name captured at link time, when
the link was made and when it last changed. A service-url refresh also updates "last changed", so a
change there alone does not prove a takeover. **Cancel my code** invalidates every code you still
hold. Every response on that page is sent `Cache-Control: no-store`.

## Talk to the bot

In the personal chat, the bot answers these commands:

| Command | What it does |
| --- | --- |
| `help` | Lists the commands. |
| `status` | Shows whether this chat is linked, to whom and since when, and which routes deliver to it. |
| `test` | Sends a test alert to this chat through the real delivery path, rendered with the universal webhook's default template. |
| `unlink` | Stops alerts arriving here. `stop` and `unsubscribe` do the same. |

Pick a command from the menu above the compose box, or type it. The command must be the whole
message; `/help` with a slash works too, but Teams' own `/` menu may catch it first. Anything else
is read as a link code.

In a team channel with the app installed, mention the bot: `@Teamster help`, `@Teamster status` or
`@Teamster test`. `status` lists the destinations naming that channel and the routes posting to
them; `test` posts a test alert to the channel. Both need a destination for the channel under
**Destinations**. The answer arrives in your message's thread. Group chats are not supported.

If `test` gets no answer, the bot cannot send. Check the `bot reply` and `bot test` lines in the
log, and that `bot.tenant-type` matches the registration.

## Stop alerts to a chat

A person has three ways out:

* **Send `unlink`** (or `stop`, or `unsubscribe`) to the bot. The bot confirms. A new link code
  reconnects.
* **Uninstall the app.** Teams reports the removal and the link retires itself.
* **Unlink in the admin UI:** your own chat from **Notifications**, or anyone's from **Recipients**.

Each removes the recipient and any alert cards still tracked for it.

With `bot.global-install` on, only the last works, and only for an admin on **Recipients**. The bot
answers `unlink` by saying IT manages the chat, **Notifications** has no Unlink button, and a
removed app is reinstalled by the next run
([ADR 0061](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0061-no-opt-out-when-installed-for-everyone.md)).

## Manage recipients

**Recipients** (`/admin/recipients`) lists everybody with a linked chat: display name (or subject),
when they linked, and which routes deliver to them. Viewing needs `viewer`; unlinking needs
`editor`.

Unlinking removes the recipient even if a route still names them. That route then shows a "missing
recipient" in `/admin/routing`.

A permanent send failure, such as a person who uninstalled or blocked the bot, marks the row
**blocked** with the reason and time. The mark is informational: the next alert is still attempted,
and a successful send clears it.

The same list and unlink action are available as `GET /api/recipients` and
`DELETE /api/recipients/{id}`.

## Install the bot for everyone

With `bot.global-install`, Teamster installs the app for every enabled member of the tenant, guests
excluded. IT can then message anyone without them linking a chat
([ADR 0059](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0059-install-the-teams-app-for-every-member.md)).

{{% steps %}}

### Publish the app to the organization catalog

Upload the app package in Teams admin center. Only an app in the organization catalog can be
installed through Graph.

### Grant the Graph permissions

`User.Read.All`, `TeamsAppInstallation.ReadWriteForUser.All`, and `AppCatalog.Read.All` unless you
set `bot.catalog-app-id`. See [Grant Microsoft Graph permissions](../graph-permissions/), which also
shows how to install through a setup policy instead.

### Turn it on

```yaml
bot:
  global-install: true
  app-id: "<manifest-id>"
  reconcile-interval: "6h"
  welcome-message: ""
```

Set `bot.app-id` to the manifest's `id`, or `bot.catalog-app-id` to the catalog id. Teamster
refuses to start with neither.

### Start the first run

Open **People** (`/admin/people`, admins only) and click **Install for all users**, or call
`POST /api/people/install`. `GET /api/people/runs/latest` reports progress.

{{% /steps %}}

After that:

* A run every `bot.reconcile-interval` installs for new members and reinstalls for anyone who
  removed the app. `0` runs only when an admin asks; any other value must be at least `5m`. Only one
  replica runs at a time.
* People who already have the app, from a setup policy for example, are only looked up. Without
  `TeamsAppInstallation.ReadWriteForUser.All`, Teamster installs nothing and only finds those chats.
* A person who left is marked departed once the member listing no longer returns them, and removed
  30 days later.
* Nobody can opt out; see [Stop alerts to a chat](#stop-alerts-to-a-chat).
* The bot greets a new install with `bot.welcome-message` when it is set, and says nothing
  otherwise.

**People** shows how many people have the app, the latest run and its progress, and failed
installs with their reason. A run started by hand retries every failed install at once, so start one
after granting a missing permission.

A route can now deliver to **People named in the message**. Only an admin may create, edit or
delete such a route. See [Send alerts to individual people](../direct-messages/).
