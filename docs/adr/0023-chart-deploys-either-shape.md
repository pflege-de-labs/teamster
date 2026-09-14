# 0023. The chart deploys either shape

* Status: Accepted
* Date: 2026-09-14
* Supersedes [0016](0016-helm-chart.md)

## Context

[ADR 0016](0016-helm-chart.md) decided that the chart deploys a single SQLite writer, and that
`database.driver=postgres` is refused at render time. That was right when it was written: the
application had one store implementation, and a values file asking for a capability that did not
exist should fail loudly rather than start a service whose state is somewhere the operator did not
expect.

[ADR 0022](0022-postgres-second-backend.md) built the capability. The guard is now the only thing
standing between an operator and the deployment shape the whole storage change was for.

## Decision

We will derive the deployment shape from `database.driver`, and reverse the guard.

`workload.kind` defaults to empty and derives a StatefulSet for `sqlite`, whose file needs a claim
that follows the pod, and a Deployment for `postgres`, which keeps nothing locally. Either can be
set explicitly; the combinations that cannot work are still refused at render time. `replicaCount`
above 1 is refused for `sqlite` and unrestricted for `postgres`. A Deployment rolls with `Recreate`
under `sqlite`, because a `ReadWriteOnce` volume admits one pod, and with `maxUnavailable: 0` and a
surge pod under `postgres`. A multi-replica `postgres` release gets a PodDisruptionBudget; a
single-pod release does not, because one-of-one is not availability, it is a node drain that never
completes.

**The chart deploys no Postgres.** A bundled single-pod database on a PVC would make "teamster is
highly available" mean "teamster now depends on something less available than the StatefulSet it
replaced", while looking like the opposite — which is exactly the class of mistake the render-time
guards exist to prevent. HA Postgres in Kubernetes is an operator's job or a managed service. What
the chart contributes instead is `database.postgres.passwordFrom`, which reads the secret that
operator already created, and an `extraObjects` recipe so one `helm install` can still declare both.

**Migrations stay in the process**, guarded by the advisory lock from ADR 0022. A
`helm.sh/hook` Job would need the config and credentials Secrets, which render after hooks, and a
failed hook leaves the release `failed` with the old pods serving — which reads as "the upgrade did
nothing" rather than "the migration failed". `config.settings.database.migrate=verify` moves it out
of the startup path for deployments that want that, and the command it names can run from a Job in
`extraObjects`.

**A secret in `config.settings` is now a render-time error.** The chart README has warned about it
in prose since 0016 and nothing enforced it. A config file value beats the environment variable
carrying the same setting ([ADR 0001](0001-kong-xdg-configuration.md)), so a password written there
silently overrides the secret — the failure is invisible and the value is in a Secret that anybody
with read access to the namespace can see in plain text.

## Consequences

`--set database.driver=postgres --set database.postgres.host=... --set replicaCount=3` is a
working HA deployment, and the guards that remain describe real constraints rather than missing
features.

`workload.kind` changing from `StatefulSet` to `""` renders identically for every existing sqlite
values file; only `helm get values` output differs. `persistence` is silently ignored under
`postgres` rather than refused, because Helm cannot tell a default `true` from an explicit one and
`--set database.driver=postgres` on its own has to work; `persistence.existingClaim`, which can
only have been typed deliberately, is refused.

The CI job that asserted `database.driver=postgres` must fail now asserts the nine combinations
that must still fail, and `scripts/check-chart-render.sh` is driver-aware: a sqlite release must
mount a data volume, a postgres release must not, and neither may render a password into the config
file.

Chart 0.2.0, appVersion 0.3.0. The appVersion must never point at an image without the driver, so
this chart cannot be released before the application release that carries it.

What this does not decide is read replicas, a shared directory cache, or whether the session
sweeper should be leader-elected — all three are currently "not worth it" and all three belong in
the roadmap rather than here.
