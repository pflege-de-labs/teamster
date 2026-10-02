---
title: Kubernetes
weight: 3
---

Installieren Sie Teamster mit dem Helm-Chart in einen Kubernetes-Cluster. Das Chart wird als
OCI-Artefakt unter `oci://ghcr.io/pflege-de-labs/charts/teamster` veröffentlicht und unabhängig von
der Anwendung versioniert; seine `appVersion` nennt das Teamster-Release, dessen Image es
standardmäßig verwendet.

## Bevor Sie beginnen {#before-you-start}

Sie benötigen die Graph- und Bot-Registrierungen aus dem
[Schnellstart]({{< ref "/docs/getting-started/quick-start" >}}), Helm 3 und einen Namespace für die
Installation.

{{% steps %}}

### Das Chart installieren {#install-the-chart}

```bash
helm install teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --create-namespace \
  --set credentials.adminPassword=<admin-password> \
  --set credentials.graphClientSecret=<graph-client-secret> \
  --set config.settings.graph.tenant-id=<tenant-id> \
  --set config.settings.graph.client-id=<graph-client-id> \
  --set credentials.botClientSecret=<bot-client-secret> \
  --set config.settings.bot.tenant-id=<tenant-id> \
  --set config.settings.bot.client-id=<bot-client-id>
```

Ohne Admin-Anmeldung und Graph-Zugangsdaten startet Teamster nicht. Ohne den Bot startet es, aber
dann erreicht nichts Teams.

{{< callout type="warning" >}}
`--set` schreibt die Secrets in die Release-Historie. Legen Sie außerhalb einer Demo selbst ein
Secret an und verweisen Sie mit `credentials.existingSecret` darauf.
{{< /callout >}}

### Die Verwaltungsoberfläche öffnen {#open-the-admin-ui}

```bash
kubectl --namespace monitoring port-forward svc/teamster 8080:8080
```

Öffnen Sie <http://localhost:8080/admin> und melden Sie sich als `admin` mit dem gesetzten Passwort
an. Fahren Sie dann mit den Schritten für Ziel, Route und Token aus dem
[Schnellstart]({{< ref "/docs/getting-started/quick-start" >}}) fort.

{{% /steps %}}

## Was das Chart bereitstellt {#what-the-chart-deploys}

* **Ein StatefulSet mit einem Replikat** als Standard. Der Zustand liegt in einer SQLite-Datei auf
  einem Volume, und SQLite erlaubt nur einen Schreiber. Deshalb lehnt das Chart mit `sqlite` ein
  `replicaCount` größer als 1 ab.
* **Ein Konfigurations-Secret**, gebaut aus `config.settings` und eingebunden unter
  `/etc/xdg/teamster/config.yaml`. Seine Schlüssel sind Teamsters eigene YAML-Schlüssel mit
  Bindestrichen.
* **Ein Secret mit Zugangsdaten**, gebaut aus `credentials` und als `TEAMSTER_*`-Umgebungsvariablen
  übergeben. Teamster liest eine Umgebungsvariable nur, wenn keine Konfigurationsdatei denselben
  Schlüssel setzt; ein Secret gehört daher nie unter `config.settings`.
* **Ein Service** auf Port `8080`. Veröffentlichen Sie ihn mit `ingress.enabled` oder über die
  Gateway API mit `httpRoute.external.enabled` und `httpRoute.internal.enabled`.

Ändert sich eines der beiden Secrets, wird der Pod neu ausgerollt.

## Mehr als ein Replikat betreiben {#run-more-than-one-replica}

Richten Sie das Release auf eine Postgres-Datenbank. Das Chart stellt dann ein Deployment statt
eines StatefulSets bereit, und beliebig viele Replikate teilen sich die Datenbank:

```bash
helm upgrade teamster oci://ghcr.io/pflege-de-labs/charts/teamster \
  --namespace monitoring --reuse-values \
  --set database.driver=postgres \
  --set database.postgres.host=teamster-pg-rw \
  --set database.postgres.passwordFrom.secretName=teamster-pg-app \
  --set replicaCount=3
```

Das Chart bringt kein eigenes Postgres mit. Es liest das Passwort aus dem Secret, das Ihr
Postgres-Operator oder Cloud-Anbieter bereits angelegt hat.

## Nächste Schritte {#next-steps}

* [Speicher wählen und betreiben](../../guides/storage/) und
  [von SQLite zu Postgres wechseln](../../guides/backup-and-migration/).
* [Alarme aus Alertmanager senden](../../guides/alertmanager/) innerhalb des Clusters.
* Alle Chart-Werte: [Helm-Werte](../../reference/helm-values/).
