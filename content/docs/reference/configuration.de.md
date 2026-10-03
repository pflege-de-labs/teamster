---
title: Konfiguration
weight: 1
---

Alle Einstellungen, die Teamster liest, gruppiert nach dem Abschnitt der Konfigurationsdatei, in
dem sie stehen. Dieselbe Liste, mit den Standardwerten des laufenden Builds, gibt
`teamster --help` aus.

## Quellen und Vorrang {#sources-and-precedence}

| Quelle | Form | Beispiel |
| --- | --- | --- |
| Kommandozeilen-Flag | `--` und der Schlüssel mit Bindestrichen | `--graph-tenant-id=<tenant-id>` |
| Konfigurationsdatei | Verschachteltes YAML, Schlüssel mit Bindestrichen | `graph: {tenant-id: <tenant-id>}` |
| Umgebungsvariable | `TEAMSTER_` und der Schlüssel in Großbuchstaben, `-` und `.` als `_` | `TEAMSTER_GRAPH_TENANT_ID` |
| Standard | Eingebaut, unten aufgeführt | — |

Von höchster zu niedrigster Priorität:

1. Kommandozeilen-Flags.
2. Konfigurationsdateien. Eine spätere Datei überschreibt eine frühere; `--config` kommt zuletzt.
3. Umgebungsvariablen. Eine Variable wird nur gelesen, wenn keine Konfigurationsdatei den
   Schlüssel setzt.
4. Eingebaute Standardwerte.

{{< callout type="warning" >}}
Ein Geheimnis in einer Konfigurationsdatei hat Vorrang vor der Umgebungsvariablen, die es
eigentlich liefern soll. Halten Sie `webhook.token`, `admin.password`, `graph.client-secret`,
`bot.client-secret`, `auth.oidc-client-secret`, `auth.broker.token-encryption-key` und
`database.postgres.password` aus der Datei heraus und übergeben Sie sie als `TEAMSTER_*`-Variablen.
{{< /callout >}}

## Orte der Konfigurationsdatei {#config-file-locations}

In dieser Reihenfolge gesucht. Spätere Dateien überschreiben frühere.

| Reihenfolge | Pfad | Standard |
| --- | --- | --- |
| 1 | `$XDG_CONFIG_DIRS/teamster/config.yaml`, jedes Verzeichnis, das am wenigsten bevorzugte zuerst | `/etc/xdg/teamster/config.yaml` |
| 2 | `$XDG_CONFIG_HOME/teamster/config.yaml` | `~/.config/teamster/config.yaml` |
| 3 | `./config.yaml` im Arbeitsverzeichnis | — |

`--config <file>` (`-c`, `TEAMSTER_CONFIG`) lädt nach diesen eine weitere Datei, deren Werte daher
gewinnen. Die Dateien oben werden weiterhin für jeden Schlüssel gelesen, den sie nicht setzt.

## Wertformate {#value-formats}

| Typ | Format | Beispiel |
| --- | --- | --- |
| Dauer | Go-Duration-String | `90s`, `15m`, `2160h` |
| Boolescher Wert | `true` oder `false`; als Flag bedeutet seine Angabe `true` | `--metrics-enabled` |
| Liste | YAML-Sequenz; in einem Flag oder einer Variablen durch Kommas getrennt | `["profile", "email"]`, `profile,email` |

Einen Schlüssel, den die Datei setzt, Teamster aber nicht kennt, ignoriert Teamster ohne Warnung.

## Server {#server}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `server.addr` | `TEAMSTER_SERVER_ADDR` | `:8080` | Adresse, auf der der HTTP-Server lauscht. |
| `server.shutdown-timeout` | `TEAMSTER_SERVER_SHUTDOWN_TIMEOUT` | `15s` | Wie lange beim Herunterfahren auf laufende Anfragen gewartet wird. |
| `server.read-timeout` | `TEAMSTER_SERVER_READ_TIMEOUT` | `15s` | Wie lange ein Client brauchen darf, um eine Anfrage samt Headern und Körper zu senden. |
| `server.write-timeout` | `TEAMSTER_SERVER_WRITE_TIMEOUT` | `60s` | Wie lange ein Handler für die Antwort brauchen darf; muss `graph.timeout-sec` übersteigen. |
| `server.idle-timeout` | `TEAMSTER_SERVER_IDLE_TIMEOUT` | `120s` | Wie lange eine untätige Keep-alive-Verbindung offen gehalten wird. |
| `server.external-url` | `TEAMSTER_SERVER_EXTERNAL_URL` | — | Absolute URL, unter der die Verwaltungsoberfläche erreichbar ist; dient Nachrichten als Link dorthin. |

## Texte der Verwaltungsoberfläche {#admin-ui-text}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `ui.language` | `TEAMSTER_UI_LANGUAGE` | `en` | Sprache, wenn ein Browser keine anfordert, die dieser Build mitbringt. |
| `ui.locale-dir` | `TEAMSTER_UI_LOCALE_DIR` | — | Verzeichnis mit Katalogdateien, die die eingebauten Texte überschreiben. |

## Metriken {#metrics}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `metrics.enabled` | `TEAMSTER_METRICS_ENABLED` | `false` | Metriken erfassen und exportieren. |
| `metrics.addr` | `TEAMSTER_METRICS_ADDR` | `127.0.0.1:9090` | Adresse des Metrik-Listeners, der keine Authentifizierung verlangt. |
| `metrics.path` | `TEAMSTER_METRICS_PATH` | `/metrics` | Pfad, unter dem der Prometheus-Exporter bereitgestellt wird. |
| `metrics.prometheus` | `TEAMSTER_METRICS_PROMETHEUS` | `true` | Den Prometheus-Exporter auf dem Metrik-Listener bereitstellen. |
| `metrics.otlp-endpoint` | `TEAMSTER_METRICS_OTLP_ENDPOINT` | — | Endpunkt des OTLP-Collectors; leer startet keinen OTLP-Exporter. |
| `metrics.otlp-protocol` | `TEAMSTER_METRICS_OTLP_PROTOCOL` | `http` | OTLP-Transport. Einer von `grpc`, `http`. |
| `metrics.otlp-insecure` | `TEAMSTER_METRICS_OTLP_INSECURE` | `false` | OTLP ohne TLS senden. |
| `metrics.otlp-interval` | `TEAMSTER_METRICS_OTLP_INTERVAL` | `60s` | Wie oft Metriken über OTLP gesendet werden. |
| `metrics.shutdown-timeout` | `TEAMSTER_METRICS_SHUTDOWN_TIMEOUT` | `5s` | Wie lange der letzte Export und das Leeren des Listeners dauern dürfen. |
| `metrics.service-name` | `TEAMSTER_METRICS_SERVICE_NAME` | `teamster` | `service.name`, das mit jeder Metrik gemeldet wird. |

## Datenbank {#database}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `database.driver` | `TEAMSTER_DATABASE_DRIVER` | `sqlite` | Speicher-Backend: sqlite für eine einzelne Instanz, postgres für mehrere. Einer von `sqlite`, `postgres`. |
| `database.path` | `TEAMSTER_DATABASE_PATH` | `teamster.db` | Pfad zur SQLite-Datenbankdatei. Verwendet, wenn `database.driver` sqlite ist. |
| `database.postgres.host` | `TEAMSTER_DATABASE_POSTGRES_HOST` | — | Hostname von Postgres. |
| `database.postgres.port` | `TEAMSTER_DATABASE_POSTGRES_PORT` | `5432` | Port von Postgres. |
| `database.postgres.dbname` | `TEAMSTER_DATABASE_POSTGRES_DBNAME` | `teamster` | Name der Postgres-Datenbank. |
| `database.postgres.user` | `TEAMSTER_DATABASE_POSTGRES_USER` | `teamster` | Postgres-Benutzer. |
| `database.postgres.password` | `TEAMSTER_DATABASE_POSTGRES_PASSWORD` | — | Postgres-Passwort. Übergeben Sie es als `TEAMSTER_DATABASE_POSTGRES_PASSWORD`; ein Wert in der Konfigurationsdatei hätte Vorrang vor der Umgebung. |
| `database.postgres.sslmode` | `TEAMSTER_DATABASE_POSTGRES_SSLMODE` | `require` | sslmode von libpq. Einer von `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`. |
| `database.postgres.sslrootcert` | `TEAMSTER_DATABASE_POSTGRES_SSLROOTCERT` | — | CA-Zertifikatsdatei, gegen die verify-ca und verify-full den Server prüfen. |
| `database.postgres.url` | `TEAMSTER_DATABASE_POSTGRES_URL` | — | Vollständige `postgres://`-Verbindungs-URL statt der einzelnen Felder. |
| `database.max-open-conns` | `TEAMSTER_DATABASE_MAX_OPEN_CONNS` | `0` | Höchstzahl offener Verbindungen; 0 überlässt die Wahl dem Backend. |
| `database.max-idle-conns` | `TEAMSTER_DATABASE_MAX_IDLE_CONNS` | `0` | Höchstzahl untätiger Verbindungen; 0 überlässt die Wahl dem Backend. |
| `database.conn-max-lifetime` | `TEAMSTER_DATABASE_CONN_MAX_LIFETIME` | `0s` | Wie lange eine Verbindung aus dem Pool wiederverwendet werden darf; 0 überlässt die Wahl dem Backend. |
| `database.connect-timeout` | `TEAMSTER_DATABASE_CONNECT_TIMEOUT` | `10s` | Wie lange beim Start auf die erste Verbindung gewartet wird, bevor Teamster aufgibt. |
| `database.migrate` | `TEAMSTER_DATABASE_MIGRATE` | `auto` | Was das Öffnen der Datenbank mit ausstehenden Migrationen macht: sie anwenden, prüfen, dass keine aussteht, oder keins von beidem. Einer von `auto`, `verify`, `off`. |

## Webhooks {#webhooks}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `webhook.token` | `TEAMSTER_WEBHOOK_TOKEN` | — | Webhook-Token für die gesamte Installation, akzeptiert als `Authorization: Bearer`. Optional, wenn Token in der Verwaltungsoberfläche ausgestellt werden. |
| `webhook.max-recipients` | `TEAMSTER_WEBHOOK_MAX_RECIPIENTS` | `100` | Höchstzahl der Personen, die eine Nachricht adressieren darf. |
| `webhook.fanout-concurrency` | `TEAMSTER_WEBHOOK_FANOUT_CONCURRENCY` | `8` | An wie viele Personen eine Nachricht gleichzeitig zugestellt wird. |

## Lokale Administrator-Anmeldung {#local-admin-login}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `admin.username` | `TEAMSTER_ADMIN_USERNAME` | — | Anmeldename des lokalen Administrators, auch als Basic Auth auf `/api` akzeptiert. |
| `admin.password` | `TEAMSTER_ADMIN_PASSWORD` | — | Passwort des lokalen Administrators, auch als Basic Auth auf `/api` akzeptiert. |

## Anmeldung (OIDC) {#sign-in-oidc}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `auth.oidc-discovery-url` | `TEAMSTER_AUTH_OIDC_DISCOVERY_URL` | — | URL des Dokuments `/.well-known/openid-configuration` des Anbieters. |
| `auth.oidc-client-id` | `TEAMSTER_AUTH_OIDC_CLIENT_ID` | — | OIDC-Client-ID. |
| `auth.oidc-client-secret` | `TEAMSTER_AUTH_OIDC_CLIENT_SECRET` | — | OIDC-Client-Secret; weglassen für einen öffentlichen Client mit PKCE. |
| `auth.oidc-redirect-url` | `TEAMSTER_AUTH_OIDC_REDIRECT_URL` | — | Absolute URL von `/admin/auth/callback`, wie beim Anbieter registriert. |
| `auth.oidc-scopes` | `TEAMSTER_AUTH_OIDC_SCOPES` | `profile,email,roles` | Zusätzlich zu openid angeforderte Scopes. |
| `auth.claim` | `TEAMSTER_AUTH_CLAIM` | `realm_access.roles` | Pfad mit Punkten zum Claim, der die Zugehörigkeit trägt, z. B. `realm_access.roles`. |
| `auth.groups-claim` | `TEAMSTER_AUTH_GROUPS_CLAIM` | `groups` | Pfad mit Punkten zum Claim, der die Gruppenzugehörigkeit trägt, die lokale Gruppen nennen können; leer, um ihn zu überspringen. |
| `auth.object-id-claim` | `TEAMSTER_AUTH_OBJECT_ID_CLAIM` | `oid` | Pfad mit Punkten zum Claim, der die Entra-Objekt-ID des Benutzers trägt, über die sein eigener Teams-Chat gefunden wird und die die eine Person ist, die ein **nur mich**-Token nennen darf. |
| `auth.default-role` | `TEAMSTER_AUTH_DEFAULT_ROLE` | — | Rolle für einen Benutzer, dessen Claim keine nennt: admin, editor, viewer oder leer für keinen Zugriff. Einer von `admin`, `editor`, `viewer` oder leer. |
| `auth.session-ttl` | `TEAMSTER_AUTH_SESSION_TTL` | `12h` | Wie lange eine Anmeldung gilt. |
| `auth.broker.enabled` | `TEAMSTER_AUTH_BROKER_ENABLED` | `false` | Das eigene Entra-Token jedes Administrators bei Keycloak abholen, um seine eigenen Teams und Kanäle aufzulisten. |
| `auth.broker.idp-alias` | `TEAMSTER_AUTH_BROKER_IDP_ALIAS` | — | Alias des Keycloak-Identitätsanbieters, über den die Entra-Anmeldung föderiert ist. |
| `auth.broker.token-encryption-key` | `TEAMSTER_AUTH_BROKER_TOKEN_ENCRYPTION_KEY` | — | Base64-Schlüssel mit 32 Byte, der gespeicherte Keycloak-Token im Ruhezustand verschlüsselt. |

## Microsoft Graph {#microsoft-graph}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `graph.tenant-id` | `TEAMSTER_GRAPH_TENANT_ID` | — | Mandanten-ID (Tenant ID) in Microsoft Entra. |
| `graph.client-id` | `TEAMSTER_GRAPH_CLIENT_ID` | — | Anwendungs-ID (Client ID) in Microsoft Entra. |
| `graph.client-secret` | `TEAMSTER_GRAPH_CLIENT_SECRET` | — | Client-Secret in Microsoft Entra. |
| `graph.base-url` | `TEAMSTER_GRAPH_BASE_URL` | `https://graph.microsoft.com/v1.0` | Basis-URL der Microsoft Graph API. |
| `graph.timeout-sec` | `TEAMSTER_GRAPH_TIMEOUT_SEC` | `10` | Timeout in Sekunden für Aufrufe der Graph API. |
| `graph.token-url` | `TEAMSTER_GRAPH_TOKEN_URL` | — | OAuth2-Token-Endpunkt. Leer leitet den öffentlichen Microsoft-Entra-Endpunkt für `graph.tenant-id` ab. |
| `graph.scope` | `TEAMSTER_GRAPH_SCOPE` | `https://graph.microsoft.com/.default` | Für Graph angeforderter OAuth2-Scope. |

## Teams-Bot {#teams-bot}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `bot.tenant-id` | `TEAMSTER_BOT_TENANT_ID` | — | Mandanten-ID in Microsoft Entra für die Bot-Registrierung. |
| `bot.client-id` | `TEAMSTER_BOT_CLIENT_ID` | — | Anwendungs-ID (Client ID) in Microsoft Entra für die Bot-Registrierung. |
| `bot.client-secret` | `TEAMSTER_BOT_CLIENT_SECRET` | — | Client-Secret in Microsoft Entra für die Bot-Registrierung. |
| `bot.tenant-type` | `TEAMSTER_BOT_TENANT_TYPE` | `single` | Ob die Bot-Registrierung für einen oder mehrere Mandanten gilt. Einer von `single`, `multi` oder leer. |
| `bot.token-url` | `TEAMSTER_BOT_TOKEN_URL` | — | OAuth2-Token-Endpunkt. Leer leitet ihn aus `bot.tenant-id` und `bot.tenant-type` ab. |
| `bot.scope` | `TEAMSTER_BOT_SCOPE` | `https://api.botframework.com/.default` | Für die Bot Connector API angeforderter OAuth2-Scope. |
| `bot.metadata-url` | `TEAMSTER_BOT_METADATA_URL` | `https://login.botframework.com/v1/.well-known/openidconfiguration` | OpenID-Konfigurationsdokument des Bot Framework, mit dem eingehende Anfragen geprüft werden. |
| `bot.timeout-sec` | `TEAMSTER_BOT_TIMEOUT_SEC` | `10` | Timeout in Sekunden für Aufrufe der Bot Connector API. |
| `bot.service-url` | `TEAMSTER_BOT_SERVICE_URL` | `https://smba.trafficmanager.net/teams/` | Bot-Connector-Endpunkt für ein Team, dessen eigener Endpunkt noch nicht bekannt ist. |
| `bot.global-install` | `TEAMSTER_BOT_GLOBAL_INSTALL` | `false` | Die Teams-App für jedes aktivierte Mitglied des Mandanten installieren, ohne dass sich jemand dagegen entscheiden kann. |
| `bot.app-id` | `TEAMSTER_BOT_APP_ID` | — | ID der Teams-App, die ID aus dem Manifest, mit der die App im Katalog der Organisation gefunden wird. |
| `bot.catalog-app-id` | `TEAMSTER_BOT_CATALOG_APP_ID` | — | Katalog-ID der Teams-App; leer sucht sie über `bot.app-id` oder `bot.client-id`. |
| `bot.reconcile-interval` | `TEAMSTER_BOT_RECONCILE_INTERVAL` | `6h` | Wie oft Installationen auf neue und zurückgekehrte Mitglieder geprüft werden; 0 prüft nur, wenn ein Administrator es anfordert. |
| `bot.reverify-interval` | `TEAMSTER_BOT_REVERIFY_INTERVAL` | `168h` | Wie lange einer Installation vertraut wird, bevor sie erneut geprüft wird. |
| `bot.install-concurrency` | `TEAMSTER_BOT_INSTALL_CONCURRENCY` | `4` | Wie viele Installationen während eines Abgleichs gleichzeitig laufen. |
| `bot.inline-install-budget` | `TEAMSTER_BOT_INLINE_INSTALL_BUDGET` | `5` | Für wie viele Personen eine Nachricht die App installieren darf, bevor sie zugestellt wird. |
| `bot.welcome-message` | `TEAMSTER_BOT_WELCOME_MESSAGE` | — | Wird einmal gesendet, wenn die App für eine Person installiert wird; leer sendet nichts. |
| `bot.directory-ttl` | `TEAMSTER_BOT_DIRECTORY_TTL` | `24h` | Wie lange einer nachgeschlagenen Person vertraut wird, bevor Graph erneut gefragt wird. |
| `bot.pacing.strategy` | `TEAMSTER_BOT_PACING_STRATEGY` | `process` | Wie Aufrufe getaktet werden: `process` gibt jedem Replikat ein eigenes Budget. Einer von `process`. |
| `bot.pacing.rate` | `TEAMSTER_BOT_PACING_RATE` | `20` | Bot-Connector-Aufrufe pro Sekunde für dieses Replikat; teilen Sie das Budget des Mandanten durch die Zahl der Replikate. |
| `bot.pacing.burst` | `TEAMSTER_BOT_PACING_BURST` | `20` | Aufrufe, die dieses Replikat auf einmal machen darf, bevor `rate` greift. |
| `bot.pacing.conversation-rate` | `TEAMSTER_BOT_PACING_CONVERSATION_RATE` | `0.5` | Aufrufe pro Sekunde an eine Unterhaltung. |
| `bot.pacing.conversation-burst` | `TEAMSTER_BOT_PACING_CONVERSATION_BURST` | `7` | Aufrufe an eine Unterhaltung auf einmal, bevor `conversation-rate` greift. |
| `bot.pacing.retries` | `TEAMSTER_BOT_PACING_RETRIES` | `3` | Wie oft ein mit `429` oder `503` abgewiesener Aufruf wiederholt wird; `0` lässt ihn sofort fehlschlagen. |
| `bot.pacing.max-retry-wait` | `TEAMSTER_BOT_PACING_MAX_RETRY_WAIT` | `30s` | Längste Wartezeit vor einer Wiederholung, gleich was `Retry-After` verlangt. |

## Ereignis-Stichproben {#event-samples}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `samples.enabled` | `TEAMSTER_SAMPLES_ENABLED` | `true` | Label-Schlüssel, Label-Werte und Attributschlüssel eingehender Ereignisse für die Vervollständigung im Editor merken. |
| `samples.retention` | `TEAMSTER_SAMPLES_RETENTION` | `720h` | Wie lange eine Stichprobe nach ihrem letzten Auftreten aufbewahrt wird. |
| `samples.max-values-per-key` | `TEAMSTER_SAMPLES_MAX_VALUES_PER_KEY` | `50` | Wie viele der zuletzt gesehenen Werte je Label-Schlüssel aufbewahrt werden. |
| `samples.max-value-length` | `TEAMSTER_SAMPLES_MAX_VALUE_LENGTH` | `200` | Label-Werte, die länger als diese Anzahl Bytes sind, gehen in keine Stichprobe ein. |
| `samples.lru-size` | `TEAMSTER_SAMPLES_LRU_SIZE` | `4096` | Wie viele Stichproben im Speicher gehalten werden, um Schreibvorgänge zusammenzufassen. |
| `samples.flush-interval` | `TEAMSTER_SAMPLES_FLUSH_INTERVAL` | `5m` | Wie lange eine bereits geschriebene Stichprobe wartet, bevor ihr Zähler erneut geschrieben wird. |

## Änderungsprotokoll {#audit-trail}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `audit.file` | `TEAMSTER_AUDIT_FILE` | — | Audit-Ereignisse als JSON-Zeilen an diese Datei anhängen; `-` ist stdout, leer schaltet ab. |
| `audit.database` | `TEAMSTER_AUDIT_DATABASE` | `false` | Audit-Ereignisse in der Datenbank aufbewahren, die die Verwaltungsoberfläche auflistet. Das Audit ist abgeschaltet, solange weder dies noch ein Sink konfiguriert ist. |
| `audit.retention-age` | `TEAMSTER_AUDIT_RETENTION_AGE` | `2160h` | Audit-Ereignisse in der Datenbank, die älter sind, verwerfen; 0 behält sie unabhängig vom Alter. |
| `audit.retention-count` | `TEAMSTER_AUDIT_RETENTION_COUNT` | `100000` | Höchstens so viele Audit-Ereignisse in der Datenbank behalten; 0 bedeutet keine Grenze. |
| `audit.prune-interval` | `TEAMSTER_AUDIT_PRUNE_INTERVAL` | `1h` | Wie oft die Aufbewahrungsregeln für Audit-Ereignisse in der Datenbank angewendet werden. |
| `audit.queue-size` | `TEAMSTER_AUDIT_QUEUE_SIZE` | `1024` | Ereignisse, die für jeden Sink außer der Datenbank vorgehalten werden, bevor neue verworfen werden. |
| `audit.nats.url` | `TEAMSTER_AUDIT_NATS_URL` | — | URL des NATS-Servers, etwa `nats://nats:4222`; leer schaltet ab. |
| `audit.nats.subject-prefix` | `TEAMSTER_AUDIT_NATS_SUBJECT_PREFIX` | `teamster.audit` | Ereignisse werden unter `<prefix>.<resource type>.<action>` veröffentlicht. |
| `audit.nats.stream` | `TEAMSTER_AUDIT_NATS_STREAM` | `TEAMSTER_AUDIT` | JetStream-Stream, der die Subjects erfasst. |
| `audit.nats.create-stream` | `TEAMSTER_AUDIT_NATS_CREATE_STREAM` | `false` | Den Stream beim Start anlegen oder aktualisieren. |
| `audit.nats.creds-file` | `TEAMSTER_AUDIT_NATS_CREDS_FILE` | — | NATS-Credentials-Datei (JWT und NKey-Seed). |
| `audit.nats.timeout` | `TEAMSTER_AUDIT_NATS_TIMEOUT` | `5s` | Timeout für den Verbindungsaufbau und Höchstdauer für das Anlegen des Streams. |
| `audit.nats.backfill` | `TEAMSTER_AUDIT_NATS_BACKFILL` | `false` | Aus dem Änderungsprotokoll der Datenbank statt aus einer Warteschlange veröffentlichen und so nachholen, was anfiel, während NATS nicht erreichbar war; braucht `audit.database`. |
| `audit.nats.backfill-interval` | `TEAMSTER_AUDIT_NATS_BACKFILL_INTERVAL` | `2s` | Wie oft das Nachholen nach zu veröffentlichenden Ereignissen sucht. |
| `audit.nats.backfill-settle` | `TEAMSTER_AUDIT_NATS_BACKFILL_SETTLE` | `5s` | Wie alt ein Ereignis sein muss, bevor das Nachholen es veröffentlicht, damit ein spät festgeschriebenes nicht übersprungen wird. |

## Logging {#logging}

| Schlüssel | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `log.level` | `TEAMSTER_LOG_LEVEL` | `info` | Niedrigste ausgegebene Stufe: debug, info, warn oder error. Einer von `debug`, `info`, `warn`, `error`. |
| `log.format` | `TEAMSTER_LOG_FORMAT` | `text` | Zeilenformat: text zum Lesen, json für eine Log-Pipeline. Einer von `text`, `json`. |

## Prüfungen beim Start {#startup-checks}

`teamster serve` startet nicht, wenn die Konfiguration eine dieser Prüfungen nicht besteht. Die
anderen Befehle führen sie nicht aus.

| Einstellung | Prüfung |
| --- | --- |
| `graph.tenant-id`, `graph.client-id`, `graph.client-secret` | Alle drei sind erforderlich. |
| `admin.username`, `admin.password` | Beide sind erforderlich. |
| `database.path` | Erforderlich, wenn `database.driver` `sqlite` ist. |
| `database.postgres.host` | Erforderlich, wenn `database.driver` `postgres` ist und `database.postgres.url` leer ist. |
| `database.postgres.dbname`, `database.postgres.user` | Erforderlich, wenn `database.driver` `postgres` ist und `database.postgres.url` leer ist. |
| `database.postgres.url` | Nicht kombinierbar mit `database.postgres.host`, `.password` oder `.sslrootcert`. |
| `server.external-url` | Wenn gesetzt, eine absolute `http`- oder `https`-URL. |
| `metrics.*` | Mit `metrics.enabled`: `metrics.prometheus` oder `metrics.otlp-endpoint` ist gesetzt; bei eingeschaltetem Exporter ist `metrics.addr` gesetzt und von `server.addr` verschieden, und `metrics.path` beginnt mit `/`; mit einem OTLP-Endpunkt ist `metrics.otlp-interval` positiv. |
| `bot.tenant-id`, `bot.client-id`, `bot.client-secret` | Ist einer gesetzt, sind `bot.client-id` und `bot.client-secret` erforderlich, außerdem `bot.tenant-id`, sofern `bot.tenant-type` nicht `multi` ist. |
| `bot.timeout-sec` | Positiv, wenn der Bot konfiguriert ist. |
| `bot.metadata-url` | Eine `https`-URL, wenn der Bot konfiguriert ist. |
| `bot.service-url` | Wenn gesetzt, eine `https`-URL. |
| `bot.directory-ttl` | Positiv, wenn der Bot konfiguriert ist. |
| `bot.pacing.*` | Wenn der Bot konfiguriert ist: `rate`, `burst`, `conversation-rate`, `conversation-burst` und `max-retry-wait` sind positiv; `retries` ist nicht negativ. |
| `bot.global-install` | Braucht einen konfigurierten Bot und `bot.app-id` oder `bot.catalog-app-id`. |
| `bot.reconcile-interval` | Mit `bot.global-install`: `0` oder mindestens `5m`. |
| `bot.reverify-interval` | Mit `bot.global-install`: positiv. |
| `bot.install-concurrency` | Mit `bot.global-install`: zwischen 1 und 16. |
| `bot.inline-install-budget` | Mit `bot.global-install`: nicht negativ. |
| `webhook.max-recipients` | Zwischen 1 und 1000. |
| `webhook.fanout-concurrency` | Mindestens 1. |
| `auth.oidc-client-id`, `auth.oidc-redirect-url`, `auth.claim` | Erforderlich, wenn `auth.oidc-discovery-url` gesetzt ist; `auth.oidc-redirect-url` ist eine absolute URL. |
| `auth.broker.*` | Mit `auth.broker.enabled`: `auth.oidc-discovery-url` und `auth.broker.idp-alias` sind gesetzt, und `auth.broker.token-encryption-key` ist Base64 für genau 32 Byte. |
| `samples.*` | Mit `samples.enabled`: `retention`, `max-values-per-key`, `max-value-length`, `lru-size` und `flush-interval` sind positiv. |
| `audit.retention-age`, `audit.retention-count` | Nicht negativ. |
| `audit.prune-interval` | Positiv, wenn `audit.database` eingeschaltet und eine Aufbewahrungsgrenze gesetzt ist. |
| `audit.queue-size` | Positiv, wenn `audit.file` oder `audit.nats.url` gesetzt ist. |
| `audit.nats.subject-prefix` | Mit `audit.nats.url`: nicht leer, keine Wildcards oder Leerraum, kein `.` am Anfang oder Ende. |
| `audit.nats.stream` | Erforderlich mit `audit.nats.create-stream`. |
| `audit.nats.timeout` | Positiv, wenn `audit.nats.url` gesetzt ist. |
| `audit.nats.backfill` | Braucht `audit.database`; `audit.nats.backfill-interval` positiv und `audit.nats.backfill-settle` nicht negativ. |

## Siehe auch {#see-also}

* [Teamster konfigurieren](../../guides/configuration/)
* [CLI](../cli/)
* [Helm-Werte](../helm-values/)
