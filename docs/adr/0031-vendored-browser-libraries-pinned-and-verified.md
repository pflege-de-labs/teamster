# 0031. Vendored browser libraries are pinned in a manifest and verified in CI

* Status: Accepted
* Date: 2026-09-17

## Context

`internal/httpserver/web/vendor` holds `d3.min.js` and `adaptivecards.min.js`, committed rather
than fetched at page load so the binary stays self-contained (see the roadmap's no-CDN constraint).
Until now the only record of what they were was a hand-written Markdown table in that directory's
`README.md`: package, version, license, SHA-256, next to a `curl` recipe with a `<version>`
placeholder to paste into a shell. Nothing checked the table against the files. A checksum that
never matched anything was as good, to the build, as one that did.

Two things followed from that. First, there was no way to notice a file drifting from what the
table claimed — corrupted by a bad merge, hand-edited, or simply never updated when the table was.
Second, Renovate reads dependency manifests, not prose tables, so it had no pin to bump here at
all; refreshing a vendored library was entirely a person's job, from noticing a new release to
running the `curl` recipe to editing the table by hand. This is the blind spot the roadmap's open
question named directly, and it is the same shape of gap [ADR 0020](0020-sqlc-generated-queries.md)
records for `bin/sqlc`: a version pinned somewhere no tool reads is a version nothing keeps honest.

## Decision

We will replace the table with `manifest.json`, machine-readable and the single source of truth:
for each library, its file name, package, version, license, download path and expected SHA-256
sum. The README keeps only prose — what the manifest is for and how to act on it — because two
records of the same versions and sums with nothing keeping them in sync is the drift this change
removes.

**A Go tool under `tools/vendor`, not a shell script with `jq`.** `jq` is not a dependency of this
repository and does not ship with macOS, while Go already is required to build anything here, and
`internal/store/queries/gen` is the existing precedent for a small `go run` developer tool over a
shell script. `go run ./tools/vendor verify` (aliased to `make vendor`) downloads each library at
its pinned version, hashes it, and writes it into place only if the hash matches what the manifest
records — a mismatch is reported by name, version, URL, expected and actual sum, and left alone
rather than silently accepted or silently written over. That doubles as repair: a corrupted or
missing file is restored by the same run that would have caught drift.
`go run ./tools/vendor record` (aliased to `make vendor-record`) is the other half — download at
the pinned version, write it unconditionally, and update the manifest's checksum to match. That is
deliberately the only way the checksum changes; there is no "fetch latest" mode, because
discovering a new version is Renovate's job, not this tool's.

**Verification is a Go test in `httpserver`, following `icons_test.go`, not a shell check.**
`icons_test.go` already exists as the precedent for "a committed copy has to match its source, and
the failure message says which `make` target fixes it." `vendor_test.go` does the same for the
vendor directory: every file's checksum against the manifest, the directory and the manifest
agreeing on what belongs there, the manifest itself being well-formed enough to redo the fetch, the
files actually being served, and — the one check nothing else would catch — that the regex
`renovate.json` uses to read this manifest still matches it. A key reordering or a renamed field
would blind Renovate silently otherwise; this makes that loud in CI instead.

**Renovate gets a `customManagers` regex entry over `manifest.json`, datasourced as `npm`,** the
same move [ADR 0007](0007-renovate-dependency-updates.md) already uses for the Tailwind and sqlc
Makefile pins. It can bump the `version` field because that is text a regex can find and replace.
It cannot download a library or compute a checksum, so the pull request it opens carries a stale
file next to a bumped version number — and everything still agrees with everything else, because
the checksum in the manifest was never touched. A `packageRules` entry scoped to this manager and
this file sets `automerge: false` explicitly, as documentation rather than as the only thing
stopping it, and a `prBodyNotes` entry tells whoever reviews it to run `make vendor-record` on the
branch. The `vendor` CI job exists for the case that instruction is skipped: it runs `make vendor`
against the tip of every branch and fails on any diff, so a bumped version whose file was never
re-recorded fails CI rather than shipping.

Alternatives considered:

* **A shell script with `jq` and `shasum`.** Works on the machines that already have `jq`, and
  nothing else here needs a JSON-manipulation dependency the way the Tailwind and sqlc downloads
  need `curl` and `tar`, which are assumed present already. Go is already a hard requirement to
  build this repository; `jq` is not.
* **Keep the README table, add a script that only checks it.** Leaves two hand-maintained records
  of the same fact. A script that reads Markdown to extract a version and a hex string is more
  fragile than one that reads JSON, for no benefit over replacing the table outright.
* **A "fetch latest" mode in the tool.** Would let a developer bump a version without touching
  Renovate at all, which is exactly the discovery path this decision wants to keep in one place.
  Two ways to learn a new version exist is two ways for them to disagree about which one is current.
* **Let Renovate open the PR and merge it once CI is green.** CI cannot be green: `make vendor`
  would fail on the very PR that needs `make vendor-record` run against it, which is the point —
  automerging a manifest that names the wrong bytes is worse than not automerging it.

## Consequences

A drifted or corrupted vendored file is now caught by `make coverage`'s test run rather than by
whoever notices the renderer behaving oddly in the admin UI. `make vendor` is also how a clean
checkout with the wrong bytes repairs itself, with no separate "clean" step.

Renovate now opens a pull request when `d3` or `adaptivecards` releases a new version, where before
it had no visibility into either. That pull request is deliberately incomplete on its own — the
`vendor` CI job fails until a person runs `make vendor-record`, commits the result, and pushes —
which is the tradeoff for a bot being unable to compute a cryptographic hash.

The README lost its table and its `curl` recipe; anyone following an old link to a specific SHA-256
in prose now finds it in `manifest.json` instead, which is where CI and Renovate both read it too.

Adding a third vendored library is now "add an entry to `manifest.json` and run
`make vendor-record`" rather than "download it, hash it, and edit a Markdown table by hand" —
the tool and the test both work over the list, not over the two files named today.
