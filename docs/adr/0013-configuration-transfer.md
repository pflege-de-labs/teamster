# 0013. Move a configuration as a versioned bundle

* Status: Accepted
* Date: 2026-09-11

## Context

There was no way to move a configuration between installations, or to back one up, other than
copying the SQLite file. That file carries sessions, login flows and active alert state along with
the configuration, so it is both more than a backup should hold and useless as a way of copying
templates from a staging installation to a production one.

Two related needs: a backup an operator can keep beside the rest of a deployment's configuration,
and a way to move templates, destinations, routes and permissions from one installation to another.

## Decision

We will treat the configuration as a **bundle**: one JSON document carrying templates,
destinations, routes and grants, with a format version and an export timestamp.

**What is not in it** is as much of the decision as what is. No webhook token, no Graph client
secret, no admin password: a bundle is meant to live in a repository next to the deployment that
uses it, and a backup that cannot be stored safely is one that does not get taken. Sessions, login
flows and active alerts are runtime state and are absent for the same reason a database dump makes a
poor migration.

**Ids are preserved**, so a bundle re-imported into the installation it came from is a no-op rather
than a second copy of everything.

`GET /api/config/export` and `POST /api/config/import` are the endpoints, and `teamster export` and
`teamster import` are kong commands over the same `internal/transfer` code — which is what the
command structure in [ADR 0003](0003-kong-commands-and-graceful-shutdown.md) was for. The commands
open the database directly rather than talking to a running server, so they work on a stopped
installation, which is when a backup is most often wanted.

Import takes a mode: `merge` upserts what the bundle carries and leaves the rest alone, `replace`
makes the installation match the bundle. Both **validate the whole bundle first** — the version,
duplicate ids, references pointing outside the bundle, cycles in the route tree — so a bundle that
will not apply changes nothing.

References must resolve **within the bundle**. Leaning on what happens to be in the database already
would make the same bundle behave differently on a fresh installation, which is exactly the case
this feature exists for.

**A dry run runs the real import and rolls it back**, rather than computing a diff a second way. A
preview produced by different code is a preview of something else.

That requires a transaction, so `store.Store` gains `WithTx(func(Store) error) error`. The SQLite
implementation runs its statements against a `queryer` — the database, or a transaction while one is
open — so one set of methods serves both. Inside a transaction, `Close`, `Ping` and a nested
`WithTx` are refused: each would be operating on a handle that is not there.

Import and export are `Action::"administer"`. An export is the whole configuration in one file, and
an import rewrites it, including who may deliver where.

## Consequences

A configuration can be reviewed in a pull request, kept in a repository, and moved between
installations. `make image-run` against a fresh volume plus one `teamster import` is now a complete
restore.

Team and channel ids belong to one tenant. A bundle carried to another imports cleanly and then
delivers nowhere, so the bundle carries Team and channel **names** beside the ids, and the
server-side import names the destinations this tenant cannot resolve. It asks Graph to find out and
says nothing when Graph is unreachable: an unreachable directory is not evidence that a channel is
missing. The CLI import does not make that check, because it has no reason to hold Graph
credentials.

The added `WithTx` is the first thing in `store.Store` that is about how writes are grouped rather
than what is written. A second implementation of the interface will have to provide it — which
[a future backend other than SQLite](../roadmap.md) would have anyway.

Grants are imported as they are. A bundle from an installation whose roles are named differently
will therefore carry grants naming roles that do not exist there; they are inert rather than wrong,
and `replace` removes the ones the bundle does not mention.
