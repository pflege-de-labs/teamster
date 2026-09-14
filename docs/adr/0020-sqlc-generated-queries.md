# 0020. Queries are generated from SQL by sqlc

* Status: Accepted
* Date: 2026-09-14

## Context

Every statement was a Go string literal with `?` placeholders, and every read was a hand-written
`Scan` into fields in the right order. That works until it does not: a column added to a `SELECT`
without a matching `Scan` argument, a `Scan` order that no longer matches the projection, a
parameter list one item short. None of it is visible to the compiler. The tests catch most of it,
which is another way of saying the type system catches none of it.

The immediate pressure is a second backend. Two hand-written implementations of the same 26 methods
would each carry their own copy of every projection and every scan order, and the two would drift —
silently, because nothing compares them.

There is also a smaller irritation the same change removes. SQLite has no boolean type, so
`routes.greedy` and `routes.is_default` are `INTEGER`, written through a `boolToInt` helper and read
back with `== 1` in four places. Every one of those is a chance to invert a flag.

## Decision

We will generate the query code with [sqlc](https://sqlc.dev) from `.sql` files, and commit the
output — the same bargain [ADR 0008](0008-templ-tailwind-admin-ui.md) makes for templ, and for the
same reason: the Dockerfile runs no generators.

Three choices inside that are worth recording.

**The schema input is the goose migration directory, not a copy of the schema.** sqlc understands
`-- +goose` annotations, so it can read the files that actually run. A separate `schema.sql` would
be a second source of truth that nothing keeps in step; pointing sqlc at
`internal/store/migrations/sqlite` makes it structurally impossible for the types to be checked
against a schema no database has.

**`sql_package: database/sql`, not `pgx/v5`.** sqlc's Postgres examples default to pgx, which emits
`pgx.Tx`, `pgtype.Timestamptz` and a `DBTX` that is not `database/sql`-shaped. Both dialects on
`database/sql` emit the same `DBTX` and the same `WithTx(*sql.Tx)`, so the two adapters have the
same shape and `WithTx` is written once per backend rather than twice in different styles.

**A column override maps `greedy` and `is_default` to `bool`.** Changing the declared type would
mean rebuilding the table on every installation; the override costs nothing and `database/sql`
converts 0 and 1 in both directions. `boolToInt` and the four `== 1` reads are deleted.

Two structural changes come with it. The `db == nil` sentinel that meant "inside a transaction" —
checked by three methods and silently trusted by the other twenty-three — is replaced by types: a
`sqliteQueries` both the store and its transaction view embed, and a `sqliteTx` that refuses
`Close`, `Ping` and a nested `WithTx` because it does not have a database to do them with. And
`policy.go` collects the five rules a second backend would otherwise re-derive: what an empty id
means, which clock stamps a row, when an existing row still counts as missing, and how a selector
survives corruption.

sqlc is a downloaded binary pinned in the Makefile, like the Tailwind CLI, and deliberately **not**
a `tool` directive: it depends on a cgo SQL parser that a tool directive would pull into `go.sum`
and make `go mod download` fetch on every image build.

Alternatives considered:

* **Keep hand-written SQL.** No new tooling, and it leaves the drift between two backends as a
  thing reviewers are asked to notice by eye.
* **An ORM.** Answers a question this codebase does not have: the queries are simple, the schema is
  small, and the value wanted here is a compiler that reads SQL, not one that writes it.
* **Generate one dialect and rewrite placeholders at the driver layer.** Halves the generated code
  and throws away the type checking that is the entire point.

## Consequences

A query and its Go now cannot disagree. Adding a column to a `SELECT` without regenerating fails to
compile, and a mistyped column name fails at `make generate` rather than in production.

The behaviour is unchanged, and the existing store suite is the evidence: it passed unmodified on
the first run against the generated queries.

The cost is real and worth stating. Editing SQL now means running `make generate`, and an edit that
is not regenerated compiles perfectly while running the old statement — invisible to review and to
the compiler both. That is exactly why the CI job that regenerates and diffs went in first; without
it this change would introduce a silent failure mode rather than remove one.

Two adapters remain unavoidable when the second backend lands: Go has no structural satisfaction
across packages, so `sqlitedb.Querier` and a Postgres equivalent can never be one interface even
though the methods line up. What `policy.go` prevents is the two adapters disagreeing about
anything other than SQL dialect.

`bin/sqlc` is a hand-bumped version in the Makefile, which Renovate cannot see — the same gap the
Tailwind pin already has.
