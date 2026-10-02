---
title: Choose and run storage
weight: 4
---

Teamster keeps its configuration and the state of open alerts in a database. This guide helps you
pick SQLite or Postgres, configure it, run schema migrations, and move from one to the other.

## Pick a backend

| `database.driver` | What it is |
| --- | --- |
| `sqlite` (default) | One file, no other runtime dependency, and exactly one instance: SQLite takes a single writer. |
| `postgres` | Several instances sharing one database. |

Several instances means a few replicas behind one Service, each able to take any request. There is
no queue, no leader election and no sharding.

The settings of the driver you did not pick are ignored, so both blocks can stay in the config file
while you move between them.

## Use SQLite

SQLite needs only a path:

```yaml
database:
  driver: sqlite
  path: "/data/teamster.db"
```

`database.path` (`TEAMSTER_DATABASE_PATH`) defaults to `teamster.db` in the working directory. The
container image sets `TEAMSTER_DATABASE_PATH=/data/teamster.db`; mount a volume at `/data`.

With the Helm chart, the chart owns `database.path`, runs a StatefulSet with a volume claim, and
refuses `replicaCount` above 1. `persistence.enabled=false` runs on an `emptyDir`, which loses all
state when the pod restarts.

## Use Postgres

Configure Postgres with discrete settings, and deliver the password through the environment:

```yaml
database:
  driver: postgres
  postgres:
    host: pg.internal
    port: 5432
    dbname: teamster
    user: teamster
    sslmode: require
```

```bash
export TEAMSTER_DATABASE_POSTGRES_PASSWORD='<password>'
```

{{< callout type="warning" >}}
Do not put the password in the config file, not even as `password: ""`. A config file value wins
over the environment variable.
{{< /callout >}}

`sslmode` defaults to `require`. `verify-ca` and `verify-full` check the server against the CA file
in `database.postgres.sslrootcert`. A managed Postgres usually needs this, because the container
image carries only the public roots, not the provider's own.

For what the fields cannot say, such as a connection pooler or `target_session_attrs`, set a full
connection URL instead. It carries the password, so deliver it as
`TEAMSTER_DATABASE_POSTGRES_URL`. Teamster refuses a URL combined with `host`, `password` or
`sslrootcert`.

### Size the connection pool

| Key | Default |
| --- | --- |
| `database.max-open-conns` | `0`: 10 for Postgres, unbounded for SQLite |
| `database.max-idle-conns` | `0`: 5 for Postgres |
| `database.conn-max-lifetime` | `0s`: no limit |
| `database.connect-timeout` | `10s` to the first connection at startup |

Before raising `max-open-conns`, divide the server's `max_connections` by the number of instances.

### Run Postgres with the Helm chart

The chart does not deploy a Postgres. Point it at one, and read the password from the secret your
Postgres operator created:

```yaml
database:
  driver: postgres
  postgres:
    host: teamster-pg-rw
    passwordFrom:
      secretName: teamster-pg-app
      key: password
replicaCount: 3
```

Under `postgres`, the chart runs a Deployment and rolls replicas with a surge pod. See the
[Helm values reference](../../reference/helm-values/).

## Run schema migrations

The schema is versioned. By default, starting the server applies whatever is missing, so an upgrade
needs no extra step. On Postgres, an advisory lock keeps two instances starting together from both
applying migrations.

To run migrations by hand:

```bash
teamster migrate status      # what has been applied, and what has not
teamster migrate up          # apply everything pending
teamster migrate down        # roll back the most recent migration
```

These commands read the same configuration as the server. `migrate down` rolls back one migration
per call, and cannot bring back data a migration dropped.

`database.migrate` (`TEAMSTER_DATABASE_MIGRATE`) decides what the server does with a database that
is behind:

| Value | What happens |
| --- | --- |
| `auto` (default) | Apply the missing migrations. |
| `verify` | Refuse to start, naming `teamster migrate up`. |
| `off` | Open the database as it is. |

Set `verify` when a schema change should be a step you watch: a deployment that runs
`teamster migrate up` before rolling out the new version, or one whose database user may not change
the schema. With the Helm chart, set `config.settings.database.migrate=verify` and run the command
from a Job in `extraObjects`; the chart has no migration hook.

`teamster export` never migrates, whatever the setting. It refuses a database with pending
migrations instead.

## Move from SQLite to Postgres

{{< callout type="warning" >}}
Only the configuration moves: templates, destinations, routes and permission grants. Sessions,
login flows and open alerts stay behind. Everyone signs in again, and an alert open during the cut
over gets a second card in Teams; its close never edits the first. Close what you can first.
{{< /callout >}}

{{% steps %}}

### Export the configuration

With the old configuration still pointing at SQLite:

```bash
teamster export -o teamster.json
```

### Point the configuration at Postgres

Set `database.driver: postgres` and the `database.postgres` settings, as in
[Use Postgres](#use-postgres).

### Create the schema

```bash
teamster migrate up
```

### Import the bundle

```bash
teamster import teamster.json
```

### Start Teamster

Start the server against Postgres, and scale it out if you want more replicas.

{{% /steps %}}

The bundle and its limits are covered in [Back up and move the configuration](../backup-and-migration/).
