---
title: CLI
weight: 2
---

Every command and flag of the `teamster` binary. `teamster <command> --help` prints the same, with
the values the current configuration resolves to.

## Synopsis

```text
teamster [<command>] [flags]
```

| Command | Arguments | Does |
| --- | --- | --- |
| [`serve`](#serve) | — | Run the webhook bridge HTTP server. The default when no command is given. |
| [`export`](#export) | — | Write the configuration to a bundle. |
| [`import`](#import) | `<file>` | Apply a configuration bundle. |
| [`migrate up`](#migrate-up) | — | Apply every pending migration. |
| [`migrate down`](#migrate-down) | — | Roll back the most recent migration. |
| [`migrate status`](#migrate-status) | — | Show which migrations have been applied. |
| [`completion`](#completion) | `<shell>` | Write a shell completion script to stdout. |

## Global flags

Every command accepts these, and every configuration flag.

| Flag | Environment variable | Description |
| --- | --- | --- |
| `-h`, `--help` | — | Show context-sensitive help and exit. |
| `-c`, `--config=FILE` | `TEAMSTER_CONFIG` | Load configuration from `FILE` on top of the XDG locations. |
| `--version` | `TEAMSTER_VERSION` | Print the version and exit. |
| `--<section>-<key>=VALUE` | `TEAMSTER_<SECTION>_<KEY>` | One flag per configuration key, such as `--server-addr` or `--database-postgres-host`. See [Configuration](../configuration/). |

Every command opens the database the configuration names. Only `serve` checks the rest of the
configuration, so the other commands run without Graph, bot or admin credentials.

## Exit status

| Status | Means |
| --- | --- |
| `0` | The command succeeded, or `--help` or `--version` was given. |
| `1` | The command failed, or the arguments did not parse. The error is written to stderr as `teamster: <error>`. |

`SIGINT` and `SIGTERM` cancel the running command. A second signal kills the process.

## serve

```text
teamster serve [flags]
```

Runs the HTTP server until it receives `SIGINT` or `SIGTERM`, then drains requests for up to
`server.shutdown-timeout`.

| Step | Behaviour |
| --- | --- |
| Start | Checks the configuration and refuses to start on a failure; see [Startup checks](../configuration/#startup-checks). |
| Database | Opens it and handles pending migrations as `database.migrate` says: `auto`, `verify` or `off`. |
| Listeners | `server.addr`; also `metrics.addr` when `metrics.enabled` and `metrics.prometheus` are on. |

No flags of its own.

## export

```text
teamster export [-o FILE] [flags]
```

Writes the configuration — templates, destinations, routes, grants and default templates — as a
JSON bundle. It reads the database directly, so it works on a stopped installation.

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `-o`, `--output=FILE` | `TEAMSTER_OUTPUT` | stdout | Write to `FILE` instead of stdout. |

| Behaviour | Detail |
| --- | --- |
| Migrations | Never applied. The export refuses a database with pending migrations, whatever `database.migrate` says. |
| Team and channel names | Not included; the bundle carries the ids. The admin UI's export adds the names. |
| Not exported | Everything else, such as webhook access tokens, Teams V2 endpoints, users, groups, active events and the audit trail. |

## import

```text
teamster import <file> [--mode=merge|replace] [--dry-run] [flags]
```

Applies a bundle written by `export` to the configured database.

| Argument | Description |
| --- | --- |
| `<file>` | Bundle to import; `-` reads stdin. |

| Flag | Environment variable | Default | Description |
| --- | --- | --- | --- |
| `--mode=STRING` | `TEAMSTER_MODE` | `merge` | `merge` upserts what the bundle carries; `replace` also deletes what it does not mention. |
| `--dry-run` | `TEAMSTER_DRY_RUN` | `false` | Report what would change without changing it. |

| Behaviour | Detail |
| --- | --- |
| Migrations | Handled as `database.migrate` says. |
| Audit | Each change is recorded in the configured audit sinks, with the operating system user as the actor. Events for NATS wait in the database trail for a running server to publish. |
| Output | One line per change: action (`create`, `update`, `delete`), kind, name and id. Then `applied <n> changes in <mode> mode`, or `would apply …` with `--dry-run`. `nothing to change` when the bundle matches. |

## migrate

Opens the database without migrating it, whatever `database.migrate` says, and changes the schema
only as the subcommand asks.

### migrate up

```text
teamster migrate up [flags]
```

Applies every pending migration. Prints `applied <version> <source> in <duration>` for each, or
`already up to date`.

### migrate down

```text
teamster migrate down [flags]
```

Rolls back the most recent migration, one per run. Prints
`rolled back <version> <source> in <duration>`.

### migrate status

```text
teamster migrate status [flags]
```

Prints one row per migration.

| Column | Holds |
| --- | --- |
| `VERSION` | Migration number. |
| `STATE` | Whether it is applied or pending. |
| `APPLIED` | When it was applied, `YYYY-MM-DD HH:MM:SS`, or `-`. |
| `SOURCE` | The migration file, or `go migration, registered in code`. |

## completion

```text
teamster completion <shell> [flags]
```

Writes a completion script for the whole command tree to stdout.

| Argument | Values |
| --- | --- |
| `<shell>` | `bash`, `zsh`, `fish` |

```bash
teamster completion bash > /etc/bash_completion.d/teamster
teamster completion zsh  > "${fpath[1]}/_teamster"
teamster completion fish > ~/.config/fish/completions/teamster.fish
```

In bash, an enum flag completes to the value of its environment variable rather than to the values
it accepts. zsh and fish offer the accepted values.

## See also

* [Configuration](../configuration/)
* [Back up and move the configuration](../../guides/backup-and-migration/)
* [Choose and run storage](../../guides/storage/)
