# 0004. Container image built multi-stage onto distroless nonroot

* Status: Accepted
* Date: 2026-09-08

## Context

Teamster is deployed as a long-running service and needs a container image. The build needs the
Go toolchain and the module cache; the runtime needs neither, and shipping them enlarges the
attack surface for no benefit.

Two runtime requirements constrain the base image. The service calls Microsoft Graph and Entra
over TLS, so it needs CA certificates. It also writes a SQLite file, so one directory must be
writable by the process user.

The service must not run as root. That rules out the usual `RUN adduser` in the final stage,
because a minimal base has no shell to run it with.

## Decision

We will build a two-stage image.

* The build stage uses `golang:1.24`, matching the `go` directive in `go.mod`, with BuildKit
  cache mounts for the module and build caches. `CGO_ENABLED=0` produces a static binary — the
  SQLite driver is `modernc.org/sqlite`, which is pure Go, so nothing links against libc.
* The runtime stage is `gcr.io/distroless/static-debian12:nonroot`: no shell, no package manager,
  CA certificates included, and a `nonroot` user (65532) already defined. `USER nonroot:nonroot`
  is set explicitly rather than inherited.
* `/data` is created in the build stage with `install -d -o 65532 -g 65532` and copied across,
  which is how a writable directory gets owned correctly without a shell in the final image.
  `TEAMSTER_DATABASE_PATH` points at it and it is declared a volume.
* Configuration is mounted at `/etc/xdg/teamster/config.yaml`, the system-wide XDG location the
  binary already searches (ADR 0001), so no container-specific configuration path exists.
* `.dockerignore` excludes `config.yaml` and `*.db`, keeping local secrets and state out of the
  build context.

Alternatives considered: `scratch` — smaller, but CA certificates and `/etc/passwd` would have to
be assembled by hand for no meaningful gain over distroless static; and Alpine — a shell and a
package manager in production for convenience we do not need.

## Consequences

* The image contains the binary, CA certificates and an empty data directory, and runs unprivileged.
* Debugging inside the container is not possible; there is no shell. Diagnosis happens through
  logs and the admin API.
* No `HEALTHCHECK` is declared: the image has no shell or HTTP client to run one with, and the
  service exposes no health endpoint yet. Orchestrators should probe an HTTP endpoint themselves.
* The base image must be rebuilt to pick up CA certificate updates, as with any pinned base.
