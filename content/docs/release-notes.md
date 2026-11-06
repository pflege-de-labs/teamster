---
title: Release notes
weight: 99
---

What changed in each release is recorded in two changelogs on GitHub:

* [Teamster changelog](https://github.com/pflege-de-labs/teamster/blob/main/CHANGELOG.md): the
  application and its container image, released as tags `vX.Y.Z`.
* [Helm chart changelog](https://github.com/pflege-de-labs/teamster/blob/main/charts/teamster/CHANGELOG.md):
  the chart, released separately as tags `teamster-X.Y.Z`.

Both list unreleased changes at the top, under `Unreleased`.

{{< callout type="warning" >}}
Before you upgrade to 0.18: a root route with an empty label selector used
to match nothing. It now receives every alert from every webhook. On startup, Teamster logs
`route has no label selector and receives every alert` for each such route. Give it a selector or
delete it before you upgrade. See [Routing]({{< ref "/docs/concepts/routing" >}}).
{{< /callout >}}

The [releases page](https://github.com/pflege-de-labs/teamster/releases) carries the binaries,
their SBOMs and the signed checksums for each release. To check them before you deploy, see
[Verify a release](../guides/verifying-a-release/).

This documentation is versioned too: pick the version you run from the **Versions** menu.
