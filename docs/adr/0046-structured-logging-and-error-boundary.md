# 0046. Log through slog and answer errors at one boundary

* Status: Accepted
* Date: 2026-09-29

## Context

Teamster logged with `log.Printf`: no levels, no fields, one format. Several failures that decide
whether anything works logged nothing at all — a Graph install check that finds no app, and most
refusals on `/bot/messages` (no bearer, a serviceurl or endorsement mismatch, an ignored activity
shape). Diagnosing "the app is missing" took reading the code.

The other way round, handlers wrote `err.Error()` straight into responses. A 500 or 502 from the
admin API, a webhook, a page or a form redirect carried the store's or Graph's own error text,
response bodies of upstream calls included. That tells a client how the server is built and still
leaves the log without the failure.

The request log line also printed the full path, which for `/teamsv2/{team}/{channel}/{token}` is
the sender's secret.

## Decision

We will log through `log/slog`, configured by `log.level` (debug, info, warn, error) and
`log.format` (text, json). `ServeCmd` builds one logger and injects it into `httpserver.NewServer`
and `samples.New`; it also calls `slog.SetDefault` once, so the standard library's and
dependencies' own output uses the same handler. That is the only global it touches.

The logging middleware gives every request an id, sends it as `X-Request-ID`, and puts a logger
carrying `request_id` into the request context (`internal/logging.WithLogger`). Code on a request
path logs through `logging.FromContext`. The middleware writes one `request` line per request —
method, path with the Teams V2 token cut off, status, duration — at info, and at debug for
`/healthz` and `/readyz`.

Handlers answer errors at one boundary:

* `writeError` for JSON. A 4xx carries err's own message: it is ours, written for the caller. A 5xx
  is logged at error with the cause, and the body is only the status text and the request id. The
  one exception is a missing bot, which the sender's operator has to fix here and which the README
  already promised to name.
* `visibleError` and `failureText` for pages and form redirects. An error wrapped in `userError`,
  or one of a short list of sentinels (`store.ErrNotFound`, `errDeliveryRefused`, `invalidRoute`,
  …), is shown as it is. Anything else is logged and replaced by "Something went wrong. Reference:
  <request id>", translated.

`transfer.Import` marks a bundle the caller has to fix as `transfer.InvalidError`, so a store
failure during an import is a 500 rather than a 400 carrying the database's message.

Refusals are warnings, failures of ours are errors, a Graph lookup that finds no app is info.

Alternatives:

* Keep `log.Printf` and only add lines. Cheapest, but no level to quiet probes or turn up detail,
  and no machine-readable format for a log pipeline.
* zap or zerolog. Faster, but slog is in the standard library and fast enough for a service that
  logs a line per request.
* Generic bodies for every 4xx. It hides nothing that is not ours, and the admin UI would lose
  messages such as "priority must be a number".

## Consequences

* 5xx response bodies change from the underlying error to `{"error":"…","request_id":"…"}`. Anything
  that parsed the old text has to match on the status, and look up the request id in the log.
* Log lines change shape. `log.format: json` is the one to parse; text is for reading.
* A new validation error in a form handler has to be wrapped in `userError`, or it reaches the page
  as "Something went wrong". Tests catch the ones that exist; review has to catch new ones.
* Debug level includes every 4xx and every accepted bot activity. That is too much for normal
  operation, which is why info is the default.
