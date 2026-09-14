# 0019. The schema is a numbered set of goose migrations

* Status: Accepted
* Date: 2026-09-14

## Context

The schema was a Go string of `CREATE TABLE IF NOT EXISTS` executed on every open, followed by
three passes of runtime inspection: `ALTER TABLE` for whichever columns a `PRAGMA table_info`
lookup said were missing, a rebuild of `active_alerts` if its primary key was still the old
single-column one, and a refusal to open a database whose timestamp columns were declared anything
but `DATETIME`.

It worked, and it had no ledger. Nothing recorded which version a database was at, so every start
re-derived the answer by looking. That has three costs. There is no way to ask what state a
database is in without starting the service. A migration that needs to happen once — a backfill, a
data fix — has nowhere to live, because everything in that function runs on every open forever.
And with more than one instance the inspection is a race: two processes starting together both
find the column missing and both add it, and worse, both reach the `active_alerts` rebuild, which
is `CREATE`, `INSERT SELECT`, `DROP`, `RENAME`. That is the one place in this codebase where a
race can destroy data.

The complication is what is already in the field. Three populations exist: databases that do not
exist yet, databases a recent build has already converged — carrying the current schema and no
ledger — and genuinely old ones. The obvious approach, replaying history as numbered `ALTER TABLE`
steps, breaks the middle population: SQLite has no `ADD COLUMN IF NOT EXISTS`, so adding
`templates.title` to a database that already has it fails, and goose offers no way to stamp a
version without running the migration.

## Decision

We will use `github.com/pressly/goose/v3`, with the migrations embedded in the binary and applied
through the provider API rather than goose's package-level one. The global API is process-wide
mutable state, which this project does not allow and which two stores open in one test would
fight over; `goose.NewProvider` carries the same state per instance and
`WithDisableGlobalRegistry(true)` makes the choice explicit.

Two migrations, and the shape of them is the interesting part:

* **`0001_baseline.sql`** is the converged schema — every table with every column a later release
  had added, written with `IF NOT EXISTS`. On a fresh database it is the whole schema; on one an
  earlier build already converged it is a no-op.
* **`0002`** is a Go migration holding the old inspection code, moved rather than rewritten:
  add-if-missing, rebuild-if-old-key, refuse-if-wrong-type. It is idempotent by construction,
  because that is what it was before. On the first two populations it looks and does nothing; on
  the third it does the work it always did. Then it is recorded, and never runs again.

After this, `PRAGMA` appears nowhere in the codebase and every future change is an ordinary
numbered file.

`teamster migrate up|down|status` exists so that applying a schema change can be a step somebody
runs and watches. `database.migrate` chooses what opening the store does: `auto` applies what is
missing and is the default, so a single instance behaves exactly as it did; `verify` refuses to
serve a database that is behind, naming the command that fixes it; `off` asks no questions.
`teamster export` always verifies rather than migrating — a command that reads must not be the
thing that changes a schema, least of all against an installation somebody else is upgrading.

We also adopt a rule, recorded in `AGENTS.md`: **a migration must leave the previous release able
to run against the new schema.** Migrations run before new pods do, so the old release is the one
that meets the new schema first.

Alternatives considered:

* **Keep converging by inspection.** Free, and it is the thing that cannot be made safe for two
  processes, cannot record a one-time fix, and cannot answer "what version is this database".
* **Replay history as numbered `ALTER TABLE` files.** The textbook approach, and it fails on every
  database currently in production, which is the population that matters most.
* **A hand-rolled `schema_version` table.** A migration runner is a solved problem with edge cases
  — advisory locking, out-of-order versions, partial application — that are not worth rediscovering.

## Consequences

A database now says what version it is, and `teamster migrate status` answers it without starting
the service. A one-time data fix has somewhere to live. The `active_alerts` rebuild runs once,
recorded, instead of being re-decided on every start by every process.

Migrations are still applied by the process by default, so nothing changes for a single-node
install: it starts, it migrates, it serves. What changes is that this is now a choice with a name,
and a deployment that runs more than one instance can move it out of the startup path entirely.

The version ledger adds a `goose_db_version` table to every database, including existing ones,
which the export bundle does not carry and does not need — a bundle is configuration, and the
schema it lands in is whatever the receiving installation has migrated to.

Rolling back is one migration at a time and `0002` has no down: the convergence it performs cannot
be meaningfully undone, and goose records the version regardless. Rolling back past `0001` drops
every table, which is why it takes two commands rather than one.

This decision does not address two processes racing on Postgres, where SQLite's whole-database
write lock does not exist. That needs an advisory lock, and it belongs with the backend that needs
it.
