# Teams app manifest

This is the Teams app package for the bot that delivers alerts to a person's chat
([ADR 0026](../docs/adr/0026-alerts-in-a-persons-chat.md)). It is packaging metadata for the Teams
app catalog — what a person sees when they install the bot — not runtime configuration, which
lives in `config.example.yaml` under `bot:`.

## What to replace before packaging

`manifest.json` ships with placeholders. Replace all of them:

| Placeholder | Replace with |
| --- | --- |
| `id` | A new GUID you generate once for this app, kept stable across versions. |
| `bots[0].botId` | The Microsoft App ID (client ID) of the bot's Entra registration — `bot-client-id` in `config.example.yaml`. |
| `developer.name` | Your organization's name. |
| `developer.websiteUrl` | A URL a person can reach for support. |
| `developer.privacyUrl` | Your privacy statement. |
| `developer.termsOfUseUrl` | Your terms of use. |
| `version` | Bump on every change you upload; Teams rejects a re-upload at the same version. |

Two settings are deliberately not placeholders and must not be changed:

- `bots[0].isNotificationOnly` must stay `false`. A notification-only bot cannot be sent messages,
  so nobody could type a linking code at it — see the ADR for why this bot needs to receive
  messages, not just send them.
- `bots[0].scopes` must stay `["personal"]` only. Adding a group or team scope would let someone
  link the bot in a group chat, which would later broadcast that person's private alerts to
  everyone in it.

## Icons

- `color.png` — 192x192, full color, generated from the same badge used for the admin UI's
  favicon (see `scripts/make-favicons.sh` and `images/favicons/logo-teamster-1/`).
- `outline.png` — 32x32, transparent background, a single white silhouette — required by the
  Teams catalog to render the bot in places that need a monochrome mark.

Regenerate them from a source logo with ImageMagick if the badge changes:

```bash
magick images/favicons/logo-teamster-1/icon-192-maskable.png manifest/color.png
magick images/favicons/logo-teamster-1/favicon-256x256.png \
  -fuzz 15% -transparent "srgb(203,182,247)" \
  -fill white -colorize 100% -resize 32x32 -strip manifest/outline.png
```

The `-transparent` color is the badge's own background fill; sample a corner pixel of
`color.png` again if the badge's background color changes.

## Packaging and uploading

Teams expects a zip containing exactly `manifest.json`, `color.png` and `outline.png` at its root
— no subdirectory:

```bash
cd manifest
zip -j teamster-bot.zip manifest.json color.png outline.png
```

Upload `teamster-bot.zip` through Teams admin center (Teams apps → Manage apps → Upload new app)
for an organization-wide install, or through a Teams client's "Upload a custom app" option for
testing with a single account. Either way the bot registration referenced by `bots[0].botId` must
already exist in Entra and have a messaging endpoint configured before anyone can complete the
one-time linking code the ADR describes — installing the app alone does not link an account.
