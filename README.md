<p align="center">
  <img src="images/teamster-header.png" alt="" width="500" />
</p>

# Teamster

Go service that accepts Alertmanager or universal webhooks, routes alerts to Teams channels, and
posts Adaptive Cards through Microsoft Graph.

## Quick start

1. Copy config example:

```bash
cp config.example.yaml config.yaml
```

2. Start the server:

```bash
make run                                     # picks up ./config.yaml
go run ./cmd/teamster serve                    # same thing, command named
go run ./cmd/teamster -c /etc/teamster.yaml    # or point at any file
```

`serve` is the default command, so it runs when no command is given. `teamster --help` lists the
others.

The server stops on SIGINT or SIGTERM: it stops accepting connections and drains in-flight
requests for up to `server.shutdown-timeout` (default 15s) before closing the database. A second
signal kills it immediately.

3. Open the admin UI at `http://localhost:8080/admin` (basic auth from config) and configure:

- Templates (Adaptive Card JSON with Go templating)
- Destinations (Team ID, Channel ID)
- Routes (label selector -> destination + template)

## Configuration

Configuration comes from YAML files, environment variables and flags. `teamster --help` lists
every setting.

Config files are searched in this order, following the XDG Base Directory Specification:

1. `$XDG_CONFIG_DIRS/teamster/config.yaml` (default `/etc/xdg/teamster/config.yaml`)
2. `$XDG_CONFIG_HOME/teamster/config.yaml` (default `~/.config/teamster/config.yaml`)
3. `./config.yaml`

Later files override earlier ones. `--config FILE` (`-c`, `$TEAMSTER_CONFIG`) overrides the whole
search, and command line flags override everything. Environment variables named after the flags —
`TEAMSTER_SERVER_ADDR`, `TEAMSTER_GRAPH_CLIENT_SECRET`, and so on — apply only when no config file
sets the value.

YAML keys match the flag names, so nested keys are hyphenated:

```yaml
graph:
  tenant-id: "your-tenant-id"
  client-secret: "your-client-secret"
```

Unknown keys are ignored silently, so a misspelled key shows up as a startup failure in
validation. See [config.example.yaml](config.example.yaml) for the full set.

### Talking to something other than the public Graph

`graph.base-url`, `graph.token-url` and `graph.scope` are the three settings that decide which
Graph this talks to. Leaving `token-url` empty derives
`https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token`, which is what a normal
deployment wants.

Change all three together for a sovereign cloud, or point them at a server you control to exercise
delivery without a Microsoft tenant:

```yaml
graph:
  base-url: "http://127.0.0.1:18500"
  token-url: "http://127.0.0.1:18500/token"
  scope: "http://127.0.0.1:18500/.default"
```

### Sending alerts to a person's chat (optional)

`bot.tenant-id`, `bot.client-id` and `bot.client-secret` configure a second, separate Entra
registration for a Teams bot that can send an alert directly to a person rather than only to a
channel (see [ADR 0026](docs/adr/0026-alerts-in-a-persons-chat.md)). This is unrelated to the
`graph` credential above, which posts to channels: revoking or rotating one never touches the
other.

The whole feature is off by default. Leave all three empty and nothing else in the `bot:` block
matters; set all three together to turn it on, since setting only one is rejected at startup.
`bot.tenant-type` distinguishes a registration that only ever signs in this tenant's users
(`single`, the default) from one registered to accept any tenant's users (`multi`), which
authenticates through a shared Microsoft endpoint rather than this tenant's own. `bot.metadata-url`
defaults to Microsoft's public endpoint and must stay `https`: it is the trust anchor every inbound
activity is checked against, so turning the feature on with it cleared or pointed at plain `http` is
rejected at startup rather than registering a route that would never validate anything. Turning the
feature on also registers `POST /bot/messages`, the endpoint the bot receives Teams activities on,
and `/admin/notifications` and its actions, described below, along with the nav link to them; leaving
it off registers none of that, so an unconfigured deployment exposes nothing new and offers nobody a
page that instructs them to talk to a bot that does not exist. Once a chat is linked, point a route
at that person and alerts arrive there — see the routing notes for how a route addresses a channel
and a person at the same time.

#### Linking your chat

1. Any signed-in person — `viewer` and up — opens **Notifications** (`/admin/notifications`) in the
   admin UI and asks for a code. The same thing is available to a script as `POST
   /api/recipients/link`, which returns the code as JSON:

   ```json
   { "code": "AB3D-EFGH-J2MN", "expires_at": "2026-09-16T10:30:00Z" }
   ```

   Basic auth is refused on both — the API endpoint and the page's own controls — even though it
   works everywhere else in the admin API: it authenticates every script as the same configured admin
   username, and a code has to bind the person who is actually asking, not whoever holds that shared
   password.
2. That person opens a 1:1 chat with the bot in Teams (adding it first if they have not) and sends
   the code, mention markup and all — pasting it after `@`-mentioning the bot works.
3. The bot confirms in the same chat. The code is single-use and expires after ten minutes; an
   expired or already-used one gets a reply that does not say which.
4. An admin edits a route and picks that person in **Person**, beside the destination. A route can
   name a channel, a person, or both; naming both sends one card and one chat message.

Redeeming a code for a subject that is already linked moves the alert stream to the new chat and
tells the *old* chat it was displaced — a code that leaks does not silently steal someone else's
alerts without them finding out.

In the chat, a repeated firing edits the message that is already there and a resolution arrives as a
new message, so the notification that matters is the one saying it cleared.

**Notifications** also shows whether a chat is linked already — the display name captured at link
time (truncated if Teams handed back an implausibly long one — that name is whoever redeemed the
code, not something this person chose), when the link was first established, and when it last
changed, worded to make clear that a service-url refresh updates the same field a new redemption
does, so a change there alone is not proof of a takeover. It never shows the raw Bot Framework
conversation id or Azure AD object id, which are opaque identifiers with no reason to be in a
screenshot or a support ticket. It offers **Unlink**, which removes the caller's own recipient and
stops delivery there, resolved from the signed-in session, not from anything the form carries, so it
can only ever remove the caller's own link. It also offers **Cancel my code**, available whether or
not the caller is currently linked, which invalidates every code outstanding for their subject —
minting a new one already supersedes an old one, but only once the replacement exists, which is no
help if a code was pasted into the wrong window and nobody wants a replacement yet. Every response on
this page is sent `Cache-Control: no-store`, including the page itself: it can show a live code or
who currently holds one, and unlike the URL or an access log, the response body is what a shared
machine's cache or a signed-out browser's Back button would otherwise replay.

Nothing about this endpoint is authenticated by teamster's own webhook token, admin password or
session cookie: `POST /bot/messages` is public, and every request on it is authenticated entirely
by **Microsoft's** own signature, checked against the Bot Framework's published metadata document
(`bot.metadata-url`) per its [authentication spec][bot-auth-spec]. Issuer, audience, signature,
expiry, the activity's `serviceUrl` claim and the channel the signing key is endorsed for are all
checked before anything in the request body is acted on.

[bot-auth-spec]: https://learn.microsoft.com/en-us/azure/bot-service/rest-api/bot-framework-rest-connector-authentication

#### Leaving

Three ways out, and the two new ones need no admin UI at all:

- **Send `unlink` to the bot** — or `stop`, or `unsubscribe`. The message has to *be* the word:
  "how do I unlink this chat?" is a question, not a command, and is treated as one. The bot
  confirms, and a new link code reconnects whenever you want it back.
- **Uninstall the bot.** Teams reports the removal and the link retires itself, so alerts stop
  rather than piling up as permanent send failures.
- **Unlink in the admin UI** — your own chat from **Notifications**, or anyone's from
  **Recipients** if you administer them.

Whichever way, the recipient row and any alert cards still tracked for it go together.

#### Managing recipients

**Recipients** (`/admin/recipients`) lists everybody who has linked a chat: their display name (or
subject, when Teams gave no name), when they linked, and which routes deliver to them by name — so
an admin about to unlink somebody can see what stops arriving before doing it, not after. Viewing
needs the `viewer` role and up; unlinking needs `editor` and up, the same split every other admin
list uses, and the button is hidden from a viewer rather than merely refused. Unlinking removes the
recipient even if a route still names them — the same as deleting a destination — and a route left
pointing at nobody shows up as a "missing recipient" in `/admin/routing` rather than failing
silently.

A permanent send failure — the person uninstalled or blocked the bot — is recorded on the row as
**blocked**, with the reason and when, shown plainly rather than as an icon to hover over. This flag
is informational only: it never stops the next alert from being attempted, and a successful send or
update clears it automatically. Treating it as a switch that turns delivery off would trade a
visible problem (this flag, and the metric behind it) for an invisible one — a person who
reinstalled the bot would otherwise receive nothing again until an admin happened to notice and
clear it by hand. `GET /api/recipients` and `DELETE /api/recipients/{id}` are the same list and
unlink action over the API; the response omits the Bot Framework conversation reference, since
nothing an admin does with this API needs it.

Turning this on also needs a Teams app package: [`manifest/`](manifest/) holds the `manifest.json`
and icons an operator uploads to Teams admin center so the bot can be installed at all, separate
from the runtime configuration above. See [`manifest/README.md`](manifest/README.md) for what to
replace before packaging and how to build the zip.

## Storage

| `database.driver` | What it is |
| --- | --- |
| `sqlite` | One file, no other runtime dependency, and exactly one instance: SQLite takes a single writer. The default. |
| `postgres` | Several instances sharing one database. |

Postgres is configured with discrete settings rather than a connection string, because a config
file value beats an environment variable — so a URL in the file would take the password with it:

```yaml
database:
  driver: postgres
  postgres:
    host: pg.internal
    dbname: teamster
    user: teamster
    sslmode: require
```

The password is the fifth value that must stay out of the config file, alongside the webhook token,
the admin password and the two client secrets. Set `TEAMSTER_DATABASE_POSTGRES_PASSWORD`.

`sslmode` defaults to `require`. `verify-ca` and `verify-full` check the server against
`database.postgres.sslrootcert`, which a managed Postgres usually needs, because the container
image carries the public roots and not the provider's own.

### Moving from SQLite to Postgres

Take a bundle with `teamster export` (or `GET /api/config/export`), point the configuration at
Postgres, run `teamster migrate up`, and import it. **The bundle carries configuration, not runtime
state**: sessions, login flows and active alerts stay behind. So everyone signs in again, and any
alert firing at the moment you cut over has a card in Teams the new instance has never heard of —
the next `firing` posts a second one, and the eventual `resolved` never edits the first. Resolve
what you can first.

## Shell completion

```bash
teamster completion bash > /etc/bash_completion.d/teamster
teamster completion zsh  > "${fpath[1]}/_teamster"
teamster completion fish > ~/.config/fish/completions/teamster.fish
```

The script is generated from the command tree itself, so it knows every command,
every flag, and the values an enum flag accepts — `--database-driver <TAB>`
offers `sqlite` and `postgres`. Regenerate it after upgrading and it picks up
whatever the new release added.

One thing to know about bash: because every flag here also reads an environment
variable, an enum flag completes to that variable's current value instead of to
the values the flag accepts — nothing when it is unset, and whatever it says
when it is set, valid or not. Commands and flags complete normally, and zsh and
fish offer the real values. Reported upstream.

## Schema migrations

The schema is versioned. Starting the server applies whatever is missing, which is what a single
instance wants and what every earlier release did implicitly, so upgrading needs nothing extra.

To make it a step of its own instead:

```bash
teamster migrate status      # what has been applied, and what has not
teamster migrate up          # apply everything pending
teamster migrate down        # roll back one migration
```

`database.migrate` decides what the server does when it opens a database that is behind:

| Value | What happens |
| --- | --- |
| `auto` | Apply the missing migrations. The default. |
| `verify` | Refuse to start, naming `teamster migrate up`. |
| `off` | Open it as it is. |

Set `verify` when a schema change should be something you watch — a deployment that runs
`teamster migrate up` as its own step before rolling out the new version, or an installation whose
database user is not allowed to change the schema.

`teamster export` never migrates, whatever the setting: taking a backup must not be the thing that
changes an installation.

## Webhooks

### Alertmanager (0.31)

`POST /webhook/alertmanager` with `X-Teamster-Token` header.

### Universal webhook

`POST /webhook/universal` with `X-Teamster-Token` header. Routes on `labels` and renders
`annotations` exactly like an Alertmanager alert does — `status` is what's optional. A `status` of
`firing` or `resolved` opts into the tracked alert lifecycle: a repeat post with the same
`fingerprint` edits the card in place, and `resolved` clears it. Any other `status` — including
none at all, the shape for a sender with no such lifecycle — is delivered once and tracked
nowhere, the same fire-and-forget contract the Teams V2 webhook below has, just routed and
templated first.

### What the webhook token protects

The webhook endpoints are authenticated by one shared token and nothing else. It is worth being
concrete about what somebody holding it can do, because it is more than "file a spurious alert".

Alert labels and annotations are interpolated into templates, and the rendered text reaches a Teams
channel or a person's chat. The sanitizer bounds that: the markup allowlist is closed, `script` and
`style` are dropped with their contents, every attribute except a link's `href` is discarded, and an
`href` survives only for `http`, `https` and `mailto`. No scripts, no images, no pixel trackers, no
`javascript:`.

What it does **not** bound is links themselves. Message text is Markdown, so an annotation
containing `[Open the runbook](https://evil.example/login)` renders as exactly that: a link whose
visible text says one thing and whose destination is another, delivered by a service your people
trust, in a channel or a 1:1 chat they are on call for. That is a workable phishing setup, and
three-in-the-morning alert traffic is close to the worst context in which to ask somebody to check a
URL before clicking.

Links are deliberately kept — templates link to runbooks and dashboards, which is most of what
message text is for — so the control is the token, not the allowlist:

- **Treat the token as a credential, not a formality.** It is the whole boundary. Rotate it by
  changing the config and restarting.
- **Do not expose the webhook endpoints to the internet** if only in-cluster senders need them.
  Alertmanager posting from inside the same cluster needs no ingress at all.
- **Give separate senders separate deployments** if they belong to different trust boundaries. One
  token means one boundary.
- **Terminate TLS in front of Teamster.** The token travels in a header on every request.
- A refused token is counted (`teamster.webhook.receipts`, status `refused`), so a token being
  guessed at is visible rather than silent. Alert on it.

The same reasoning applies to anyone who can edit templates, who can of course write whatever link
they like — but that is an authenticated admin action, scoped by permission grants, and a far
smaller group than "whatever can reach the webhook port".

Payload shape, for an alert with a lifecycle:

```json
{
  "status": "firing",
  "labels": {"alertname": "HighCPU", "severity": "critical"},
  "annotations": {"summary": "CPU spiking"},
  "starts_at": "2025-12-07T20:07:00Z",
  "ends_at": "0001-01-01T00:00:00Z",
  "generator": "custom",
  "fingerprint": "optional-stable-id",
  "title": "optional, sent as-is when the route has no template",
  "text": "optional, Markdown, sanitized the same way a template's text is",
  "card": {"optional": "Adaptive Card JSON, sent as-is when the route has no template"}
}
```

`status`, `starts_at`, `ends_at`, `generator` and `fingerprint` are all optional. A general
message needs only `labels` and `annotations`:

```json
{
  "labels": {"app": "checkout", "environment": "production"},
  "annotations": {"summary": "Deployment finished"}
}
```

See [samples/universal-message.json](samples/universal-message.json) alongside
[samples/universal-firing.json](samples/universal-firing.json) and
[samples/universal-resolved.json](samples/universal-resolved.json).

`title`, `text` and `card` matter only for a route with no `TemplateID`: a route that has one
renders through it exactly as before, and these three fields are ignored for that delivery. A
route with no template sends them directly instead. This is what lets a sender that already knows
what it wants to say skip writing a template. A message that has no template and carries none of the
three still goes out, with teamster's built-in default: a title taken from `summary` or
`alertname`, the status and description, and the alert as a JSON block. Every message sent without
a template is followed by a small card saying so. Set `server.external-url` to the address people
use for the admin UI, and that card links straight to where templates are created. See
[samples/universal-direct-message.json](samples/universal-direct-message.json) and
[ADR 0036](docs/adr/0036-direct-content-when-a-route-has-no-template.md) and
[ADR 0039](docs/adr/0039-built-in-default-message.md).

### Teams V2 (Power Automate) webhook

For migrating a sender that already posts to a Microsoft Teams "Workflows" webhook. It accepts the
payloads that webhook accepts, so moving a sender is changing one URL rather than rewriting it.

`POST /teamsv2/{team}/{channel}/{token}` — no header, because the URL is the credential, exactly as
it was before.

Create the endpoint under **Teams V2 webhooks** on `/admin`: pick the destination to post into and
give the URL two readable segments (lower case letters, digits and hyphens). The full URL, token
included, is shown once on the page that answers the form. Only a digest of the token is stored, so
there is no way to show it again — if it is lost, or leaks, use **New token**, which replaces it and
stops the old URL working.

Three bodies are accepted, and the shape is recognised from the body itself:

```json
{
  "type": "message",
  "attachments": [
    {
      "contentType": "application/vnd.microsoft.card.adaptive",
      "content": {"type": "AdaptiveCard", "version": "1.4", "body": []}
    }
  ]
}
```

```json
{"text": "something broke on node-3"}
```

```json
{
  "@type": "MessageCard",
  "themeColor": "D70000",
  "title": "Disk almost full",
  "text": "node-3 is at 94%",
  "sections": [{"facts": [{"name": "severity", "value": "critical"}]}],
  "potentialAction": [
    {"@type": "OpenUri", "name": "Open runbook", "targets": [{"os": "default", "uri": "https://example.test/runbook"}]}
  ]
}
```

An Adaptive Card is forwarded to Teams byte for byte. Text is sanitized against the same allow-list
the templates use.

A MessageCard is converted into one Adaptive Card, and loses two things in the process. Actions
other than `OpenUri` — `HttpPOST`, `ActionCard`, `InvokeAddInCommand` — are dropped, because each
needs the connector to call the sender back and nothing here can. `themeColor` becomes one of the
five container styles Adaptive Cards has, chosen by hue: red reads as `attention`, orange and
yellow as `warning`, green as `good`, blue and purple as `accent`, and a grey gets none.

By default the payload is posted as it came, with a small card after it saying that no template is
defined. You can instead pick a **Template** for the endpoint in its form. The template then shapes
the message:

- `.Alert.Title`, `.Alert.Text` and `.Alert.Card` hold the parsed message, and `.Alert.Source` is
  `teamsv2`. `Text` is already HTML.
- `.Payload` is the body as it was sent, for fields the parsed form flattens, such as
  `{{ .Payload.themeColor }}` or `{{ range .Payload.sections }}`.

The template preview has a **Teams V2 webhook** sample to try this against.

Nothing is tracked afterwards. There is no status and no fingerprint in these payloads, so a message
sent this way is never updated or resolved — unlike an alert, which keeps its card up to date.

Responses: `200` when the message was posted, `404` for a team and channel nobody configured, `401`
for a wrong token, `400` for a body that carries neither text nor a card, `413` for a body over
128 KiB, and `502` when Teams or the database fails, or the endpoint's template does not render.

## Templates

A template is three optional parts, and needs at least one of them:

| Part | What it is | Where it shows |
| --- | --- | --- |
| Title | A template rendering to one line of plain text | The Teams activity feed preview |
| Message text | A template rendering to formatted text | The message body |
| Adaptive Card JSON | The card, as before | Below the text |

A message that is only a card previews in the activity feed as `Card`, which is why the title
exists. A card without a title keeps the summary line this service always sent —
`annotations.summary`, then `labels.alertname`, then `Alert update` — so existing templates are
unaffected. Text without a title is left alone: the feed previews the text itself.

Message text is **Markdown**. It is rendered to HTML and sanitized before it is sent: `p`, `br`,
`b`, `strong`, `i`, `em`, `u`, `s`, `code`, `pre`, `blockquote`, `ul`, `ol`, `li`, `h1`–`h3` and `a`
survive, `script` and `style` are dropped with their contents, anything else is unwrapped to its
text, and a link keeps its `href` only for `http`, `https` and `mailto`.

Raw HTML passes through the Markdown parser, so **a template written as HTML keeps working** and
needs no migration — that is what the passthrough is for. The one visible difference is that a
fragment which is only inline markup, like `<b>bold</b>` with no block element around it, becomes
the paragraph it always implied.

The same sanitized text reaches both transports: a Team channel gets the HTML, and a person's chat
gets Markdown emitted from that sanitized HTML rather than from the template source. One sanitizer
covers both.

An alert annotation ends up in that text, so it cannot be trusted to be markup-free. Sanitizing
bounds what it can do — no scripts, no images, no `javascript:` or `data:` links — but it does not
make alert data inert. Anyone who can POST a webhook can put a **link** in a message, with link text
that need not match where it leads. See [what the webhook token protects](#what-the-webhook-token-protects).

## Template data

Templates receive:

- `Alert` (normalized alert payload)
- `Now` (RFC3339 string)

Helper functions:

- `toJSON` to JSON-encode structures
- `default` to provide fallbacks, including for a key an alert did not set

## Editor completion

The template fields, a route's label selector and the label box on `/admin/routing` are code
editors with completion. Inside `{{ … }}` they offer the template data (`.Alert.Status`,
`.Alert.Labels`, `.Now`, …), the functions above and text/template's own, and the actions (`if`,
`range`, `end`, …). After `.Alert.Labels.` or `.Alert.Annotations.` they offer the keys that recent
alerts actually carried; a key that cannot follow a dot, such as `app.kubernetes.io/name`, is
inserted as `(index .Alert.Labels "app.kubernetes.io/name")`. In the card they offer Adaptive Card
element types, property names and enum values, taken from the same renderer the preview uses. A
label selector, and the routing check, offer label keys and then the recent values of the key
chosen. `Ctrl-Space` asks for completion anywhere. Without JavaScript the fields are plain text
boxes, as before.

The label keys and values come from the alerts this service receives: every Alertmanager and
universal alert is sampled, whether or not a route matches it, and the samples are kept in the
`alert_samples` table. **Annotation values are never stored** — only annotation keys — because
they are free text that may carry detail nobody asked this service to keep. Label values are stored,
which is why only a role that may edit templates or routes can read them, at `GET /api/samples`.

```yaml
samples:
  enabled: true              # sample incoming alerts at all
  retention: "720h"          # forget a key or value not seen for this long
  max-values-per-key: 50     # keep only the most recently seen values of each label key
  max-value-length: 200      # do not sample a longer label value
  lru-size: 4096             # tuples held in memory to coalesce writes
  flush-interval: "5m"       # write a tuple already stored at most this often
```

Sampling never delays a delivery: alerts are handed to a background writer that drops them if it
falls behind, and a label every alert carries is written once per `flush-interval` rather than once
per alert. Counts are therefore approximate. Replicas sharing Postgres add to the same rows. Setting
`enabled: false` stops sampling and serving; rows kept before stay in the table until deleted.

## Notes

- Route selection matches label selectors exactly; highest priority wins.
- Routes nest. A child refines its parent's match and sends the alert somewhere else — *as well as*
  its parent, or *instead of* it when marked greedy. A child that names no destination, person or
  template inherits the nearest ancestor's, so "the same card, one more channel" is a one-field
  route. A greedy child takes *both* of its parent's deliveries with it, not just the one it shares
  a kind with.
- A route has two targets and they are independent, not alternatives: a destination for a Team
  channel and, optionally, a person for a chat. A route naming both sends both — one card and one
  chat message, each claimed and tracked on its own, so one failing does not cost the other its
  message. A route naming neither delivers nothing.
- Delivering to a person needs a linked chat and a configured bot; see the recipient section. Any
  admin or editor who may edit routes may point one at any linked person — grants scope Teams and
  channels, and a person is neither.
- A route with children cannot be deleted; remove or reparent them first, because an orphan becomes
  a root that matches alerts its parent used to filter out.
- A database file written before the `DATETIME` timestamp fix cannot be read. Migrating it fails
  and names the file; delete it and start again to recreate the schema.
- A default route is used if no labels match.
- Where no route matches and there is no default route, the message goes to the **global default
  destination**. The first destination you create becomes the global default, and an admin can
  make another one the default from the Destinations list. Only one destination is the default at
  a time. It cannot be deleted while other destinations remain, so switch the default first. The
  route list and the routing page show it as a built-in route that cannot be edited.
- Active alerts are tracked per channel and per person, so an alert that fans out updates and
  resolves every message it sent. One target failing does not stop the others; the response is a
  `502` and the sender's retry updates what already landed rather than duplicating it.
- In a chat, a repeated `firing` edits the message in place, and a `resolved` sends a new one.
  Teams does not re-notify on an edit, so resolving in place would leave whoever is on call never
  told that it cleared.
- If somebody uninstalls or blocks the bot, that delivery is counted as `blocked` rather than
  `failed` and stops being retried for that alert. The next alert tries again.
- The right to post a card is claimed before the card is posted, so two deliveries of the same
  alert to the same channel produce one card rather than two. The one that loses gets a `502`, and
  its retry edits the card the winner made. A claim left behind by a process that died is taken
  over by the next attempt after `max(30s, 3 x graph.timeout-sec)`.
- A database written before templates had a title gains the columns the first time a newer build
  migrates it; nothing needs to be deleted.

## Sample payloads

See the JSON examples in [samples](samples):

- [samples/alertmanager-firing.json](samples/alertmanager-firing.json)
- [samples/alertmanager-resolved.json](samples/alertmanager-resolved.json)
- [samples/universal-firing.json](samples/universal-firing.json)
- [samples/universal-resolved.json](samples/universal-resolved.json)
- [samples/universal-message.json](samples/universal-message.json)
- [samples/universal-direct-message.json](samples/universal-direct-message.json) — direct
  content, for a route with no template
- [samples/teamsv2-card.json](samples/teamsv2-card.json)
- [samples/teamsv2-text.json](samples/teamsv2-text.json)
- [samples/teamsv2-messagecard.json](samples/teamsv2-messagecard.json)

## Signing in

The admin UI needs a session. Configure either or both:

- **An OIDC provider.** Point `auth.oidc-discovery-url` at the provider's
  `/.well-known/openid-configuration`, and set `auth.oidc-client-id` and `auth.oidc-redirect-url`,
  then name the claim that carries the roles. For Keycloak that is usually `realm_access.roles` for
  a realm role, or `resource_access.<client>.roles` for a client role. A public client using PKCE
  needs no secret.
- **Local credentials.** `admin.username` and `admin.password` are accepted at `/admin/login`.

The claim is looked for in the ID token, then at the userinfo endpoint, then in the access token,
and the first hit wins. Keycloak's built-in role mappers populate the **access token** and leave
roles out of the ID token, so a realm needs no mapper changes — and if you would rather the roles
travel in the ID token, they are used in preference. [Configuring Keycloak](docs/keycloak.md) covers
the client, the role and the mapper.

Signing in is no longer the place access is decided — the role is, and a user whose claim names none
gets `auth.default-role` or, with none configured, nothing at all. Whether everyone in the realm can
reach this client is therefore the provider's decision to make; leaving `auth.default-role` empty is
the safe setting when they can. Keep the local credentials configured either way: they are the way
back in if the provider is unreachable or the claim is wrong.

`/api` accepts either a session or those same credentials as HTTP basic auth, so existing automation
keeps working and the admin UI's own fetches work for a session that never saw the local password.

### Delegated Teams/Channels (optional)

When Keycloak brokers the OIDC login above against Microsoft Entra as an upstream identity
provider, `auth.broker.enabled` lets the Destinations picker offer each admin their own Teams and
channels alongside the tenant-wide list, using the Entra token Keycloak already stored for that
login rather than a delegated Entra registration of Teamster's own — see
[ADR 0037](docs/adr/0037-delegated-teams-via-keycloak-broker-token.md).

```yaml
auth:
  broker:
    enabled: true
    idp-alias: "microsoft"                 # the Entra identity provider link's alias in Keycloak
    token-encryption-key: "<32 random bytes, base64>"   # e.g. openssl rand -base64 32
```

It is off by default and needs `auth.oidc-discovery-url` configured; a local login, or an OIDC login
through a realm with no Entra federation, never sees the "My Teams" toggle regardless. Setting up
the Keycloak side — Store Tokens, Stored Tokens Readable, the delegated Entra scopes — is covered in
[Configuring Keycloak](docs/keycloak.md#delegated-teams-and-channels).

## Roles

Three roles, in order: `admin`, `editor`, `viewer`.

| Role | May |
| --- | --- |
| `admin` | everything, including whatever later releases add |
| `editor` | read the configuration and change it |
| `viewer` | read it, and nothing else, plus [link or unlink their own chat](#linking-your-chat) |

**Every role in the claim is mapped one to one, by name.** A client or realm role called `admin`,
`editor` or `viewer` **is** that role here, and one called `auditor` arrives as `auditor`. There is
nothing to configure in between, and a user carrying several holds all of them.

**A role only means something if a policy says so.** The rules are Cedar policies in
[`internal/authz/policies.cedar`](internal/authz/policies.cedar), embedded in the binary. A role no
policy mentions grants nothing — which is what makes passing every claim value through safe, and
what you have to change to make a role of your own useful: define `auditor` in the provider, then add
a policy for `Role::"auditor"`.

**`admin`, `editor` and `viewer` are the required minimum.** Those three are what the shipped
policies define, what the admin UI asks about when it decides whether to show a control, and what
`auth.default-role` may be set to. Removing or renaming them in the policy file breaks the UI;
adding to them does not.

### Limiting a role to Teams and channels

An admin can narrow where a role may deliver on **Permissions** (`/admin/permissions`): pick a role,
then tick Teams and channels in a collapsible tree, with one toggle at the top for all of them.
Ticking a Team grants its channels, including ones added later; ticking channels individually grants
only those. Saving replaces what that role had, in one transaction.

`/api/grants` still works for scripts, and `PUT /api/grants/role` is what the page posts. A grant
names a role and a Team, optionally one channel of it:

| Grant | Reaches |
| --- | --- |
| `editor` → team `platform` | every channel of that Team |
| `editor` → team `platform`, channel `alerts` | that channel only |

**A role with no grant reaches everything.** Narrowing starts the moment a grant names that role;
from then on it reaches what its grants name and nothing else. An installation that has never added
a grant behaves exactly as it did before they existed.

Because roles map 1:1 from the provider, "the payments editors" is a role there and a grant here —
no separate notion of a group. Admins are never limited. Grants apply to what a session may **see**
as well as where it may deliver: the Team and channel pickers offer only what is granted, and a
destination outside them is absent from the lists and the API. A write naming a channel the picker
would not have offered is refused with a `403`, because hiding a control is a courtesy and the check
is the control. Pointing a route at a destination outside the grants is refused the same way: a
route is how an alert actually reaches a channel.

`auth.default-role` is what someone gets when the claim names none of those three. Set it to
`viewer` and everyone the provider authenticates may read the configuration; leave it empty and they
sign in with no access and are shown a page telling them to ask an administrator for a role. Roles
of your own ride along beside the default either way — holding `auditor` does not count as holding a
Teamster role. The local credentials administer, because they are the way back in when the provider
is wrong.

A role is decided at sign-in and travels with the session, so a change at the provider applies the
next time that person signs in. The header names who is signed in and the roles they hold, so a
missing control has a visible reason. The admin UI hides the controls a role may not use and says why;
the server refuses the request either way. A refusal is a `403` naming the role and the resource.

The rules are three policies in
[`internal/authz/policies.cedar`](internal/authz/policies.cedar), evaluated in-process by
[Cedar](https://www.cedarpolicy.com/) — see [ADR 0012](docs/adr/0012-role-based-authorization.md).

## Routing visualization

`/admin/routing` draws the path an alert takes — webhook, the routes in the order they are
evaluated with child routes hanging off their parents, then the channels they deliver to — and
answers "which route would this alert take?": paste `key=value` labels and every route that
delivers is named, explained, and the paths are picked out in colour while the rest dims.

Route nodes show the labels they filter for and name the template they render with, marked when it
is inherited. A dashed arrow between two routes is a refinement, labelled *as well as* or *instead
of* depending on whether the child is greedy. A second graph below pairs templates with the routes
that use them; a template with nothing beside it is used by no route. A route pointing at a deleted
destination — or a deleted person — shows up as a missing node rather than disappearing. A route
that delivers to both draws an arrow to each.

## Backup and migration

The configuration — templates, destinations, routes and permission grants — moves as one JSON
bundle. It carries **no credentials** and no runtime state, so it can live in a repository beside
the rest of a deployment's configuration.

Linked people are **not** carried: a link binds one person to one conversation in one tenant, so it
would mean nothing where the bundle lands. A bundle whose route names a person is refused on import
rather than imported as a route that looks like it delivers there and never does — clear the route's
person before exporting.

```bash
teamster export -o teamster.json          # reads the database directly, server or no server
teamster import teamster.json             # merge: upsert what the bundle carries
teamster import teamster.json --mode replace --dry-run
```

`merge` leaves anything the bundle does not mention alone; `replace` makes the installation match
the bundle. Both validate the whole bundle first and apply it in one transaction, so a bundle that
will not apply changes nothing. `--dry-run` reports the diff by running the import and rolling it
back, so the preview is of the real thing.

The same over HTTP, for an admin session:

```bash
curl -u admin:pw http://localhost:8080/api/config/export > teamster.json
curl -u admin:pw -X POST --data-binary @teamster.json   'http://localhost:8080/api/config/import?mode=replace&dry-run=true'
```

Ids are preserved, so re-importing a bundle where it came from is a no-op. Team and channel ids
belong to one tenant, though: a bundle carried to another imports cleanly and then delivers nowhere.
The bundle therefore carries the Team and channel names beside the ids, and the HTTP import names
the destinations this tenant cannot resolve — see
[ADR 0013](docs/adr/0013-configuration-transfer.md).

## Metrics

Off by default. Turn them on and the service exports what it is doing — over OTLP, to a Prometheus
scrape, or both:

```yaml
metrics:
  enabled: true
  addr: "127.0.0.1:9090"     # its own listener, unauthenticated, loopback by default
  path: "/metrics"
  prometheus: true
  otlp-endpoint: ""          # host:port for grpc, a URL host for http; empty runs no OTLP
  otlp-protocol: "http"
  otlp-interval: "60s"
```

| Metric | Says |
| --- | --- |
| `http.server.request.duration` | how long this service took to answer, by route and status |
| `http.client.request.duration` | how long Microsoft Graph took, by host and status |
| `teamster.deliveries` | messages delivered, by route and outcome — posted, updated, failed |
| `teamster.webhook.receipts` | alerts received, by source and status, including refused tokens |
| `teamster.render.failures` | alerts that never became a message, by template and stage |
| `teamster.active_alerts` | cards currently tracked, one per alert per channel |

`teamster.active_alerts` counts rows in the database when it is collected. Because the listener takes
no credentials, the answer is cached for a second, so scraping in a loop is not querying in a loop.

Go runtime metrics come with them, under OpenTelemetry's names (`go_memory_used_bytes`) rather than
the `go_memstats_*` an older dashboard may expect.

The listener is separate from the one serving the admin UI, is not authenticated, and defaults to
loopback: the attributes name routes, templates and channels. Route it to your collector
deliberately.

### Native histograms, and the scrape that silently loses them

The Prometheus exporter emits **native** histograms. Prometheus has to negotiate protobuf to receive
them:

```bash
prometheus --enable-feature=native-histograms
```

Without that flag the scrape does not fail — it degrades. A native histogram has no classic buckets,
so the text exposition carries only the synthetic `+Inf` bucket, the sum and the count. Rates and
averages keep working, every `histogram_quantile` returns `NaN`, and the dashboard renders while
saying nothing. This alert fires exactly when that has happened:

```promql
count without(le) (http_server_request_duration_seconds_bucket) == 1
```

To check by hand, compare the two shapes:

```bash
curl -s localhost:9090/metrics | grep -c '_bucket'
curl -s -H 'Accept: application/vnd.google.protobuf;proto=io.prometheus.client.MetricFamily;encoding=delimited'   localhost:9090/metrics | wc -c
```

## Container

```bash
make image                       # builds teamster:<version>
```

CI publishes images to `ghcr.io/pflege-de-labs/teamster`:

| Tag | Points at |
| --- | --- |
| `latest` | the newest stable release |
| `1.2.3`, `1.2`, `1` | that release |
| `<short-sha>` | the build of that commit, never moves |
| `main` | the newest build of `main` |
| `pr-<n>` | the newest build of that pull request |

A release rebuilds from its tag, so the release tags share one digest of their own and the binary
inside reports the version rather than a commit sha. Use a `<short-sha>` tag to deploy an exact
CI build.

Every `main` build and every release is scanned with Trivy and the findings are reported to
SecObserve; pull requests are not. A release is tracked as its own branch there, named after the
tag, so its findings stay attributed to that release rather than blurred into `main`'s ongoing
history — see [ADR 0024](docs/adr/0024-trivy-image-scanning.md).

### Verifying a release

Images are signed with cosign using the release workflow's own identity — there is no public key
to distribute:

```bash
cosign verify \
  --certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  ghcr.io/pflege-de-labs/teamster:1.2.3
```

The image carries an SPDX SBOM and SLSA provenance as attestations:

```bash
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:1.2.3 --format '{{ json .SBOM }}'
docker buildx imagetools inspect ghcr.io/pflege-de-labs/teamster:1.2.3 --format '{{ json .Provenance }}'
```

Release binaries come with an SPDX SBOM each and a signed `checksums.txt`, which covers the
binaries and their SBOMs:

```bash
cosign verify-blob \
  --bundle checksums.txt.bundle \
  --certificate-identity-regexp '^https://github.com/pflege-de-labs/teamster/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  checksums.txt
sha256sum -c checksums.txt
```

Run it with the configuration mounted at the system-wide XDG location the binary searches, and
the database on a volume:

```bash
docker run --rm -p 8080:8080 --read-only \
  -v "$PWD/config.yaml:/etc/xdg/teamster/config.yaml:ro" \
  -v teamster-data:/data \
  teamster:latest
```

`server.external-url` is the address people reach the admin UI at, for example
`https://teamster.example.com`. Messages sent without a template link there, and it must be an
absolute `http` or `https` URL.

Connections are bounded by `server.read-timeout`, `server.write-timeout` and
`server.idle-timeout`. If you raise `graph.timeout-sec`, raise the write timeout past it, or a
handler waiting on Microsoft Graph is cut off first.

### Probes

| Path | Answers |
| --- | --- |
| `GET /healthz` | the process is running |
| `GET /readyz` | it can serve: not shutting down, and the database answers |

Both are unauthenticated, because a probe carries no credentials, and both answer `HEAD` as well as
`GET`. Liveness deliberately ignores the database: restarting the process does not bring one back,
it only turns an outage into a crash loop. Readiness deliberately ignores Microsoft Graph, which is
somebody else's service — taking the instance out of rotation when Graph is unreachable would close
the admin UI exactly when an operator wants to see why delivery is failing.

Readiness starts failing as soon as a shutdown begins, so a rolling update stops traffic arriving
before the process stops accepting it.

```yaml
livenessProbe:
  httpGet: { path: /healthz, port: http }
readinessProbe:
  httpGet: { path: /readyz, port: http }
```

`--read-only` works because `/data` is the only path the service writes; see
[SECURITY.md](SECURITY.md) for the rest of the deployment expectations.

`make image-run` does exactly that. Settings can also come from `TEAMSTER_*` variables instead of
a mounted file, though a mounted file wins over them.

The image runs as uid 65532 with no shell or package manager, so there is nothing to exec into;
diagnose through the container logs. `/data` is the only writable path, and
`TEAMSTER_DATABASE_PATH` already points the database at it.

## Kubernetes

The [Helm chart](charts/teamster) deploys the image, published as an OCI artifact:

```bash
helm install teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --create-namespace \
  --set credentials.webhookToken=... \
  --set credentials.adminPassword=... \
  --set credentials.graphClientSecret=... \
  --set config.settings.graph.tenant-id=... \
  --set config.settings.graph.client-id=...
```

It runs a single-replica StatefulSet with the SQLite file on its own volume, mounts the config as
a Secret at `/etc/xdg/teamster/config.yaml` and injects the credentials as `TEAMSTER_*` variables
from a second secret — `credentials.existingSecret` points at one the cluster manages instead.
`ingress.enabled` and `httpRoute.enabled` publish the service through an Ingress or a Gateway API
HTTPRoute, and `extraObjects` carries arbitrary resources alongside the release.

There is no multi-replica mode: teamster keeps its state in SQLite, which takes a single writer.
See the [chart README](charts/teamster/README.md) for the full set of values. Metrics there are a values
key and an optional ServiceMonitor; the chart publishes the port and refuses a loopback address.
For more than one replica, point it at a Postgres:

```bash
helm upgrade teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --reuse-values \
  --set database.driver=postgres \
  --set database.postgres.host=teamster-pg-rw \
  --set database.postgres.passwordFrom.secretName=teamster-pg-app \
  --set replicaCount=3
```

The chart deploys no Postgres of its own — a single-pod database bundled into the release would be
less available than the StatefulSet it replaced. It reads the secret your Postgres operator or
cloud provider already made. See the [chart README](charts/teamster/README.md) for the full set of
values.

## Development

```bash
make hooks          # install the git hooks, once per clone
make generate       # regenerate the queries, the templ components and the stylesheet
make icons          # copy the favicon set the binary serves from images/
make vendor         # verify the browser libraries under web/vendor against manifest.json
make vendor-record  # re-record a checksum after bumping a version in manifest.json
make test           # go test ./...
make coverage       # coverage report, fails below 75%
make coverage-html  # writes coverage.html
make lint           # golangci-lint
make build          # builds bin/teamster
```

Contributions must meet the definition of done in [AGENTS.md](AGENTS.md): 75% coverage, clean
lint, and current documentation.

The hooks format Go with the same settings CI checks, lint Markdown, and reject commit messages
that are not [Conventional Commits](https://www.conventionalcommits.org). They need
[pre-commit](https://pre-commit.com) on your PATH.

Destinations are configured by picking a Team and a channel by name once Microsoft Graph answers;
the fields fall back to accepting ids typed by hand. Listing requires the `Team.ReadBasic.All` and
`Channel.ReadBasic.All` application permissions with tenant admin consent.

The template form has a card palette and a preview. The palette inserts the elements our own
templates use — text, facts, columns, a link action, all labels, a conditional block — at the cursor,
and **Start from an example** replaces the card with a complete one to edit down. The preview renders
on the server against a sample alert and the browser draws the result, as you type or on demand, so
a template can be checked before any alert arrives.

The fragments are defined in `internal/cards` and rendered by a test against a sample alert and an
empty one, so the palette cannot offer something the renderer rejects. They also show the quoting
idiom worth copying: write `{{ toJSON .Alert.Annotations.summary }}` with no surrounding quotes
rather than `"{{ .Alert.Annotations.summary }}"`, because `toJSON` adds the quotes and escapes what
is inside them — an alert containing a quotation mark then produces a card instead of broken JSON.
The renderer, and the CodeMirror modules behind the editors, are vendored in
`internal/httpserver/web/vendor`, pinned by version and checksum in that directory's
`manifest.json`; `make vendor` refreshes them and `make vendor-record` re-records a
checksum after a version bump, see the README there.

The favicon set is generated from a logo with `scripts/make-favicons.sh` into
`images/favicons/<logo>/`, and the binary serves copies of it under
`internal/httpserver/web/`, because `go:embed` reads only from its own package directory.
`make icons` refreshes those copies from `images/favicons/logo-teamster-1` — the admin UI wears logo
1, the README header logo 2 — and a test fails when the two drift apart, which is how they came to
be a release behind in the first place.

The admin UI is rendered from [templ](https://github.com/a-h/templ) components in
`internal/httpserver/views`, styled with Tailwind. Every generator runs through `make generate`
and its output is committed, so building or testing the service needs none of them — only changing
the thing they generate does. The same is true of the store: the SQL in
`internal/store/queries/sqlite` is the source, and sqlc generates the Go that runs it. `make tools`
fetches the pinned Tailwind and sqlc binaries; templ comes from
`go.mod`. `air` runs the generators before each rebuild, so editing a `.templ` file reloads the
running server.

Dependencies are kept current by Renovate, which groups Go and Actions updates and merges the
routine ones itself once CI and branch protection allow it.

## Language

The admin UI reads its text from catalogs rather than from the components, and ships English and
German. A browser's `Accept-Language` picks between them; `ui.language` says what to use when it
names neither.

The picker in the header changes it for whoever is looking, including on the login page, and the
choice is remembered in a cookie. **Browser default** puts it back to `Accept-Language`.

`ui.locale-dir` points at a directory of JSON files named for their language — `de.json`,
`pt-BR.json` — whose entries override the built-in text, entry by entry. That is how to retune
wording, or add a language, without waiting for a release:

```json
{ "nav.routing": "Wegefindung" }
```

A key no catalog carries renders as the key itself, which is a visible fault rather than an empty
space. Webhook responses, API errors and log lines are deliberately not translated: they are read by
machines, and by whoever is reading a log at three in the morning.

## Documentation

- [Architecture](docs/architecture.md)
- [Helm chart](charts/teamster/README.md)
- [Configuring Keycloak for the admin login](docs/keycloak.md)
- [Roadmap](docs/roadmap.md)
- [Architecture Decision Records](docs/adr/)
- [Contribution rules and definition of done](AGENTS.md)
- [Security policy](SECURITY.md)

## License

[Apache License 2.0](LICENSE).
