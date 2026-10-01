# 0079. The chart declares the audit stream through NACK

* Status: Accepted
* Date: 2026-10-01

## Context

The audit export publishes to a JetStream stream
([ADR 0071](0071-publish-audit-events-to-nats-jetstream.md)). `audit.nats.create-stream` lets
teamster create it, with nothing but a name and subjects. That suits a quick start, not a cluster
where streams are declared: replicas, retention, the duplicate window and the consumers that read
the stream are set by whoever runs NATS, and teamster's account may not be allowed to create
streams at all. Clusters that declare JetStream resources in Kubernetes mostly use NACK
(nats.io/nack), whose controller reconciles `Stream`, `Consumer` and `Account` resources.

## Decision

* **The chart renders NACK resources on request.** `nack.stream.enabled` renders a `Stream`,
  `nack.consumers` renders a durable `Consumer` per key, and `nack.account.create` renders an
  `Account` that both connect through. All are off by default.
* **The stream follows the application's settings.** Its name and subjects come from
  `config.settings.audit.nats.stream` and `subject-prefix`, with the application's defaults, so the
  stream always captures what teamster publishes. `nack.stream.spec` is merged over the chart's
  defaults: file storage, one replica, a two-minute duplicate window, and `preventDelete`, which
  keeps the history when the release is uninstalled.
* **One manager per stream.** The chart refuses `nack.stream.enabled` together with
  `audit.nats.create-stream`. NACK enforces the state of what it manages and would undo teamster's
  configuration on every reconcile.
* **NACK is a prerequisite, not a dependency.** The chart neither installs the CRDs nor the
  controller. They are cluster-wide and shared, and their lifecycle is not the release's.

Alternatives considered:

* **`extraObjects`.** It works today, but each operator restates the subjects, and a typo there
  leaves a stream that captures nothing.
* **A NACK subchart.** It installs cluster-scoped CRDs and a controller per release, where a
  cluster runs one.

## Consequences

* Rendering these resources on a cluster without NACK's CRDs fails the install. Without the
  controller they are accepted and never reconciled, so the export has no stream.
* `Account` resources are reconciled only in NACK's control-loop mode.
* Changing `subject-prefix` or `stream` updates the declared stream as well. Renaming the stream
  makes NACK create a new one; the old one stays, because of `preventDelete`.
