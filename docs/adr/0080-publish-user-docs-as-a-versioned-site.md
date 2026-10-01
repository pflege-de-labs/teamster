# 0080. Publish the user documentation as a versioned site

* Status: Accepted
* Date: 2026-10-01

## Context

The README has grown to over 1600 lines. It holds the quick start, every operational guide, the
reference material and the development notes in one page, and more user documentation is spread
over `docs/keycloak.md`, `docs/permissions.md`, the chart README and `config.example.yaml`. A reader
cannot tell which part answers their question, and cannot tell whether what they read describes the
release they run: the README always describes `main`.

The site has to be static, served from GitHub Pages without other infrastructure, and reviewable in
a pull request before it is published.

## Decision

* **Hugo with the Hextra theme.** Hextra is a Hugo module with a prebuilt stylesheet, so a build
  needs only the Hugo binary and Go — no Node toolchain. It brings search and a dark mode. Docsy
  lost: it has a native version menu, but needs Hugo extended, Node and PostCSS to build. MkDocs
  with mike lost because it is a second language toolchain in a Go project.
* **The source lives on its own branches for now.** `pages` holds the documentation of unreleased
  `main`. At each minor release, `pages-vX.Y` is cut from it and carries fixes for that version.
  Neither branch contains Go code, so neither runs the Go CI.
* **One directory per version, built once.** CI publishes to the `gh-pages` branch: `pages` into
  `dev/`, `pages-vX.Y` into `vX.Y/`. A deploy replaces only its own directory, so an old version is
  never rebuilt by a newer Hugo or theme. The root holds `versions.json`, generated from the
  `pages-v*` branches, and a redirect to the newest release. The theme's version menu reads
  `versions.json`, so a version built long ago still lists versions released after it.
* **Versions are minors.** A patch release does not change what a user configures, and fixes to its
  documentation go onto the minor's branch.
* **Pull requests are previewed in public.** A pull request to a docs branch is built into
  `pr-preview/pr-N/` on `gh-pages` and removed when it closes.
* **The README and developer documentation stay on `main`.** Architecture, ADRs, roadmap and
  contribution rules describe the code, not a release, and stay next to it. The site links to them.

## Consequences

* Each user-visible change on `main` needs a second pull request, against `pages`. AGENTS.md lists
  that step; nothing enforces it.
* Cutting a release adds a step: create `pages-vX.Y` from `pages` when tagging `vX.Y.0`.
* Renovate must scan the docs branches as base branches, or their workflows and Hugo never update.
* GitHub Pages serves from the `gh-pages` branch, not from a workflow artifact, because both the
  per-version directories and the previews need to coexist in one published tree.
* Previews of pull requests from forks are not built: the workflow has no write token there.
* The README is not shortened yet. Once the site's content has been reviewed, a later change can cut
  the README down to an overview that links to the site.
