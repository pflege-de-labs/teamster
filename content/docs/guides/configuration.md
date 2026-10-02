---
title: Configure Teamster
weight: 1
---

Teamster reads its settings from YAML files, environment variables and command line flags. This
guide shows where each comes from and which one wins. Every key, its default and its environment
variable are listed in the [configuration reference](../../reference/configuration/);
`teamster --help` prints the same list.

## Put the config file where Teamster finds it

Teamster searches these files in order, following the XDG Base Directory Specification:

1. `$XDG_CONFIG_DIRS/teamster/config.yaml` (default `/etc/xdg/teamster/config.yaml`)
2. `$XDG_CONFIG_HOME/teamster/config.yaml` (default `~/.config/teamster/config.yaml`)
3. `./config.yaml` in the working directory

Every file that exists is read, and a later file overrides an earlier one key by key. When
`$XDG_CONFIG_DIRS` lists several directories, the first one listed takes precedence, as the
specification says.

To skip the search and read one file only, pass it explicitly:

```bash
teamster --config /path/to/config.yaml
```

`-c` is the short form, and `TEAMSTER_CONFIG` sets the same thing from the environment.

Start from
[`config.example.yaml`](https://github.com/pflege-de-labs/teamster/blob/main/config.example.yaml),
which carries every key with a comment.

## Write the keys hyphenated

YAML keys are the flag names, nested by their prefix. A flag `--graph-tenant-id` is the key
`tenant-id` under `graph`:

```yaml
graph:
  tenant-id: "<tenant-id>"
  client-id: "<client-id>"
  client-secret: "<client-secret>"
```

Teamster ignores keys it does not know. A misspelled required key shows up as a validation error at
startup; a misspelled optional key silently keeps its default. If a setting seems to have no
effect, check its spelling against the [reference](../../reference/configuration/).

## Keep secrets in environment variables

Every flag also reads an environment variable: `TEAMSTER_`, then the flag name in upper case with
underscores. `graph.client-secret` is `TEAMSTER_GRAPH_CLIENT_SECRET`, `server.addr` is
`TEAMSTER_SERVER_ADDR`.

The order of precedence, highest first:

1. command line flags
2. config files
3. environment variables
4. built-in defaults

{{< callout type="warning" >}}
An environment variable applies only when no config file sets that key. A key set to `""` counts as
set: it overrides the variable your secret store provides. Delete the key or comment it out.
`config.example.yaml` sets several secrets to `""`, so remove those lines when you copy it.
{{< /callout >}}

Keep these out of the config file and deliver them as environment variables:

| Key | Environment variable |
| --- | --- |
| `webhook.token` | `TEAMSTER_WEBHOOK_TOKEN` |
| `admin.password` | `TEAMSTER_ADMIN_PASSWORD` |
| `graph.client-secret` | `TEAMSTER_GRAPH_CLIENT_SECRET` |
| `bot.client-secret` | `TEAMSTER_BOT_CLIENT_SECRET` |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` |
| `database.postgres.password` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` |
| `database.postgres.url` | `TEAMSTER_DATABASE_POSTGRES_URL` |

The Helm chart does this split for you: `config.settings` becomes the config file and
`credentials` the environment. See the [Helm values reference](../../reference/helm-values/).

Teamster refuses to start without `graph.tenant-id`, `graph.client-id`, `graph.client-secret`,
`admin.username` and `admin.password`.

## Talk to something other than the public Graph

Three keys decide which Microsoft Graph Teamster talks to:

| Key | Default |
| --- | --- |
| `graph.base-url` | `https://graph.microsoft.com/v1.0` |
| `graph.token-url` | empty: `https://login.microsoftonline.com/<tenant-id>/oauth2/v2.0/token` |
| `graph.scope` | `https://graph.microsoft.com/.default` |

A normal deployment leaves all three alone. For a sovereign cloud, change all three together. To
exercise delivery without a Microsoft tenant, point them at a server you control:

```yaml
graph:
  base-url: "http://127.0.0.1:18500"
  token-url: "http://127.0.0.1:18500/token"
  scope: "http://127.0.0.1:18500/.default"
```

The bot has the same knobs for the Bot Framework: `bot.token-url`, `bot.scope` and
`bot.metadata-url`. A sovereign cloud changes those together too. `bot.metadata-url` must stay an
`https` URL; see [Set up the Teams bot](../teams-bot/).

## Set the admin UI language

The admin UI ships in English and German. The browser's `Accept-Language` picks between them.
`ui.language` (`TEAMSTER_UI_LANGUAGE`, default `en`) applies when the browser names neither:

```yaml
ui:
  language: "de"
```

Each person can override it: the flags in the user menu, or the picker in the header of the login
page. The choice is kept in a cookie. **Browser default** (the globe) goes back to
`Accept-Language`.

To change wording or add a language without waiting for a release, point `ui.locale-dir` at a
directory of JSON files named for their language, such as `de.json` or `pt-BR.json`. Each entry
overrides one built-in text:

```json
{ "nav.routing": "Wegefindung" }
```

A key that no catalog carries renders as the key itself, so a missing entry is visible. Webhook
responses, API errors and log lines are always in English.

## Show times in your own time zone

The admin UI shows times in UTC, with the zone named. To see them in your browser's zone, pick
**Browser time** in the user menu; **UTC** switches back. The choice is kept in a cookie, per
browser.

Browser time needs JavaScript; without it, times stay in UTC. API responses and logs are always in
UTC. There is no server-side setting for this.

## Install shell completion

Teamster generates completion scripts for bash, zsh and fish:

```bash
teamster completion bash > /etc/bash_completion.d/teamster
teamster completion zsh  > "${fpath[1]}/_teamster"
teamster completion fish > ~/.config/fish/completions/teamster.fish
```

The script is generated from the command tree, so it knows every command, every flag and the values
of an enum flag: `--database-driver <TAB>` offers `sqlite` and `postgres`. Regenerate it after an
upgrade to pick up new flags.

{{< callout type="info" >}}
In bash, an enum flag completes to the current value of its environment variable instead of its
allowed values: nothing when the variable is unset. Commands and flags complete normally, and zsh
and fish offer the real values.
{{< /callout >}}
