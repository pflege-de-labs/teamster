# 0089. Renovate runs as the hosted app

* Status: Accepted
* Date: 2026-10-08

## Context

[ADR 0007](0007-renovate-dependency-updates.md) ran Renovate from a scheduled workflow in this
repository, so its configuration and run history lived with the code. That workflow needs a
`RENOVATE_TOKEN` secret, which is a personal or bot token someone has to own and rotate.

The Renovate GitHub App is now installed on the pflege-de-labs repositories, teamster among them.
With both in place every update is proposed twice.

## Decision

The hosted app runs Renovate, and the scheduled workflow is removed. Everything else in ADR 0007
stands: `renovate.json` is unchanged, with its grouping, automerge rules, digest pinning and
vulnerability alerts.

Keeping the workflow and uninstalling the app for this repository lost: the labs repositories
share one way of running Renovate, and the app's pull requests trigger CI without a token in this
repository.

## Consequences

* `RENOVATE_TOKEN` is no longer used and can be deleted from the repository's secrets.
* Run logs live in the Mend developer portal rather than in this repository's Actions history, and
  a run is no longer started from the Actions tab.
* The schedule in `renovate.json` still decides when pull requests open; the app checks more
  often than the weekly workflow did, so vulnerability alerts arrive sooner.
