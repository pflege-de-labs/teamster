# 0001. Configuration through kong with XDG file locations

* Status: Accepted
* Date: 2026-09-08

## Context

Configuration was loaded twice over. `cmd/server/main.go` pre-scanned `os.Args` by hand to find
`--config` before kong parsed the same arguments, and `internal/config` carried a second,
unreferenced `Load()` built on `yaml.Unmarshal`. Defaults lived in a third place, an
`ApplyDefaults` function, so a flag's default was invisible in `--help`.

The search paths were `config.yaml`, `.teamster.yaml` and `~/.config/teamsert/config.yaml` — the
last one misspelled, none of them honouring `$XDG_CONFIG_HOME` or `$XDG_CONFIG_DIRS`, and
system-wide configuration was impossible.

The binary also could not start: the sub-structs of `config.Config` carried no `embed:""` tag, so
`kong.New` failed with `unsupported field type config.ServerConfig`.

## Decision

We will use kong as the single source of configuration.

* `kong.ConfigFlag` provides `--config` / `-c`. Kong loads that file itself, so the hand-rolled
  pre-scan is deleted.
* `kong.Configuration(kongyaml.Loader, config.SearchPaths()...)` supplies the default locations,
  and `kong.DefaultEnvars("TEAMSTER")` binds `TEAMSTER_*` variables.
* Defaults and help text live in struct tags. `ApplyDefaults` and the dead `Load()` are removed;
  `Validate()` stays as the post-parse check for required secrets.
* `config.SearchPaths()` implements the XDG Base Directory Specification with the standard
  library — `$XDG_CONFIG_DIRS` (default `/etc/xdg`), then `$XDG_CONFIG_HOME` (default
  `~/.config`), each below `teamster/config.yaml`, then `./config.yaml`. Kong keeps the value of
  the *last* matching resolver, so the list runs least specific first and `$XDG_CONFIG_DIRS` is
  reversed relative to the spec's own most-preferred-first ordering.

Alternatives considered: keeping the bespoke YAML loader (two code paths that drift, no `--help`
integration), and adding `github.com/adrg/xdg` (a dependency for roughly forty lines of standard
library code).

## Consequences

* One configuration path, visible in `--help`, covered by tests in `internal/config`.
* Nested YAML keys must match the kong flag names, which are hyphenated. Existing files using
  `tenant_id`, `client_id`, `client_secret`, `base_url` or `timeout_sec` must be renamed to
  `tenant-id`, `client-id`, `client-secret`, `base-url` and `timeout-sec`; unrecognised keys are
  ignored silently and startup then fails in `Validate()`.
* A config file value overrides the matching environment variable, because kong runs resolvers
  before env-backed defaults. Command line flags still win over everything.
* Machine-wide deployment via `/etc/xdg/teamster/config.yaml` works without flags.
