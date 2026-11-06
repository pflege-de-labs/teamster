---
title: Versionshinweise
weight: 99
---

Was sich in jedem Release geändert hat, steht in zwei Changelogs auf GitHub:

* [Teamster-Changelog](https://github.com/pflege-de-labs/teamster/blob/main/CHANGELOG.md): die
  Anwendung und ihr Container-Image, veröffentlicht als Tags `vX.Y.Z`.
* [Changelog des Helm-Charts](https://github.com/pflege-de-labs/teamster/blob/main/charts/teamster/CHANGELOG.md):
  das Chart, separat veröffentlicht als Tags `teamster-X.Y.Z`.

Beide führen noch nicht veröffentlichte Änderungen oben unter `Unreleased`.

{{< callout type="warning" >}}
Vor dem Upgrade auf 0.18: Eine Wurzelroute mit leerem Label-Selektor traf bisher auf nichts zu. Jetzt
erhält sie jeden Alarm von jedem Webhook. Beim Start protokolliert Teamster für jede solche Route
`route has no label selector and receives every alert`. Geben Sie ihr einen Selektor oder löschen
Sie sie vor dem Upgrade. Siehe [Routing]({{< ref "/docs/concepts/routing" >}}).
{{< /callout >}}

Die [Release-Seite](https://github.com/pflege-de-labs/teamster/releases) enthält zu jedem Release
die Binaries, ihre SBOMs und die signierten Prüfsummen. Wie Sie diese vor dem Deployment prüfen,
beschreibt [Release verifizieren](../guides/verifying-a-release/).

Auch diese Dokumentation ist versioniert: Wählen Sie im Menü **Versionen** die Version, die Sie
betreiben.
