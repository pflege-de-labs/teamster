# Teams app manifest

This is the Teams app package for the bot that delivers alerts to Teams channels and to a person's
chat ([ADR 0045](../docs/adr/0045-channel-delivery-through-the-bot.md),
[ADR 0026](../docs/adr/0026-alerts-in-a-persons-chat.md)). It is packaging metadata for the Teams
app catalog — what a person sees when they install the bot — not runtime configuration, which
lives in `config.example.yaml` under `bot:`.

## What to replace before packaging

The tracked `manifest-template.json` holds placeholders. Copy it to `manifest.json` — gitignored,
since it carries your organization's ids — and replace all of them there:

| Placeholder | Replace with |
| --- | --- |
| `id` | A new GUID you generate once for this app, kept stable across versions. |
| `bots[0].botId` | The App ID of the bot registration (see [Registering the bot](#registering-the-bot)) — `bot.client-id` in `config.example.yaml`. |
| `developer.name` | Your organization's name. |
| `developer.websiteUrl` | A URL a person can reach for support. |
| `developer.privacyUrl` | Your privacy statement. |
| `developer.termsOfUseUrl` | Your terms of use. |
| `version` | Bump on every change you upload; Teams rejects a re-upload at the same version. |

Two settings are deliberately not placeholders and must not be changed:

- `bots[0].isNotificationOnly` must stay `false`. A notification-only bot cannot be sent messages,
  so nobody could type a linking code at it — see the ADR for why this bot needs to receive
  messages, not just send them.
- `bots[0].scopes` must stay `["personal", "team"]`. `team` is what lets the bot post to a
  channel, and the bot reads only install and removal events there, never commands. Adding
  `groupChat` would let someone link the bot in a group chat, which would later broadcast that
  person's private alerts to everyone in it.
- `supportsChannelFeatures` must stay `"tier1"`. Teams rejects an upload with a `team` scope bot
  and manifest version 1.25+ that omits it; it opts the app into shared and private channels, not
  anything this bot depends on.

## Icons

- `color.png` — 192x192, full color, the same badge used for the admin UI's favicon (see
  `scripts/make-favicons.sh` and `images/favicons/logo-teamster-1/`). It is already the size and
  format the Teams catalog wants, so it is a plain copy of `icon-192-maskable.png`, not a
  re-encode — running an image tool over an already-192x192 source would only risk producing a
  file that differs from the one committed here for no reason.
- `outline.png` — 32x32, transparent background, a single white silhouette — required by the
  Teams catalog to render the bot in places that need a monochrome mark. This one genuinely is
  generated: it is downsized and recolored from a larger source.

Regenerate them from a source logo if the badge changes. Both commands are repo-root-relative —
run them from the top of the checkout, not from `manifest/`:

```bash
cp images/favicons/logo-teamster-1/icon-192-maskable.png manifest/color.png
magick images/favicons/logo-teamster-1/favicon-256x256.png \
  -fuzz 15% -transparent "srgb(203,182,247)" \
  -fill white -colorize 100% -resize 32x32 -strip manifest/outline.png
```

The `-transparent` color is the badge's own background fill; sample a corner pixel of
`color.png` again if the badge's background color changes.

## Registering the bot

An Entra app registration alone is not a bot. Teams looks `bots[0].botId` up in the Bot Framework,
so the app needs a bot registration under that App ID with the Microsoft Teams channel enabled.
Without one, adding the app to a team fails with "Invalid bot" ("Ungültiger Bot").

The quickest way is the [Teams Developer Portal](https://dev.teams.microsoft.com):

1. Tools → Bot management → New bot. This creates its own Entra app registration and enables the
   Teams channel.
2. Configure → Endpoint address: `https://<external host>/bot/messages`, the path teamster
   receives activities on.
3. Client secrets → Add a client secret. It is shown once.
4. Point teamster at that registration: its App ID is `bot.client-id` and the manifest's
   `bots[0].botId`, the secret goes into `bot.client-secret` (the chart's
   `credentials.botClientSecret`), and `bot.tenant-id` is your tenant.
5. Set `bot.tenant-type` to match the registration's "Supported account types" in Entra:
   `single` for this directory only, `multi` for any directory. A mismatch lets the install
   succeed and then fails every token request or inbound activity.

To reuse an existing app registration instead, create an Azure Bot resource with "Use existing app
registration", set its messaging endpoint to the same URL, and enable the Microsoft Teams channel
under Channels — the Azure resource does not enable it for you.

## Packaging and uploading

Teams expects a zip containing exactly `manifest.json`, `color.png` and `outline.png` at its root
— no subdirectory. From the top of the checkout:

```bash
make manifest
```

builds `manifest/teamster-bot-<version>.zip`, named by the manifest's `version`. It is a build
artifact, not a tracked file — see `.gitignore` — so it is safe to rebuild as often as needed.

Upload the zip through Teams admin center (Teams apps → Manage apps → Upload new app) to publish it
to the organization's catalog, or through a Teams client's "Upload a custom app" option for testing
with a single account. Either way the bot must be [registered](#registering-the-bot) first.
Installing the app alone does not link an account; that takes the one-time linking code the ADR
describes.

## Installing it in a team

Every team a route posts to needs the app installed. An owner of the team adds it from the app's
page in Teams (Apps → Built for your org → the app → Add to a team), and picks any channel. The
bot then receives an install event. Teamster records the team from that event, and from then on
it knows the team's regional Bot Connector endpoint. Removing the app from the team stops
delivery there, and Teamster forgets the team.
