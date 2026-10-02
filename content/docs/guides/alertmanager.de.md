---
title: Alarme aus Alertmanager senden
weight: 10
---

Richten Sie einen Receiver von Prometheus Alertmanager auf Teamster aus. Jede Gruppe von Alarmen
erscheint dann in Teams als eine Karte, die aktualisiert wird, solange ihre Alarme feuern, und
geschlossen wird, sobald sie aufgelöst sind.

Sie brauchen ein laufendes Teamster und ein Zugriffstoken, das an den Alertmanager-Webhook senden
darf. Wie Sie eines ausstellen, beschreibt [Einen Webhook-Absender authentifizieren](../webhook-tokens/).

## Den Receiver einrichten {#configure-the-receiver}

{{% steps %}}

### Ein Token ausstellen {#issue-a-token}

Melden Sie sich in der Verwaltungsoberfläche an und öffnen Sie **/admin/tokens**. Benennen Sie das
Token nach dem Absender (zum Beispiel `alertmanager-prod`). Unter **Darf senden an** sind beide
Webhooks vorausgewählt; entfernen Sie den Haken bei `/webhook/universal`, damit das Token nur
`/webhook/alertmanager` erreicht. Kopieren Sie das Token sofort: Es wird nur dieses eine Mal
angezeigt.

Legen Sie es dort ab, wo Alertmanager es lesen kann, zum Beispiel in einem Kubernetes-Secret:

```bash
kubectl -n monitoring create secret generic teamster-webhook --from-literal=token=<token>
```

### Den Receiver hinzufügen {#add-the-receiver}

Der Endpunkt ist `POST /webhook/alertmanager`. Das Token steht im Header `Authorization: Bearer`.
Setzen Sie `send_resolved`, sonst wird die Karte nie geschlossen.

{{< tabs >}}
{{< tab name="alertmanager.yml" >}}

```yaml
receivers:
  - name: teamster
    webhook_configs:
      - url: http://teamster.monitoring.svc:8080/webhook/alertmanager
        send_resolved: true
        http_config:
          authorization:
            type: Bearer
            credentials_file: /etc/alertmanager/secrets/teamster/token
```

Hängen Sie das Secret unter `/etc/alertmanager/secrets/teamster/` ein.

{{< /tab >}}
{{< tab name="AlertmanagerConfig" >}}

Für den Prometheus Operator:

```yaml
receivers:
  - name: teamster
    webhookConfigs:
      - url: http://teamster.monitoring.svc:8080/webhook/alertmanager
        sendResolved: true
        httpConfig:
          authorization:
            type: Bearer
            credentials:
              name: teamster-webhook
              key: token
```

{{< /tab >}}
{{< /tabs >}}

Verwenden Sie nicht `basic_auth`. Teamster akzeptiert an den Webhooks nur ein Bearer-Token.

### Alarme an den Receiver leiten {#route-alerts-to-the-receiver}

Legen Sie in Alertmanager eine Route an, die die gewünschten Alarme an den Receiver `teamster`
schickt. Ihr `group_by` bestimmt, welche Alarme sich eine Karte teilen. In welchem Teams-Kanal eine
Gruppe landet, entscheidet Teamster mit seinen eigenen Routen. Siehe
[Wie das Routing funktioniert](../../concepts/routing/).

### Ausprobieren {#test-it}

Senden Sie einen Beispielalarm von Hand:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  --data @alertmanager-firing.json http://localhost:8080/webhook/alertmanager
```

Die Dateien
[`alertmanager-firing.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-firing.json)
und
[`alertmanager-resolved.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-resolved.json)
finden Sie im Repository, für eine Gruppe aus zwei Alarmen außerdem
[`alertmanager-group.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/alertmanager-group.json).
Die Antwort `200 {"status":"ok"}` bedeutet, dass die Benachrichtigung zugestellt wurde.

{{% /steps %}}

## Was Teamster mit einer Benachrichtigung macht {#what-teamster-does-with-a-notification}

* Eine Benachrichtigung ist ein Ereignis, gleich wie viele Alarme Alertmanager darin gruppiert hat.
  Ihr `groupKey` ist der Schlüssel, daher bearbeitet jede Benachrichtigung für die Gruppe dieselbe
  Karte.
* Der `status` der Benachrichtigung ist der Zustand. Er ist `firing`, solange ein Alarm der Gruppe
  feuert, was die Karte öffnet, und `resolved`, sobald alle aufgelöst sind, was sie schließt.
  Jeder andere Status wird einmal zugestellt und nicht verfolgt. Siehe
  [Lebenszyklus eines Alarms](../../concepts/alert-lifecycle/).
* Routen wählen nach `commonLabels` aus, den Labels, die alle Alarme der Gruppe gemeinsam haben.
  Bei einer Gruppe mit einem Alarm sind das die Labels dieses Alarms.
* Jedes Ereignis trägt das Label `teamster_source: alertmanager`. Eine Route kann danach auswählen.
* Vorlagen finden die Alarme der Gruppe in `.Event.Alertmanager.Alerts`. Die flachen Felder
  `Annotations`, `StartsAt`, `EndsAt` und `GeneratorURL` sind nur bei einer Gruppe mit einem Alarm
  gesetzt. Siehe [Die Alarme einer Gruppe auflisten](../templates/#list-the-alerts-of-a-group) und
  [Vorlagendaten](../../reference/template-data/#alertmanager-extension).
* Um einen Alarm an Personen statt an einen Kanal zu senden, setzen Sie das Label
  `teamster_recipient`. Die Adressen aller Alarme der Gruppe werden zusammengeführt. Siehe
  [Nachrichten an einzelne Personen senden](../direct-messages/).

## Nach dem Upgrade von einem Release mit einer Karte je Alarm {#after-upgrading-from-a-release-with-one-card-per-alert}

Releases bis 0.11.0 haben je Alarm eine Karte gepostet. Nach dem Upgrade gilt:

* Eine Gruppe, die bereits feuert, erhält eine neue Gruppenkarte neben ihren alten Karten je
  Alarm. Jede alte Karte wird geschlossen, sobald Alertmanager ihren Alarm als `resolved` meldet.
* Die Standardvorlage für Alertmanager, die das frühere Release angelegt hat, listet die Alarme
  einer Gruppe nicht auf. Wenden Sie die vorgefertigte Vorlage erneut an, wie unter
  [Die Alarme einer Gruppe auflisten](../templates/#list-the-alerts-of-a-group) beschrieben.

## Wenn Alertmanager einen Fehler meldet {#if-alertmanager-reports-an-error}

| Alertmanager protokolliert | Bedeutung |
| --- | --- |
| `unexpected status code 401` | Teamster hat das Token nicht akzeptiert. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 403` | Ein Service Mesh oder Gateway hat die Anfrage abgewiesen, der Bereich des Tokens erlaubt diesen Webhook nicht, oder ein Alarm setzt `teamster_recipient` und das Token darf keine Personen nennen. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 422` | Die Benachrichtigung war an Personen gerichtet, und keine von ihnen war erreichbar. Alertmanager wiederholt sie nicht. |
| `unexpected status code 502` | Teams, Graph oder die Datenbank sind ausgefallen. Alertmanager wiederholt den Versuch. |
| `unexpected status code 503` | Das Token ließ sich nicht prüfen, weil die Datenbank nicht geantwortet hat. Alertmanager wiederholt den Versuch. |
