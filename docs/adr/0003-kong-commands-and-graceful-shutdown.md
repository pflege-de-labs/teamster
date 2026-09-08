# 0003. Commands run through kong, cancelled by signal

* Status: Accepted
* Date: 2026-09-08

## Context

`main` opened the database, built the Graph client, constructed the HTTP server and called
`ListenAndServe` inline. Two problems followed from that.

Serving was the only thing the binary could do. Planned tooling — listing and editing templates,
destinations and routes from a terminal — had nowhere to attach, and adding it would have meant
hand-dispatching on `os.Args`, which is exactly what ADR 0001 removed from configuration
handling.

Nothing handled SIGINT or SIGTERM either. A container stop killed the process mid-request: the
listener vanished, in-flight webhook deliveries were dropped, and the SQLite handle was closed by
the kernel rather than by `Close`.

## Decision

We will model every operation as a kong command and let kong dispatch it.

* `internal/cli.CLI` holds the command tree. `ServeCmd` is tagged `cmd:"" default:"1"`, so running
  the binary with no command still serves, while `teamster serve` works explicitly and future
  commands sit beside it.
* Configuration stays embedded at the root of the tree, so every command shares the same flags,
  config file and `TEAMSTER_*` variables.
* Commands expose `Run(ctx context.Context, cfg *config.Config) error`. `kong.Context.Run`
  dispatches to it; `ctx` and the parsed configuration are bound by `cli.Run`.
* `main` installs `signal.NotifyContext` for `os.Interrupt` and `syscall.SIGTERM` and passes the
  resulting context in. A second signal restores the default disposition and kills the process.
* `ServeCmd.Run` binds the listener itself, so a bind failure is returned rather than lost in a
  goroutine, and on cancellation calls `http.Server.Shutdown` with a deadline from
  `server.shutdown-timeout` (default 15s). The deadline is derived with `context.WithoutCancel`,
  since the context it comes from is already cancelled.
* Validation moved from `main` into `ServeCmd.Run`, because it checks credentials that only
  serving needs. A future `config list` command must not demand a Graph client secret.

## Consequences

* Adding a command is a struct field with a `Run` method; no dispatch code changes.
* Rolling deployments drain instead of dropping in-flight webhook deliveries, and the database is
  closed properly.
* `main` is nine lines and holds no logic, so the command wiring is covered by tests in
  `internal/cli` instead of being untestable.
* Every command must accept a context and honour cancellation; a long-running command that
  ignores it reintroduces the abrupt-kill behaviour.
