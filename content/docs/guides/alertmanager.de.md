---
title: Alarme aus Alertmanager senden
weight: 10
---

Richten Sie einen Receiver von Prometheus Alertmanager auf Teamster aus. Seine Alarme erscheinen
dann in Teams als Karten, die aktualisiert werden, solange der Alarm feuert, und geschlossen
werden, sobald er aufgelöst ist.

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
schickt. In welchem Teams-Kanal ein Alarm landet, entscheidet Teamster mit seinen eigenen Routen.
Siehe [Wie das Routing funktioniert](../../concepts/routing/).

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
finden Sie im Repository. Die Antwort `200 {"status":"ok"}` bedeutet, dass der Alarm zugestellt
wurde.

{{% /steps %}}

## Was Teamster mit einem Alarm macht {#what-teamster-does-with-an-alert}

* Jeder Alarm der Gruppe wird zu einem Ereignis. Sein `fingerprint` ist der Schlüssel, `firing`
  wird zum Zustand `open` und `resolved` zu `closed`. Jeder andere Status wird einmal zugestellt
  und nicht verfolgt.
* Ein wiederholter `firing`-Alarm bearbeitet die Karte, die er bereits gepostet hat. Ein
  `resolved`-Alarm schließt sie. Siehe [Lebenszyklus eines Alarms](../../concepts/alert-lifecycle/).
* Jedes Ereignis trägt das Label `teamster_source: alertmanager`. Eine Route kann danach auswählen.
* Annotations, `generatorURL` und die Felder der Gruppe stehen Vorlagen unter
  `.Event.Alertmanager` zur Verfügung. Siehe [Vorlagendaten](../../reference/template-data/).
* Um einen Alarm an Personen statt an einen Kanal zu senden, setzen Sie das Label
  `teamster_recipient`. Siehe [Nachrichten an einzelne Personen senden](../direct-messages/).

## Wenn Alertmanager einen Fehler meldet {#if-alertmanager-reports-an-error}

| Alertmanager protokolliert | Bedeutung |
| --- | --- |
| `unexpected status code 401` | Teamster hat das Token nicht akzeptiert. Siehe [Wenn ein Absender abgewiesen wird](../webhook-tokens/#when-a-sender-is-refused). |
| `unexpected status code 403` | Ein Service Mesh oder Gateway hat die Anfrage abgewiesen, oder der Bereich des Tokens erlaubt diesen Webhook nicht. |
| `unexpected status code 422` | Der Alarm war an Personen gerichtet, und keine von ihnen war erreichbar. Alertmanager wiederholt ihn nicht. |
| `unexpected status code 502` | Teams, Graph oder die Datenbank sind ausgefallen. Alertmanager wiederholt den Versuch. |
| `unexpected status code 503` | Das Token ließ sich nicht prüfen, weil die Datenbank nicht geantwortet hat. Alertmanager wiederholt den Versuch. |
