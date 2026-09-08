# 0007. Dependency updates run through Renovate

* Status: Accepted
* Date: 2026-09-08

## Context

Dependency updates were configured with Dependabot: weekly checks of the Go modules, the
workflow actions and the Dockerfile bases. That opens pull requests but merges none of them, so
a patch bump still costs a review cycle, and a quiet week produces a stack of one-line pull
requests that nobody looks at until something breaks.

Releases re-resolve dependencies at build time ([ADR 0006](0006-release-rebuild-sbom-signing.md)),
which makes a stale `go.sum` a release-time surprise rather than a background chore.

## Decision

Renovate replaces Dependabot, driven by a scheduled workflow in this repository rather than the
hosted app, so the configuration and the run history live with the code.

* `renovate.json` groups Go module updates into one pull request and the action updates into
  another, and keeps a dependency dashboard issue as the single view of what is pending.
* Minor and patch Go updates and all GitHub Actions updates automerge. `platformAutomerge` hands
  the merge to GitHub, so it happens only once branch protection is satisfied — the required
  checks, and a review if the branch requires one. Renovate never bypasses those rules.
* Major Go updates, Dockerfile base images and the Go toolchain do not automerge. A major bump can
  change behaviour, and a base image change alters what ships in the release image.
* Vulnerability alerts ignore the weekly schedule and automerge as soon as they pass.
* Actions are referenced by commit sha and base images by digest, so a moved tag cannot change
  what runs in CI or what ships in the image. `helpers:pinGitHubActionDigests` and `pinDigests`
  keep both current, and the readable version stays in a trailing comment. A major bump of either
  is reviewed by hand, because it can rename inputs or change a base layout.

Dependabot's `dependabot.yml` is removed, because running both produces two pull requests for
every update. Dependabot *alerts* are a separate feature and stay enabled.

## Consequences

* Routine updates land without a human in the loop, so CI is what stands between a bad bump and
  `main`. The 75% coverage gate and the linter matter more than they did.
* Two prerequisites, without which the workflow is inert: a `RENOVATE_TOKEN` secret, because pull
  requests opened with the workflow's own `GITHUB_TOKEN` do not trigger CI and automerge would
  wait forever on checks that never run; and *Allow auto-merge* enabled on the repository.
* Whether a human sees an automerged update before it lands is a branch protection setting, not a
  Renovate one. Requiring an approving review on `main` is what makes "automerge after review"
  true.
