# 0002. The HTTP layer depends on a messenger interface

* Status: Accepted
* Date: 2026-09-08

## Context

`httpserver.Server` held a `*graph.Client`. The client builds its HTTP transport from
`clientcredentials.Config` with the token endpoint hardcoded to
`https://login.microsoftonline.com/{tenant}/oauth2/v2.0/token`, so constructing one in a test
means either reaching Entra over the network or making the token URL configurable purely for
tests.

That left the alert lifecycle in `processAlert` — post a new card, update an existing one, resolve
and delete — untested, which is the part of the service most likely to break.

## Decision

We will have `httpserver` declare the behaviour it needs and accept that instead of the concrete
client:

```go
type messenger interface {
	PostMessage(teamID, channelID string, card json.RawMessage, summary string) (string, error)
	UpdateMessage(teamID, channelID, messageID string, card json.RawMessage, summary string) error
}
```

`NewServer` takes a `messenger`. `*graph.Client` satisfies it, so `cmd/teamster` is unchanged. The
interface is declared by the consumer and stays unexported, so it cannot grow into a public
abstraction by accident.

The alternative — adding a `TokenURL` field to `GraphConfig` — would expose a setting that exists
only to serve tests, and would still leave the tests dependent on a live OAuth2 exchange.

## Consequences

* The webhook handlers are covered with a fake messenger, including the Graph failure paths.
* `internal/graph` is now free to change its constructor without touching `httpserver`.
* Adding a Graph call that the handlers use means extending the interface as well as the client.
