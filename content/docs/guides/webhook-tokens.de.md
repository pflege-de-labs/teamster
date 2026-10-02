---
title: Einen Webhook-Absender authentifizieren
weight: 13
---

Geben Sie jedem Absender ein eigenes Zugriffstoken, beschränkt auf den Webhook, den er verwendet,
und finden Sie heraus, warum ein Absender abgewiesen wird.

`/webhook/alertmanager` und `/webhook/universal` nehmen beide ein Token als
`Authorization: Bearer <token>` entgegen. Beim [Teams-V2-Webhook](../teams-v2-webhook/) ist das
anders: Dort ist das Token Teil der URL.

## Ein Token ausstellen {#issue-a-token}

Wer einen Webhook verwenden darf, kann ein Token dafür ausstellen: Bearbeiter und Administratoren
für beide Webhooks, alle anderen für die Webhooks, die ihnen freigegeben sind. Wie Sie den Zugriff
auf die Webhooks freigeben, beschreibt
[Rollen und Zugriff verwalten](../roles/#let-someone-use-the-webhooks).

{{% steps %}}

### Das Token erstellen {#create-it}

Öffnen Sie **/admin/tokens**. Benennen Sie das Token nach dem Absender, zum Beispiel
`alertmanager-prod`, und kreuzen Sie die Webhooks an, an die es senden darf. Kreuzen Sie nur den an,
den der Absender verwendet.

### Das Token kopieren {#copy-it}

Das Token beginnt mit `tst_` und wird nur einmal angezeigt. Teamster speichert nur einen Digest und
kann es daher nicht erneut anzeigen. Legen Sie es im Secret-Speicher des Absenders ab.

### Den Absender einrichten {#configure-the-sender}

Siehe [Alarme aus Alertmanager senden](../alertmanager/) oder
[Ereignisse über den universellen Webhook senden](../universal-webhook/).

{{% /steps %}}

Sie sehen und widerrufen die Tokens, die Sie erstellt haben. Webhook-Administratoren und
Administratoren sehen und widerrufen die aller Personen. Die Seite zeigt auf die Stunde genau, wann
jedes Token zuletzt verwendet wurde.

Skripte erledigen dasselbe über die API, mit einer Sitzung oder den lokalen
Administrator-Zugangsdaten:

```bash
# ausstellen; die Antwort enthält das Token einmalig, in "token"
curl -u <admin-user>:<admin-password> -X POST http://localhost:8080/api/tokens \
  -d '{"name": "alertmanager-prod", "scope": ["alertmanager"]}'

# auflisten
curl -u <admin-user>:<admin-password> http://localhost:8080/api/tokens

# widerrufen
curl -u <admin-user>:<admin-password> -X DELETE http://localhost:8080/api/tokens/<id>
```

`scope` nimmt `alertmanager`, `universal` oder beides.

### Ein Token hängt an seinem Ersteller {#a-token-answers-to-its-creator}

Jede Verwendung prüft zweierlei: Der Bereich des Tokens muss den Webhook nennen, und sein Ersteller
muss ihn mit den Rollen und Gruppen seiner letzten Anmeldung noch verwenden dürfen. Wird der
Ersteller deaktiviert, aus einer Gruppe entfernt oder verliert er seine Webhook-Stufe, sind seine
Tokens ab der nächsten Anfrage widerrufen. Stellen Sie Tokens als jemand aus, dessen Zugriff länger
besteht als der Absender, sonst hört der Absender auf zu funktionieren, wenn diese Person geht.
Siehe [Eigentum und Teilen](../../concepts/ownership/) und
[ADR 0077](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0077-scoped-tokens-answer-to-their-creator.md).

Tokens, die vor Release 0.11.0 ausgestellt wurden, haben keinen Bereich: Sie erreichen beide
Webhooks, gleich wer sie erstellt hat. Stellen Sie neue aus, um sie zu binden.

## Ein Token für die gesamte Installation verwenden {#use-a-deployment-wide-token}

`webhook.token` ist ein einzelnes Token aus der Konfiguration, das zusätzlich zu den ausgestellten
akzeptiert wird. Verwenden Sie es, wenn ein Absender deklarativ eingerichtet werden muss, bevor sich
jemand anmelden und ein Token ausstellen kann.

```yaml
webhook:
  token: <token>
```

Übergeben Sie es als `TEAMSTER_WEBHOOK_TOKEN` statt in der Datei. Es hat keinen Bereich: Es
erreicht beide Webhooks. Zum Rotieren ändern Sie den Wert und starten neu.

{{< callout type="warning" >}}
Der Header `X-Teamster-Token: <token>` aus früheren Releases funktioniert für beide Arten von
Tokens weiterhin, ist aber veraltet und wird in einem Release mit Breaking Changes entfernt. Stellen
Sie die Absender auf `Authorization: Bearer` um.
{{< /callout >}}

## Wenn ein Absender abgewiesen wird {#when-a-sender-is-refused}

**`401`**, von Alertmanager als `unexpected status code 401` protokolliert, bedeutet, dass Teamster
das Token nicht akzeptiert hat. Das Serverlog nennt den Grund in einer Warnung wie
`msg="webhook refused" source=alertmanager reason=…`, und die Abweisung wird in
`teamster.webhook.receipts` mit dem Zustand `refused` gezählt. Prüfen Sie der Reihe nach:

1. Der Absender setzt einen Header `Authorization: Bearer`. `basic_auth` wird nicht akzeptiert.
2. Das Token existiert in dieser Installation. Ausgestellte Tokens liegen in der Datenbank, daher
   übernehmen `teamster export` und `import` sie nicht, und eine neue Datenbank ebenso wenig.
3. Das Token wurde nicht widerrufen.
4. `webhook.token` ist nicht gleichzeitig in einer Konfigurationsdatei und in
   `TEAMSTER_WEBHOOK_TOKEN` gesetzt. Die Datei hat Vorrang, der Wert aus dem Secret wird also
   ignoriert. Entfernen Sie den Schlüssel aus der Datei.
5. Es gibt überhaupt ein Token. Ist `webhook.token` nicht gesetzt und keines ausgestellt, wird
   jeder Absender abgewiesen, und Teamster protokolliert das beim Start.

**`403` mit einem JSON-Body** `{"error": "the token's scope, or its creator, does not allow this
webhook"}` bedeutet, dass ein Token mit Bereich für einen Webhook verwendet wurde, den sein Bereich
nicht nennt, oder dass sein Ersteller ihn nicht mehr verwenden darf. Die Logzeile `webhook refused`
nennt das Token und seinen Ersteller. Die Abweisung wird mit dem Zustand `forbidden` gezählt.

**`403 RBAC: access denied`** ist nicht die Antwort von Teamster. Das ist der Wortlaut von Envoy:
Ein Service Mesh, etwa eine Istio-`AuthorizationPolicy`, oder ein Gateway hat die Anfrage
abgewiesen, bevor sie den Pod erreicht hat. Erlauben Sie dort dem Workload des Absenders den
Zugriff auf den Pfad des Webhooks.

**`503`** bedeutet, dass Teamster das Token nicht prüfen konnte, weil die Datenbank nicht
geantwortet hat. Der Absender sollte es erneut versuchen, wie bei jeder `5xx`-Antwort.

## Was die Tokens schützen {#what-the-tokens-protect}

Die Webhook-Endpunkte sind allein durch ihre Tokens geschützt. Wer eines besitzt, kann mehr als
einen falschen Alarm absetzen.

Labels und Annotations werden in Vorlagen eingesetzt, und das Ergebnis landet in einem Kanal oder
im Chat einer Person. Die Bereinigung entfernt Skripte, Bilder und jedes Attribut außer dem `href`
eines Links, und sie behält ein `href` nur für `http`, `https` und `mailto`. Links selbst entfernt
sie nicht. Eine Annotation wie `[Open the runbook](https://evil.example/login)` wird zu einem Link,
dessen Text das eine sagt und dessen Ziel ein anderes ist, zugestellt von einem Dienst, dem Ihre
Leute vertrauen, um drei Uhr morgens. Links bleiben erhalten, weil Vorlagen auf Runbooks und
Dashboards verlinken. Die Kontrolle ist deshalb das Token:

* **Behandeln Sie ein Token wie ein Passwort.** Widerrufen Sie ausgestellte Tokens unter
  **/admin/tokens**.
* **Halten Sie die Webhooks vom Internet fern**, wenn nur Absender im Cluster sie brauchen. Ein
  Alertmanager im selben Cluster braucht keinen Ingress.
* **Geben Sie jedem Absender ein eigenes Token**, beschränkt auf den einen Webhook, den er
  verwendet, damit sich eines widerrufen lässt, ohne die anderen zu stören. Ein Token erreicht
  dennoch jede Route hinter seinem Webhook, Absender in unterschiedlichen Vertrauensbereichen
  brauchen also getrennte Installationen.
* **Terminieren Sie TLS vor Teamster.** Das Token reist bei jeder Anfrage in einem Header mit.
* **Alarmieren Sie bei Abweisungen.** Ein Token, das erraten werden soll, zeigt sich als
  `teamster.webhook.receipts` mit dem Zustand `refused`:

  ```promql
  sum by (source) (rate(teamster_webhook_receipts_total{state="refused"}[5m])) > 0
  ```

Wie Sie Metriken aktivieren, beschreibt [Teamster überwachen](../observability/), den Entwurf
[ADR 0044](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0044-webhook-access-tokens.md).
