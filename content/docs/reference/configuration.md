---
title: Configuration
weight: 1
---

Every setting Teamster reads, grouped by the section of the config file it lives in. The same list,
with the defaults of the running build, is printed by `teamster --help`.

## Sources and precedence

| Source | Form | Example |
| --- | --- | --- |
| Command line flag | `--` and the hyphenated key | `--graph-tenant-id=<tenant-id>` |
| Config file | Nested YAML, hyphenated keys | `graph: {tenant-id: <tenant-id>}` |
| Environment variable | `TEAMSTER_` and the key in upper case, `-` and `.` as `_` | `TEAMSTER_GRAPH_TENANT_ID` |
| Default | Built in, listed below | — |

From highest to lowest priority:

1. Command line flags.
2. Config files. A later file overrides an earlier one; `--config` comes last.
3. Environment variables. One is read only when no config file sets that key.
4. Built-in defaults.

{{< callout type="warning" >}}
A secret set in a config file wins over the environment variable that should carry it. Keep
`webhook.token`, `admin.password`, `graph.client-secret`, `bot.client-secret`,
`auth.oidc-client-secret`, `auth.broker.token-encryption-key` and `database.postgres.password` out
of the file and deliver them as `TEAMSTER_*` variables.
{{< /callout >}}

## Config file locations

Searched in this order. Later files override earlier ones.

| Order | Path | Default |
| --- | --- | --- |
| 1 | `$XDG_CONFIG_DIRS/teamster/config.yaml`, each directory, least preferred first | `/etc/xdg/teamster/config.yaml` |
| 2 | `$XDG_CONFIG_HOME/teamster/config.yaml` | `~/.config/teamster/config.yaml` |
| 3 | `./config.yaml` in the working directory | — |

`--config <file>` (`-c`, `TEAMSTER_CONFIG`) loads one more file after these, so its values win. The
files above are still read for every key it does not set.

## Value formats

| Type | Format | Example |
| --- | --- | --- |
| Duration | Go duration string | `90s`, `15m`, `2160h` |
| Boolean | `true` or `false`; as a flag, its presence means `true` | `--metrics-enabled` |
| List | YAML sequence; comma-separated in a flag or variable | `["profile", "email"]`, `profile,email` |

A key the file sets but Teamster does not know is ignored without a warning.

## Server

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `server.addr` | `TEAMSTER_SERVER_ADDR` | `:8080` | Address the HTTP server listens on. |
| `server.shutdown-timeout` | `TEAMSTER_SERVER_SHUTDOWN_TIMEOUT` | `15s` | How long to wait for in-flight requests when shutting down. |
| `server.read-timeout` | `TEAMSTER_SERVER_READ_TIMEOUT` | `15s` | How long a client may take to send a request, headers and body. |
| `server.write-timeout` | `TEAMSTER_SERVER_WRITE_TIMEOUT` | `60s` | How long a handler may take to answer; must exceed `graph.timeout-sec`. |
| `server.idle-timeout` | `TEAMSTER_SERVER_IDLE_TIMEOUT` | `120s` | How long an idle keep-alive connection is held open. |
| `server.external-url` | `TEAMSTER_SERVER_EXTERNAL_URL` | — | Absolute URL the admin UI is reachable at, used to link to it from messages. |

## Admin UI text

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `ui.language` | `TEAMSTER_UI_LANGUAGE` | `en` | Language to use when a browser asks for none this build carries. |
| `ui.locale-dir` | `TEAMSTER_UI_LOCALE_DIR` | — | Directory of catalog files that override the built-in text. |

## Metrics

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `metrics.enabled` | `TEAMSTER_METRICS_ENABLED` | `false` | Collect metrics and export them. |
| `metrics.addr` | `TEAMSTER_METRICS_ADDR` | `127.0.0.1:9090` | Address of the unauthenticated metrics listener. |
| `metrics.path` | `TEAMSTER_METRICS_PATH` | `/metrics` | Path the Prometheus exporter is served at. |
| `metrics.prometheus` | `TEAMSTER_METRICS_PROMETHEUS` | `true` | Serve the Prometheus exporter on the metrics listener. |
| `metrics.otlp-endpoint` | `TEAMSTER_METRICS_OTLP_ENDPOINT` | — | OTLP collector endpoint; empty runs no OTLP exporter. |
| `metrics.otlp-protocol` | `TEAMSTER_METRICS_OTLP_PROTOCOL` | `http` | OTLP transport. One of `grpc`, `http`. |
| `metrics.otlp-insecure` | `TEAMSTER_METRICS_OTLP_INSECURE` | `false` | Send OTLP without TLS. |
| `metrics.otlp-interval` | `TEAMSTER_METRICS_OTLP_INTERVAL` | `60s` | How often metrics are pushed over OTLP. |
| `metrics.shutdown-timeout` | `TEAMSTER_METRICS_SHUTDOWN_TIMEOUT` | `5s` | How long the final export and the listener drain may take. |
| `metrics.service-name` | `TEAMSTER_METRICS_SERVICE_NAME` | `teamster` | `service.name` reported with every metric. |

## Database

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `database.driver` | `TEAMSTER_DATABASE_DRIVER` | `sqlite` | Storage backend: sqlite for a single instance, postgres for several. One of `sqlite`, `postgres`. |
| `database.path` | `TEAMSTER_DATABASE_PATH` | `teamster.db` | Path to the SQLite database file. Used when `database.driver` is sqlite. |
| `database.postgres.host` | `TEAMSTER_DATABASE_POSTGRES_HOST` | — | Postgres host name. |
| `database.postgres.port` | `TEAMSTER_DATABASE_POSTGRES_PORT` | `5432` | Postgres port. |
| `database.postgres.dbname` | `TEAMSTER_DATABASE_POSTGRES_DBNAME` | `teamster` | Postgres database name. |
| `database.postgres.user` | `TEAMSTER_DATABASE_POSTGRES_USER` | `teamster` | Postgres user. |
| `database.postgres.password` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` | — | Postgres password. Deliver it as `TEAMSTER_DATABASE_POSTGRES_PASSWORD`; a config file value would win over the environment. |
| `database.postgres.sslmode` | `TEAMSTER_DATABASE_POSTGRES_SSLMODE` | `require` | libpq sslmode. One of `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`. |
| `database.postgres.sslrootcert` | `TEAMSTER_DATABASE_POSTGRES_SSLROOTCERT` | — | CA certificate file that verify-ca and verify-full check the server against. |
| `database.postgres.url` | `TEAMSTER_DATABASE_POSTGRES_URL` | — | Full `postgres://` connection URL instead of the individual fields. |
| `database.max-open-conns` | `TEAMSTER_DATABASE_MAX_OPEN_CONNS` | `0` | Maximum open connections; 0 lets the backend choose. |
| `database.max-idle-conns` | `TEAMSTER_DATABASE_MAX_IDLE_CONNS` | `0` | Maximum idle connections; 0 lets the backend choose. |
| `database.conn-max-lifetime` | `TEAMSTER_DATABASE_CONN_MAX_LIFETIME` | `0s` | How long a pooled connection may be reused; 0 lets the backend choose. |
| `database.connect-timeout` | `TEAMSTER_DATABASE_CONNECT_TIMEOUT` | `10s` | How long to wait for the first connection before giving up at startup. |
| `database.migrate` | `TEAMSTER_DATABASE_MIGRATE` | `auto` | What opening the database does about pending migrations: apply them, verify none are pending, or neither. One of `auto`, `verify`, `off`. |

## Webhooks

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `webhook.token` | `TEAMSTER_WEBHOOK_TOKEN` | — | Deployment-wide webhook token, accepted as `Authorization: Bearer`. Optional when tokens are issued in the admin UI. |
| `webhook.max-recipients` | `TEAMSTER_WEBHOOK_MAX_RECIPIENTS` | `100` | Most people one message may address. |
| `webhook.fanout-concurrency` | `TEAMSTER_WEBHOOK_FANOUT_CONCURRENCY` | `8` | How many people one message is delivered to at once. |

## Local admin login

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `admin.username` | `TEAMSTER_ADMIN_USERNAME` | — | Local admin login name, also accepted as basic auth on `/api`. |
| `admin.password` | `TEAMSTER_ADMIN_PASSWORD` | — | Local admin login password, also accepted as basic auth on `/api`. |

## Sign-in (OIDC)

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `auth.oidc-discovery-url` | `TEAMSTER_AUTH_OIDC_DISCOVERY_URL` | — | URL of the provider's `/.well-known/openid-configuration` document. |
| `auth.oidc-client-id` | `TEAMSTER_AUTH_OIDC_CLIENT_ID` | — | OIDC client ID. |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | — | OIDC client secret; omit for a public client using PKCE. |
| `auth.oidc-redirect-url` | `TEAMSTER_AUTH_OIDC_REDIRECT_URL` | — | Absolute URL of `/admin/auth/callback` as registered with the provider. |
| `auth.oidc-scopes` | `TEAMSTER_AUTH_OIDC_SCOPES` | `profile,email,roles` | Extra scopes to request beyond openid. |
| `auth.claim` | `TEAMSTER_AUTH_CLAIM` | `realm_access.roles` | Dotted path of the claim carrying membership, e.g. `realm_access.roles`. |
| `auth.groups-claim` | `TEAMSTER_AUTH_GROUPS_CLAIM` | `groups` | Dotted path of the claim carrying group membership, which local groups can name; empty to skip. |
| `auth.object-id-claim` | `TEAMSTER_AUTH_OBJECT_ID_CLAIM` | `oid` | Dotted path of the claim carrying the user's Entra object id, which finds their own Teams chat and is the one person an **only me** token may name. |
| `auth.default-role` | `TEAMSTER_AUTH_DEFAULT_ROLE` | — | Role for a user whose claim names none: admin, editor, viewer, or empty for no access. One of `admin`, `editor`, `viewer` or empty. |
| `auth.session-ttl` | `TEAMSTER_AUTH_SESSION_TTL` | `12h` | How long a login lasts. |
| `auth.broker.enabled` | `TEAMSTER_AUTH_BROKER_ENABLED` | `false` | Fetch each admin's own Entra token from Keycloak to list their own Teams/Channels. |
| `auth.broker.idp-alias` | `TEAMSTER_AUTH_BROKER_IDP_ALIAS` | — | Keycloak identity provider alias the Entra login is federated through. |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` | — | 32-byte base64 key encrypting stored Keycloak tokens at rest. |

## Microsoft Graph

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `graph.tenant-id` | `TEAMSTER_GRAPH_TENANT_ID` | — | Microsoft Entra tenant ID. |
| `graph.client-id` | `TEAMSTER_GRAPH_CLIENT_ID` | — | Microsoft Entra application (client) ID. |
| `graph.client-secret` | `TEAMSTER_GRAPH_CLIENT_SECRET` | — | Microsoft Entra client secret. |
| `graph.base-url` | `TEAMSTER_GRAPH_BASE_URL` | `https://graph.microsoft.com/v1.0` | Microsoft Graph API base URL. |
| `graph.timeout-sec` | `TEAMSTER_GRAPH_TIMEOUT_SEC` | `10` | Timeout in seconds for Graph API calls. |
| `graph.token-url` | `TEAMSTER_GRAPH_TOKEN_URL` | — | OAuth2 token endpoint. Empty derives the public Microsoft Entra endpoint for `graph.tenant-id`. |
| `graph.scope` | `TEAMSTER_GRAPH_SCOPE` | `https://graph.microsoft.com/.default` | OAuth2 scope requested for Graph. |

## Teams bot

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `bot.tenant-id` | `TEAMSTER_BOT_TENANT_ID` | — | Microsoft Entra tenant ID for the bot registration. |
| `bot.client-id` | `TEAMSTER_BOT_CLIENT_ID` | — | Microsoft Entra application (client) ID for the bot registration. |
| `bot.client-secret` | `TEAMSTER_BOT_CLIENT_SECRET` | — | Microsoft Entra client secret for the bot registration. |
| `bot.tenant-type` | `TEAMSTER_BOT_TENANT_TYPE` | `single` | Whether the bot registration is single or multi tenant. One of `single`, `multi` or empty. |
| `bot.token-url` | `TEAMSTER_BOT_TOKEN_URL` | — | OAuth2 token endpoint. Empty derives one from `bot.tenant-id` and `bot.tenant-type`. |
| `bot.scope` | `TEAMSTER_BOT_SCOPE` | `https://api.botframework.com/.default` | OAuth2 scope requested for the Bot Connector API. |
| `bot.metadata-url` | `TEAMSTER_BOT_METADATA_URL` | `https://login.botframework.com/v1/.well-known/openidconfiguration` | Bot Framework OpenID configuration document, used to validate inbound requests. |
| `bot.timeout-sec` | `TEAMSTER_BOT_TIMEOUT_SEC` | `10` | Timeout in seconds for Bot Connector API calls. |
| `bot.service-url` | `TEAMSTER_BOT_SERVICE_URL` | `https://smba.trafficmanager.net/teams/` | Bot Connector endpoint for a team whose own is not yet known. |
| `bot.global-install` | `TEAMSTER_BOT_GLOBAL_INSTALL` | `false` | Install the Teams app for every enabled member of the tenant, and let nobody opt out. |
| `bot.app-id` | `TEAMSTER_BOT_APP_ID` | — | Teams app id, the manifest's id, used to find the app in the organization catalog. |
| `bot.catalog-app-id` | `TEAMSTER_BOT_CATALOG_APP_ID` | — | Teams app catalog id; empty looks it up from `bot.app-id` or `bot.client-id`. |
| `bot.reconcile-interval` | `TEAMSTER_BOT_RECONCILE_INTERVAL` | `6h` | How often installs are checked for new and returning members; 0 checks only when an admin asks. |
| `bot.reverify-interval` | `TEAMSTER_BOT_REVERIFY_INTERVAL` | `168h` | How long an install is trusted before it is checked again. |
| `bot.install-concurrency` | `TEAMSTER_BOT_INSTALL_CONCURRENCY` | `4` | How many installs run at once during a reconcile. |
| `bot.inline-install-budget` | `TEAMSTER_BOT_INLINE_INSTALL_BUDGET` | `5` | How many people one message may install the app for before it is delivered. |
| `bot.welcome-message` | `TEAMSTER_BOT_WELCOME_MESSAGE` | — | Sent once when the app is installed for a person; empty sends nothing. |
| `bot.directory-ttl` | `TEAMSTER_BOT_DIRECTORY_TTL` | `24h` | How long a looked-up person is trusted before Graph is asked again. |
| `bot.pacing.strategy` | `TEAMSTER_BOT_PACING_STRATEGY` | `process` | How calls are paced: `process` gives each replica a budget of its own. One of `process`. |
| `bot.pacing.rate` | `TEAMSTER_BOT_PACING_RATE` | `20` | Bot Connector calls per second for this replica; divide the tenant's budget by the replica count. |
| `bot.pacing.burst` | `TEAMSTER_BOT_PACING_BURST` | `20` | Calls this replica may make at once before `rate` applies. |
| `bot.pacing.conversation-rate` | `TEAMSTER_BOT_PACING_CONVERSATION_RATE` | `0.5` | Calls per second to one conversation. |
| `bot.pacing.conversation-burst` | `TEAMSTER_BOT_PACING_CONVERSATION_BURST` | `7` | Calls to one conversation at once before `conversation-rate` applies. |
| `bot.pacing.retries` | `TEAMSTER_BOT_PACING_RETRIES` | `3` | How often a call refused with `429` or `503` is retried; `0` fails it at once. |
| `bot.pacing.max-retry-wait` | `TEAMSTER_BOT_PACING_MAX_RETRY_WAIT` | `30s` | Longest wait before a retry, whatever `Retry-After` asks for. |

## Event samples

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `samples.enabled` | `TEAMSTER_SAMPLES_ENABLED` | `true` | Remember label keys, label values and attribute keys of incoming events for editor completion. |
| `samples.retention` | `TEAMSTER_SAMPLES_RETENTION` | `720h` | How long a sample is kept after it was last seen. |
| `samples.max-values-per-key` | `TEAMSTER_SAMPLES_MAX_VALUES_PER_KEY` | `50` | How many of the most recently seen values are kept per label key. |
| `samples.max-value-length` | `TEAMSTER_SAMPLES_MAX_VALUE_LENGTH` | `200` | Label values longer than this many bytes are not sampled. |
| `samples.lru-size` | `TEAMSTER_SAMPLES_LRU_SIZE` | `4096` | How many samples are held in memory to coalesce writes. |
| `samples.flush-interval` | `TEAMSTER_SAMPLES_FLUSH_INTERVAL` | `5m` | How long a sample already written waits before its count is written again. |

## Audit trail

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `audit.file` | `TEAMSTER_AUDIT_FILE` | — | Append audit events to this file as JSON lines; `-` is stdout, empty is off. |
| `audit.database` | `TEAMSTER_AUDIT_DATABASE` | `false` | Keep audit events in the database, which the admin UI lists. Audit is off unless this or a sink is configured. |
| `audit.retention-age` | `TEAMSTER_AUDIT_RETENTION_AGE` | `2160h` | Forget database audit events older than this; 0 keeps them regardless of age. |
| `audit.retention-count` | `TEAMSTER_AUDIT_RETENTION_COUNT` | `100000` | Keep at most this many database audit events; 0 is no limit. |
| `audit.prune-interval` | `TEAMSTER_AUDIT_PRUNE_INTERVAL` | `1h` | How often database audit retention is applied. |
| `audit.queue-size` | `TEAMSTER_AUDIT_QUEUE_SIZE` | `1024` | Events held for each sink other than the database before new ones are dropped. |
| `audit.nats.url` | `TEAMSTER_AUDIT_NATS_URL` | — | NATS server URL, such as `nats://nats:4222`; empty is off. |
| `audit.nats.subject-prefix` | `TEAMSTER_AUDIT_NATS_SUBJECT_PREFIX` | `teamster.audit` | Events are published to `<prefix>.<resource type>.<action>`. |
| `audit.nats.stream` | `TEAMSTER_AUDIT_NATS_STREAM` | `TEAMSTER_AUDIT` | JetStream stream that captures the subjects. |
| `audit.nats.create-stream` | `TEAMSTER_AUDIT_NATS_CREATE_STREAM` | `false` | Create or update the stream at start. |
| `audit.nats.creds-file` | `TEAMSTER_AUDIT_NATS_CREDS_FILE` | — | NATS credentials file (JWT and NKey seed). |
| `audit.nats.timeout` | `TEAMSTER_AUDIT_NATS_TIMEOUT` | `5s` | Connect timeout, and how long creating the stream may take. |
| `audit.nats.backfill` | `TEAMSTER_AUDIT_NATS_BACKFILL` | `false` | Publish from the database trail instead of a queue, catching up after NATS was unreachable; needs `audit.database`. |
| `audit.nats.backfill-interval` | `TEAMSTER_AUDIT_NATS_BACKFILL_INTERVAL` | `2s` | How often the backfill looks for events to publish. |
| `audit.nats.backfill-settle` | `TEAMSTER_AUDIT_NATS_BACKFILL_SETTLE` | `5s` | How old an event must be before the backfill publishes it, so one committed late is not skipped. |

## Logging

| Key | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `log.level` | `TEAMSTER_LOG_LEVEL` | `info` | Lowest level written: debug, info, warn or error. One of `debug`, `info`, `warn`, `error`. |
| `log.format` | `TEAMSTER_LOG_FORMAT` | `text` | Line format: text to read, json for a log pipeline. One of `text`, `json`. |

## Startup checks

`teamster serve` refuses to start when the configuration fails one of these checks. The other
commands do not run them.

| Setting | Check |
| --- | --- |
| `graph.tenant-id`, `graph.client-id`, `graph.client-secret` | All three are required. |
| `admin.username`, `admin.password` | Both are required. |
| `database.path` | Required when `database.driver` is `sqlite`. |
| `database.postgres.host` | Required when `database.driver` is `postgres` and `database.postgres.url` is empty. |
| `database.postgres.dbname`, `database.postgres.user` | Required when `database.driver` is `postgres` and `database.postgres.url` is empty. |
| `database.postgres.url` | Cannot be combined with `database.postgres.host`, `.password` or `.sslrootcert`. |
| `server.external-url` | Absolute `http` or `https` URL when set. |
| `metrics.*` | With `metrics.enabled`: `metrics.prometheus` or `metrics.otlp-endpoint` is set; with the exporter on, `metrics.addr` is set and differs from `server.addr`, and `metrics.path` starts with `/`; with an OTLP endpoint, `metrics.otlp-interval` is positive. |
| `bot.tenant-id`, `bot.client-id`, `bot.client-secret` | Setting any one requires `bot.client-id` and `bot.client-secret`, and `bot.tenant-id` unless `bot.tenant-type` is `multi`. |
| `bot.timeout-sec` | Positive when the bot is configured. |
| `bot.metadata-url` | An `https` URL when the bot is configured. |
| `bot.service-url` | An `https` URL when set. |
| `bot.directory-ttl` | Positive when the bot is configured. |
| `bot.pacing.*` | When the bot is configured: `rate`, `burst`, `conversation-rate`, `conversation-burst` and `max-retry-wait` are positive; `retries` is not negative. |
| `bot.global-install` | Needs the bot configured and `bot.app-id` or `bot.catalog-app-id`. |
| `bot.reconcile-interval` | With `bot.global-install`: `0` or at least `5m`. |
| `bot.reverify-interval` | With `bot.global-install`: positive. |
| `bot.install-concurrency` | With `bot.global-install`: between 1 and 16. |
| `bot.inline-install-budget` | With `bot.global-install`: not negative. |
| `webhook.max-recipients` | Between 1 and 1000. |
| `webhook.fanout-concurrency` | At least 1. |
| `auth.oidc-client-id`, `auth.oidc-redirect-url`, `auth.claim` | Required when `auth.oidc-discovery-url` is set; `auth.oidc-redirect-url` is an absolute URL. |
| `auth.broker.*` | With `auth.broker.enabled`: `auth.oidc-discovery-url` and `auth.broker.idp-alias` are set, and `auth.broker.token-encryption-key` is base64 for exactly 32 bytes. |
| `samples.*` | With `samples.enabled`: `retention`, `max-values-per-key`, `max-value-length`, `lru-size` and `flush-interval` are positive. |
| `audit.retention-age`, `audit.retention-count` | Not negative. |
| `audit.prune-interval` | Positive when `audit.database` is on and a retention limit is set. |
| `audit.queue-size` | Positive when `audit.file` or `audit.nats.url` is set. |
| `audit.nats.subject-prefix` | With `audit.nats.url`: not empty, no wildcards or whitespace, no leading or trailing `.`. |
| `audit.nats.stream` | Required with `audit.nats.create-stream`. |
| `audit.nats.timeout` | Positive when `audit.nats.url` is set. |
| `audit.nats.backfill` | Needs `audit.database`; `audit.nats.backfill-interval` positive and `audit.nats.backfill-settle` not negative. |

## See also

* [Configure Teamster](../../guides/configuration/)
* [CLI](../cli/)
* [Helm values](../helm-values/)
