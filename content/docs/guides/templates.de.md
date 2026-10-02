---
title: Vorlagen schreiben
weight: 16
---

Schreiben Sie eine Vorlage, die aus einem Ereignis eine Teams-Nachricht macht, legen Sie fest,
welche Webhooks sie verarbeitet, und bestimmen Sie die Standardvorlage, auf die jeder Webhook
zurückfällt.

Vorlagen finden Sie unter **Vorlagen** auf **/admin**. Jedes Feld, das eine Vorlage lesen kann,
führt [Vorlagendaten](../../reference/template-data/) auf.

## Eine Vorlage schreiben {#write-a-template}

{{% steps %}}

### Mit einer Vorlage beginnen {#start-from-a-preset}

Öffnen Sie eine neue Vorlage und wählen Sie eine unter **Mit einer Vorlage beginnen**. Das füllt
Name, Titel, Text, Karte und Webhooks in einem Schritt und geht schneller, als leer zu beginnen.

### Die Teile ausfüllen {#fill-in-the-parts}

Eine Vorlage hat drei optionale Teile und braucht mindestens einen davon:

| Teil | Was es ist | Wo es erscheint |
| --- | --- | --- |
| Titel | Ein Go-Template, das eine Zeile reinen Text ergibt | Vorschau im Aktivitätsfeed von Teams |
| Nachrichtentext | Ein Go-Template, das Markdown ergibt | Inhalt der Nachricht |
| Adaptive-Card-JSON | Eine Adaptive Card als Go-Template | Unter dem Text |

Schreiben Sie einen Titel, wenn die Nachricht nur aus einer Karte besteht. Sonst zeigt der Feed sie
als `Card` an. Ohne Titel greift Teamster auf die Annotation oder das Attribut `summary` zurück,
bei einer Alertmanager-Gruppe auf die gemeinsame Annotation `summary`, dann auf `alertname`, dann
auf `Update`. Ein Kanalbeitrag mit Karte zeigt den Titel außerdem oben auf der Karte.

Ein minimaler Nachrichtentext für eine Alertmanager-Gruppe mit einem Alarm:

```gotemplate
**{{ .Event.Alertmanager.Annotations.summary }}** is {{ .Event.State }}

{{ default .Event.Alertmanager.Annotations.description "No description." }}
```

### Die verarbeiteten Webhooks wählen {#choose-the-webhooks-it-handles}

Kreuzen Sie **Alertmanager**, **Universeller Webhook** oder **Teams-V2-Webhook** für die Nutzdaten
an, für die die Vorlage geschrieben ist, oder lassen Sie alles frei für jeden Webhook.

* Ein Teams-V2-Endpunkt bietet nur Vorlagen an, die Nutzdaten des Teams-V2-Webhooks verarbeiten.
* Eine Route, deren Selektor `teamster_source` festlegt, bietet nur Vorlagen für diesen Webhook
  an.
* Ein Ereignis, das eine Vorlage erreicht, die nicht für seinen Webhook geschrieben ist, erhält
  stattdessen die Standardvorlage seines Webhooks oder, wenn es keine gibt, die eingebaute
  Nachricht. Dabei wird eine Warnung protokolliert.

### Die Vorschau ansehen {#preview-it}

Die Vorschau rendert die Vorlage gegen Beispielnutzdaten für jeden Webhook und kann das JSON
zeigen, das der Bot für einen Kanalbeitrag und für einen Chat senden würde. Für Alertmanager gibt
es zwei Beispiele: **Alertmanager-Alarm**, eine Gruppe mit einem Alarm, und
**Alertmanager-Alarmgruppe** mit zwei Alarmen.

### Die Vorlage zuweisen {#attach-it}

Wählen Sie die Vorlage an einer Route, an der globalen Standardroute oder an einem
Teams-V2-Endpunkt. Siehe [Wie das Routing funktioniert](../../concepts/routing/).

{{% /steps %}}

## Webhook-spezifische Felder sicher lesen {#read-webhook-specific-fields-safely}

Felder, die nur ein Webhook kennt, liegen in seiner Erweiterung: `.Event.Alertmanager` oder
`.Event.Universal`. Für jeden anderen Webhook ist die Erweiterung nil, und der Zugriff in eine
nil-Erweiterung lässt das Rendern scheitern.

Eine Vorlage, die nur einen Webhook verarbeitet, kann seine Erweiterung direkt lesen:

```gotemplate
{{ .Event.Alertmanager.Annotations.summary }}
```

Eine Vorlage für jeden Webhook umschließt den Zugriff mit `with`:

```gotemplate
{{ with .Event.Alertmanager }}{{ .Annotations.summary }}{{ end }}
{{ with .Event.Universal }}{{ .Attributes.summary }}{{ end }}
```

Ein Label-Schlüssel, der nicht auf einen Punkt folgen kann, braucht `index`:

```gotemplate
{{ index .Event.Labels "app.kubernetes.io/name" }}
```

Verwenden Sie `default` für einen Schlüssel, den ein Ereignis womöglich nicht setzt, und `toJSON`,
um eine Struktur in eine Karte einzubetten.

## Die Alarme einer Gruppe auflisten {#list-the-alerts-of-a-group}

Alertmanager sendet eine Gruppe von Alarmen als eine Benachrichtigung, und Teamster postet dafür
eine Karte. Die Alarme stehen in `.Event.Alertmanager.Alerts`, jeder mit `Status`, `Labels`,
`Annotations`, `StartsAt`, `EndsAt`, `GeneratorURL` und `Fingerprint`. `.Event.Labels` und
`.Event.Alertmanager.CommonAnnotations` enthalten, was alle gemeinsam haben.

Die flachen Felder `.Event.Alertmanager.Annotations`, `StartsAt`, `EndsAt` und `GeneratorURL` sind
eine Abkürzung für eine Gruppe mit einem Alarm und bei einer größeren Gruppe leer. Zählen Sie die
Alarme mit `len`, um beide Fälle zu behandeln:

```gotemplate
{{ if eq (len .Event.Alertmanager.Alerts) 1 }}
  {{ .Event.Alertmanager.Annotations.summary }}
{{ else }}
  {{ range .Event.Alertmanager.Alerts }}{{ .Labels.alertname }} ({{ .Status }}): {{ .Annotations.summary }}
  {{ end }}
{{ end }}
```

In einer Karte fügt der Baustein **Alarmliste** aus der Palette des Editors ein `FactSet` mit
einem Eintrag je Alarm ein. Die vorgefertigte Vorlage für Alertmanager listet die Alarme einer
größeren Gruppe genauso auf und nennt ihre Anzahl im Titel.

{{< callout type="warning" >}}
Eine vorgefertigte Vorlage wird nur in einer neuen Installation angelegt. Eine Installation, die
mit 0.11.0 oder früher eingerichtet wurde, behält ihre alte Vorlage **Alertmanager (default)**.
Sie zeigt eine Gruppe ohne ihre Alarme und mit dem gemeinsamen `alertname` als Titel. Um sie zu
aktualisieren, öffnen Sie die Vorlage, wählen unter **Mit einer Vorlage beginnen** den Eintrag
**Alertmanager-Alarm**, bestätigen und speichern. Dabei werden Titel, Text und Karte ersetzt;
sichern Sie eigene Änderungen also vorher.
{{< /callout >}}

## Wissen, was der Text enthalten darf {#know-what-the-text-may-contain}

Der Nachrichtentext ist Markdown. Teamster rendert ihn zu HTML und bereinigt dieses:

* `p`, `br`, `b`, `strong`, `i`, `em`, `u`, `s`, `code`, `pre`, `blockquote`, `ul`, `ol`, `li`,
  `h1` bis `h3` und `a` bleiben erhalten.
* `script`, `style` und ähnliche Elemente entfallen samt Inhalt. Alles andere wird auf seinen Text
  reduziert.
* Ein Link behält sein `href` nur für `http`, `https` und `mailto`. Jedes andere Attribut entfällt.

Rohes HTML durchläuft den Markdown-Parser, eine als HTML geschriebene Vorlage funktioniert also
weiterhin. Ein Teams-Kanal erhält das bereinigte HTML. Der Chat einer Person erhält Markdown, das
aus diesem bereinigten HTML erzeugt wird.

Ein Kartenbody, der zu nichts rendert, sendet keine Karte, statt zu scheitern. Daher ist
`{{ if .Event.Card }}…{{ end }}` auch für Nutzdaten ohne Karte sicher.

{{< callout type="warning" >}}
Annotations und Attribute stammen von jedem, der an den Webhook posten darf. Die Bereinigung
entfernt Skripte und Bilder, aber keine Links, deren Text und Ziel sich unterscheiden. Siehe
[Was die Tokens schützen](../webhook-tokens/#what-the-tokens-protect).
{{< /callout >}}

## Die Standardvorlage je Webhook festlegen {#set-the-default-template-per-webhook}

Jeder Webhook hat eine Standardvorlage. Sie wird verwendet, wenn eine Route oder ein
Teams-V2-Endpunkt keine Vorlage nennt, die seine Ereignisse verarbeitet. Wählen Sie sie unter
**Standardvorlage je Webhook** im Bereich der Vorlagen oder über die API:

```bash
curl -u <admin-user>:<admin-password> -X PUT http://localhost:8080/api/templates/source-defaults \
  -d '{"templates": {"alertmanager": "<template id>", "universal": "<template id>", "teamsv2": ""}}'
```

Eine leere ID steht für die eingebaute Nachricht. Standard eines Webhooks kann nur eine Vorlage
werden, die ihn verarbeitet. `GET` auf denselben Pfad liefert die aktuelle Auswahl.

Beim ersten Start legt Teamster drei Vorlagen an und macht jede zum Standard ihres Webhooks, sofern
dieser noch keinen hat:

| Vorlage | Rendert |
| --- | --- |
| Alertmanager (default) | Eine nach Zustand eingefärbte Karte: Zusammenfassung, Schweregrad, Beschreibung, Labels, Beginn und Ende oder bei einer Gruppe mehrerer Alarme eine Zeile je Alarm sowie Links zur Generator-URL und zu einer Annotation `runbook_url` |
| Universal webhook (default) | Die eigene Karte oder den eigenen Text des Absenders, falls er eines davon gesendet hat, sonst eine nach Zustand eingefärbte Karte: Zusammenfassung, Zustand, Beschreibung, Labels und die URL |
| Teams V2 webhook (default) | Die Karte der Nutzdaten, eine daraus umgewandelte MessageCard oder nur deren Text |

Es sind gewöhnliche Vorlagen. Bearbeiten oder löschen Sie sie nach Belieben; eine gelöschte wird
nicht neu angelegt.

Die globale Standardroute hat eine eigene Vorlage. Sie wird in ihrer Zeile im Bereich **Routen**
gewählt oder mit `PUT /api/routes/global-default` und `{"template_id": "…"}`. Ohne Vorlage sendet
sie die eingebaute Nachricht.

## Die Vervollständigung im Editor nutzen {#use-editor-completion}

Die Felder einer Vorlage, der Label-Selektor einer Route und das Label-Feld auf **/admin/routing**
vervollständigen während der Eingabe. Mit `Ctrl-Space` fordern Sie die Vervollständigung an
beliebiger Stelle an.

* Innerhalb von `{{ … }}` bieten sie die Vorlagendaten, die Funktionen und die Aktionen an (`if`,
  `range`, `end`, …).
* Nach einer Label-Map wie `.Event.Labels.` oder einer Attribut-Map wie
  `.Event.Universal.Attributes.` bieten sie die Schlüssel an, die jüngste Ereignisse trugen. Ein
  Schlüssel, der nicht auf einen Punkt folgen kann, wird als
  `(index .Event.Labels "app.kubernetes.io/name")` eingefügt.
* In der Karte bieten sie Elementtypen, Eigenschaftsnamen und Enum-Werte von Adaptive Cards an.
* Ein Label-Selektor bietet Label-Schlüssel an, danach die jüngsten Werte des gewählten Schlüssels.

Ohne JavaScript sind die Felder einfache Textfelder.

Die Schlüssel und Werte stammen aus Stichproben jedes Alertmanager- und jedes universellen
Ereignisses, gleich ob eine Route darauf zutrifft. **Werte von Attributen und Annotations werden
nie gespeichert**, nur ihre Schlüssel. Label-Werte werden gespeichert, daher kann sie nur eine
Rolle lesen, die Vorlagen oder Routen bearbeiten darf, unter `GET /api/samples`.

Die Stichproben sind standardmäßig aktiv. Die gezeigten Werte sind die Standardwerte:

```yaml
samples:
  enabled: true              # eingehende Ereignisse überhaupt erfassen
  retention: "720h"          # Schlüssel oder Wert vergessen, der so lange nicht vorkam
  max-values-per-key: 50     # nur die zuletzt gesehenen Werte jedes Label-Schlüssels behalten
  max-value-length: 200      # längere Label-Werte nicht erfassen
  lru-size: 4096             # Stichproben im Speicher, um Schreibvorgänge zusammenzufassen
  flush-interval: "5m"       # eine bereits gespeicherte Stichprobe höchstens so oft schreiben
```

Die Stichproben verzögern nie eine Zustellung: Ein Schreiber im Hintergrund verwirft Stichproben,
wenn er nicht hinterherkommt, die Zählungen sind daher Näherungswerte. `enabled: false`
(`TEAMSTER_SAMPLES_ENABLED=false`) beendet Stichproben und Vervollständigung. Zuvor gespeicherte
Zeilen bleiben in der Tabelle `event_samples`, bis sie gelöscht werden.

## Vorlagen mit Beispielnutzdaten ausprobieren {#try-templates-against-sample-payloads}

Das Verzeichnis
[`samples/`](https://github.com/pflege-de-labs/teamster/tree/main/samples) im Repository enthält
Nutzdaten für jeden Webhook und Zustand. Senden Sie eine Datei mit `curl`, um die Vorlage an einer
echten Nachricht zu sehen:

```bash
curl -H "Authorization: Bearer <token>" -H 'Content-Type: application/json' \
  --data @samples/alertmanager-firing.json http://localhost:8080/webhook/alertmanager
```

| Beispiel | Zeigt |
| --- | --- |
| `alertmanager-firing.json`, `alertmanager-resolved.json` | Einen Alertmanager-Alarm, der sich öffnet und aufgelöst wird |
| `alertmanager-group.json` | Eine Alertmanager-Gruppe mit zwei Alarmen, einer feuernd und einer aufgelöst |
| `universal-open.json`, `universal-closed.json` | Ein verfolgtes universelles Ereignis |
| `universal-message.json` | Eine einmalige universelle Nachricht |
| `universal-direct-message.json` | Direkten Inhalt, für eine Route ohne Vorlage |
| `universal-password-expiry.json` | Eine Nachricht an die Personen, die sie nennt |
| `teamsv2-card.json`, `teamsv2-text.json`, `teamsv2-messagecard.json` | Die drei Formen des Teams-V2-Webhooks |

## Vorlagen aus der Zeit vor 0.9.0 umstellen {#migrate-templates-from-before-090}

Vorlagen, die auf die alten `.Alert`-Daten geschrieben waren, hat die Migration umgeschrieben, die
Ereignisse eingeführt hat: Aus `.Alert.Status` wurde `.Event.State`, aus `.Alert.Fingerprint`
`.Event.Key`, und `.Alert.Annotations` samt verwandter Felder zog nach `.Event.Alertmanager` um,
bei einer Vorlage, die nur den universellen Webhook verarbeitet, nach `.Event.Universal`. Vorlagen,
die Sie außerhalb von Teamster aufbewahren, brauchen dieselbe Änderung. Siehe
[ADR 0056](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0056-events-not-alerts.md).
