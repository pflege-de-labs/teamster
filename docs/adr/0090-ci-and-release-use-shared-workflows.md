# 0090. CI and release use the shared labs workflows

* Status: Accepted
* Date: 2026-10-08

## Context

teamster's CI and release workflows were copied into compactor, tranquila and nats-auth-callout,
and the copies drifted: different action pins, SecObserve uploads that fail the build in one
repository and only warn in another, chart signing in one of four. The shared parts now live in
[pflege-de-labs/github-workflows](https://github.com/pflege-de-labs/github-workflows) as reusable
workflows.

## Decision

teamster calls the shared workflows, pinned by commit sha with the release in a comment, for the
Go checks, the CI image, SecObserve, the release guard, the release image, the release binaries,
chart linting and chart publishing.

What only teamster needs stays in its own workflows:

* The tests, which need Postgres and NATS. A called workflow cannot start service containers, so
  the shared checks compile the tests (`go test -run '^$' ./...`) and the local `test` job runs
  them with coverage, in CI and before a release.
* The vendored-library check, the docs branch a release creates, and the chart's refusal checks,
  which moved from inline workflow script to `scripts/check-chart-rejects.sh` so the shared chart
  lint can run them.

The repository ruleset requires checks named `lint`, `test` and `image`. A called workflow reports
as `<job> / <name>`, so `lint` and `image` are small jobs that pass only when the shared checks and
the image build did. They can go once the ruleset requires the new names.

This amends two decisions:

* [ADR 0006](0006-release-rebuild-sbom-signing.md): images and the release checksums are signed
  by the shared workflow. The certificate names `github-workflows/.github/workflows/<name>.yml` as
  the identity and `pflege-de-labs/teamster` as the repository that ran it; verification checks
  both. Releases up to 0.13.0 keep the old identity. Builds of `main` are now signed as well.
* [ADR 0024](0024-trivy-image-scanning.md): SecObserve also receives the image's SBOMs, and a
  failed upload is a warning in the run summary instead of a failed build. SecObserve reports
  findings; it was never meant to decide whether an image ships.

Also new: a release tag that is not reachable from `main` fails before anything is built, the
release image is run with `--version` to prove the tag was linked in, and the checks include
gofmt, `go mod tidy` and govulncheck. gosec runs nowhere yet; its first run reports 42 findings
that need triage before it can gate.

## Consequences

* A pin bump or a fix to a shared step reaches teamster as a Renovate pull request against the
  github-workflows pin, instead of being repeated in four repositories.
* Anyone verifying a signature has to use the new identity for releases after 0.13.0; the README
  shows both.
* A change to the shared workflows can break teamster's release only through a pin bump, which is
  reviewed like any other dependency.
