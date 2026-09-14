# 0022. Postgres is the second storage backend

* Status: Accepted
* Date: 2026-09-14

## Context

Everything up to here made one instance correct: a context that reaches the database, a schema with
a version ledger, generated queries, a claim that stops two concurrent deliveries posting two
cards, and invariants the database holds rather than the code above it. None of it needs a second
instance, and none of it provides one — SQLite takes a single writer, so the deployment is one pod
whatever the code does.

Running more than one instance means the state has to live somewhere several processes can write at
once. That is the whole of what this ADR decides.

## Decision

We will add Postgres as a second backend, chosen by `database.driver`, with SQLite remaining the
default.

**The driver is named, never inferred.** The container image sets `TEAMSTER_DATABASE_PATH`
unconditionally, so an operator who configures only a Postgres host and lets the code work out what
they meant would get a service that starts, works, and writes everything to a file that dies with
the container. `store.Open` switches on the name and nothing else, `config.Validate` checks what
the named driver needs and deliberately ignores the other's settings, and startup logs which
database it actually opened.

**Discrete connection settings, not a DSN.** [ADR 0001](0001-kong-xdg-configuration.md) makes a
config file value beat an environment variable, so a DSN in the file would carry the password with
it and a DSN in the environment would mean the chart could render none of host, port, database or
sslmode. Discrete fields leave exactly one secret-bearing key, which is the shape the webhook
token, admin password and client secrets already have. A `url` escape hatch remains for what the
fields cannot say — a pooler, a `target_session_attrs` — and is mutually exclusive with them.

**One adapter, two dialects.** The plan for this work budgeted for a second hand-written adapter of
around five hundred lines, on the grounds that Go has no structural satisfaction across packages
and sqlc's two generated `Querier` interfaces can therefore never be one type. That is true, but
the parameter and row structs are generated from the same queries and are field for field
identical, which makes them *convertible*. So the mapping between rows and `internal/models` — all
the behaviour — is written once in `queryAdapter`, and the cost of the second backend is
`pgqueries.go`: one conversion per statement, and a compiler error the moment the two shapes stop
matching.

The Postgres query files are likewise generated from the SQLite ones by
`internal/store/queries/gen`, because the statements differ only in how a parameter is spelled.
Generating rather than maintaining two copies makes drift impossible instead of merely detectable.
If a statement ever has to differ for real, its file comes out of the generator with a comment
saying why.

**`pgx` in `database/sql` mode.** It is maintained and pure Go, so [ADR 0004](0004-container-image.md)'s
`CGO_ENABLED=0` premise holds. `database/sql` rather than pgx's own interface is what makes both
generated packages emit the same `DBTX` and the same `WithTx(*sql.Tx)`, and therefore what makes
one adapter possible.

**What Postgres adds that SQLite did not need.** A bounded pool with a connection lifetime, because
the real limit is the server's `max_connections` divided by the number of instances and an
unbounded pool discovers that during an incident. `statement_timeout`, `lock_timeout` and
`idle_in_transaction_session_timeout` on the session, which turn "no network I/O inside a
transaction" from an agreement into something the database enforces. Isolation asked for by name,
with `WithSerializableTx` meaning what it says instead of being free. A retry, bounded at three
attempts with jittered backoff, for the three SQLSTATEs that mean *the database gave up on this
attempt, not on the work* — serialization failure, deadlock, and the unique violation a concurrent
insert can raise. And goose's Postgres session lock, so two instances starting together cannot both
migrate.

**Testing.** The behavioural tests run against both backends from one table. A Postgres run needs a
server, so it skips when `TEAMSTER_TEST_POSTGRES_DSN` is unset — and **fails** when that variable is
unset in CI, because a skip is how a whole backend quietly goes untested. Each test gets its own
schema, which keeps `t.Parallel()` and makes every test a migration test as well. `make db-up`
starts a server with whatever container tool is present.

Alternatives considered:

* **MySQL as well.** A third dialect doubles the review surface of every schema change for nobody
  who has asked.
* **A remote database instead of SQLite.** The single-binary install with no runtime dependency is
  worth keeping, and it is what most deployments of this service are.
* **`pgxpool` and pgx's native interface.** Faster in ways this workload will never notice, and it
  would have forced the two adapters the shared one avoids.

## Consequences

`database.driver=postgres` with several replicas works, and the claim protocol behaves the same on
both engines — twenty concurrent deliveries of one alert across two instances sharing one Postgres
produced one card, one row, and nineteen refusals.

Both drivers compile into every binary. One image, one set of release binaries, one SBOM, one
signature; the deployment picks the driver rather than the download. The cost is a few megabytes in
a binary that a single-node SQLite install will never use.

Coverage is 86.9% without a Postgres to test against and 88.4% with one — the shared adapter is why
the gap is small enough not to matter.

Moving an existing installation is the export/import bundle from
[ADR 0013](0013-configuration-transfer.md) and nothing new. What the bundle does not carry is
sessions, login flows and active alerts, so everyone signs in again and any alert firing at cutover
has a card the new instance has never heard of.

What this does not decide is how the chart deploys it, which is the next change, and read replicas,
which nothing routes anything to.
