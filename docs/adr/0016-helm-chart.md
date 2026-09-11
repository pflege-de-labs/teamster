# 0016. The Helm chart ships in this repository and deploys a single SQLite writer

* Status: Accepted
* Date: 2026-09-11

## Context

Teamster was deployable by hand: an image, a config file mounted at the XDG location, a volume for
the SQLite file. Every operator rebuilt the same manifests, and the ones that matter are easy to
get subtly wrong — a second replica, a config file carrying secrets, a database on an `emptyDir`.

The application constrains what a chart may do. State lives in SQLite
([ADR 0004](0004-container-image.md) puts the file in `/data`), and SQLite takes one writer, so the
workload is a single pod whatever its kind. Configuration comes from a file, environment variables
and flags in that order of increasing precedence, and an environment variable is honoured *only*
when no config file sets the key ([ADR 0001](0001-kong-xdg-configuration.md)). There is no health
endpoint. Clusters differ in how they publish a service — Ingress in some, Gateway API in others —
and in how they manage claims.

A remote database was considered as the second deployment shape, so that a Deployment with several
replicas could share it. No such driver exists: `internal/store` has one implementation, and adding
Postgres is an application change with a schema, a migration story and its own ADR.

## Decision

We will keep a Helm chart in `charts/teamster` in this repository and publish it as an OCI artifact
to `ghcr.io/pflege-de-labs/charts` from `main`.

It lives here rather than in a separate charts repository because the chart's correctness depends
on the application's configuration schema, the container's file layout and the endpoints it serves.
Those change in a pull request that can change the chart in the same commit; a chart in a second
repository learns about a rename after someone's deployment breaks.

The chart defaults to a **StatefulSet** with one replica and a `volumeClaimTemplate` for `/data`. A
**Deployment** is offered for clusters that provision claims separately: it needs an existing claim
or an explicitly chart-managed one, and rolls with `Recreate`, because a `ReadWriteOnce` volume
admits one pod. `replicaCount` above 1 fails at render time rather than corrupting a database.

`database.driver` accepts `sqlite` and rejects `postgres` with a message pointing at the roadmap.
Rejecting is better than ignoring: a values file written for a capability we do not have should
fail loudly, not start a server whose state is somewhere else than the operator believes.

Configuration is split in two, following the precedence rule above:

* `config.settings` is rendered verbatim into a Secret mounted at `/etc/xdg/teamster/config.yaml`.
  It is a Secret and not a ConfigMap because the operator's own YAML may carry an OIDC client
  secret, and it is passed through untouched — in teamster's own hyphenated key names — so a new
  setting needs no chart release. The chart owns `database.path` alone, the one key that has to
  agree with the volume mount.
* `credentials` becomes a second Secret whose keys are `TEAMSTER_*` variable names, injected with
  `envFrom`, and it may be replaced by a secret the cluster manages. The four required credentials
  must not appear in the config file, or they would win over the secret.

Both Ingress and Gateway API `HTTPRoute` are supported, neither enabled by default, because a
cluster has one of the two and the chart cannot guess which. `extraObjects` renders arbitrary
resources — as a map, whose keys merge across values files, or as a list — through `tpl`, so a
deployment can carry its Alertmanager receiver, network policy or sealed secret without a fork.

Probes are `httpGet` checks against `/healthz` and `/readyz`. Readiness reports a shutdown before
the listener stops, which is what lets a rolling update drain, and it fails on a store that does
not answer; liveness deliberately checks nothing outside the process.

## Consequences

Deploying teamster is now `helm install` plus five values that have no safe default. The single
writer, the secret split and the durable volume are the defaults rather than something each
operator rediscovers.

The chart is versioned independently of the application and its `appVersion` is bumped by hand when
a release is cut, which is a step that can be forgotten; `image.tag` overrides it in the meantime.
Chart changes are gated by `helm lint` and a render of both deployment shapes in CI, which catches
a template error but not a cluster-specific one.

Adding Postgres later means an application driver, a store implementation and a migration path.
The chart's `database.driver` key and the Deployment mode are the places that would grow to meet
it; until then they carry a validation error and a single-writer Deployment respectively.

Nothing changes for an existing hand-rolled deployment. Adopting the chart means importing the
SQLite volume into a release, or accepting a fresh database and re-creating templates,
destinations and routes.
