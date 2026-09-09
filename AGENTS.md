# Application

Teamster is a Go webhook bridge that routes alerts to Microsoft Teams.

## Key functionality

* Receives webhooks from Prometheus Alertmanager or a generic universal format via
  POST /webhook/alertmanager and POST /webhook/universal
* Routes alerts using label selectors (highest priority match wins, with a default fallback)
* Posts to Microsoft Teams by sending Adaptive Cards through the Microsoft Graph API to configured
  Team channels
* Tracks active alerts in SQLite to update or resolve existing cards when alert state changes
* Admin UI at /admin for managing:
  * Templates (Adaptive Card JSON with Go templating)
  * Destinations (Team/Channel IDs)
  * Routes (label selector → destination + template mappings)

## Definition of done

A change is done when all of these hold:

* `make coverage` passes — total statement coverage is at least 75%
* `make lint` is clean (`golangci-lint`, `gofmt`, `goimports`)
* [Architecture documentation](docs/architecture.md) reflects the change
* An [ADR](docs/adr/) exists for every architectural decision the change makes
* `README.md` and the user-facing documentation match the behaviour that shipped

## Development workflow

* Install the git hooks once per clone with `make hooks`. They format Go with the settings from
  `.golangci.yml`, lint Markdown, and reject a commit message that is not a Conventional Commit.
* Commits follow [Conventional Commits](https://www.conventionalcommits.org): `feat:`, `fix:`,
  `docs:`, `refactor:`, `test:`, `chore:`, `build:`, `ci:`. Breaking changes carry a `!` before
  the colon or a `BREAKING CHANGE:` footer.
* Features are developed in branches. Never commit to `main` directly.
* Check a feature branch out with `git worktree`, rather than switching the branch of an existing
  clone:

  ```bash
  git worktree add ../teamster-<topic> -b <branch> origin/main
  git worktree remove ../teamster-<topic>   # once the branch is merged
  ```

  Switching in place pulls the ground out from under whatever is using that working tree — a
  running server, a live-reload watcher, an open editor. Gitignored files stay behind in the clone
  that created them, so a fresh worktree has no `config.yaml` and no database; copy
  `config.example.yaml` or point `--config` at an existing file before running the server there.
* Branches are merged into `main` via pull request; the PR checklist mirrors the definition of
  done above.
* Dependency updates arrive as Renovate pull requests. Minor and patch Go bumps and action
  updates merge themselves once branch protection is satisfied; majors, base images and the Go
  toolchain are reviewed by hand.
* Workflow actions are pinned to a commit sha and container bases to a digest, with the readable
  version in a trailing comment. Never reintroduce a floating tag; Renovate does the updating.
* Releases are cut from `main` by tagging `vX.Y.Z`, which triggers the release workflow: it
  re-runs lint and tests on the tagged commit, rebuilds the image and binaries, attaches SBOMs
  and signs everything with cosign.

## Go conventions

* `github.com/alecthomas/kong` handles the CLI and configuration. Config file loading uses the
  matching `github.com/alecthomas/*` implementation — `kong.ConfigFlag` for the config file flag
  and `kong.Configuration` with `kong-yaml`. Never hand-roll argument scanning or YAML loading.
* Config file locations follow the XDG Base Directory Specification, see
  `internal/config.SearchPaths`. YAML keys must match kong's hyphenated flag names.
* Flag defaults and help text live in struct tags, not in Go code.
* Every operation is a kong command in `internal/cli` with a
  `Run(ctx context.Context, cfg *config.Config) error` method, dispatched by `kong.Context.Run`.
  Commands take a context and must return promptly once it is cancelled.
* Code is `gofmt` and `goimports` clean; `golangci-lint run` reports no issues.
* The admin UI is server-rendered from templ components in `internal/httpserver/views`. Edit the
  `.templ` sources, never the generated `*_templ.go`, and run `make generate` — the generated Go
  and the Tailwind stylesheet are committed so that building needs no generators.
* State-changing form endpoints go through `formPost`, which rejects a request that cannot prove
  its origin. Basic auth credentials travel with a cross-site post.
* Tests are table-driven and call `t.Parallel()`. Tests that need `t.Setenv` cannot be parallel —
  keep those cases in their own test function.
* No package-level mutable state. Dependencies are injected through constructors, as in
  `httpserver.NewServer(cfg, store, graphClient)`.

## Planned work

Upcoming features and their intended order live in [docs/roadmap.md](docs/roadmap.md). A feature is
designed before it is implemented: a design note in its pull request, an ADR when it changes how
components are structured, and one feature per pull request. Update the roadmap when a milestone
lands or the plan changes.

## Architecture decisions

Recorded as ADRs in [docs/adr/](docs/adr/), numbered `NNNN-title.md` and copied from
[0000-template.md](docs/adr/0000-template.md). An accepted ADR is never rewritten — supersede it
with a new one and update the old status line.
