---
title: Vorlagendaten
weight: 3
---

Was eine Vorlage lesen und aufrufen kann. Vorlagen verwenden die Syntax von Gos
[`text/template`](https://pkg.go.dev/text/template).

## Bestandteile einer Vorlage {#template-parts}

Eine Vorlage hat drei Bestandteile. Jeder wird mit denselben Daten gerendert.

| Bestandteil | Gerendert als | Hinweise |
| --- | --- | --- |
| Titel | Reiner Text | Leerraum, Zeilenumbrüche eingeschlossen, wird zu einzelnen Leerzeichen zusammengezogen. Erscheint als Vorschau im Aktivitätsfeed. |
| Text | Markdown, danach bereinigtes HTML | Rohes HTML durchläuft Markdown und wird danach bereinigt. |
| Karte | Adaptive-Card-JSON | Die Ausgabe muss gültiges JSON sein. Eine leere Ausgabe oder eine nur aus Leerraum sendet keine Karte. |

Eine Vorlage braucht mindestens einen Bestandteil. Eine Vorlage mit Karte und ohne Titel erhält
diesen Titel:

```gotemplate
{{ $summary := "" }}{{ with .Event.Alertmanager }}{{ $summary = index .Annotations "summary" }}{{ end }}{{ with .Event.Universal }}{{ $summary = index .Attributes "summary" }}{{ end }}{{ default $summary (default .Event.Labels.alertname "Update") }}
```

## Felder der obersten Ebene {#top-level-fields}

| Feld | Typ | Enthält |
| --- | --- | --- |
| `.Event` | Objekt | Das normalisierte Ereignis, siehe [Ereignis](#event). |
| `.Now` | String | Zeitpunkt des Renderns in UTC, RFC 3339, etwa `2025-12-07T20:07:00Z`. |
| `.Payload` | beliebig | Der Anfragekörper als dekodiertes JSON. Nur beim Teams-V2-Webhook; nil bei allen anderen Webhooks. |
| `.Recipient` | Objekt | Die Person, für die eine Chat-Nachricht gerendert wird, siehe [Empfänger](#recipient). Leer bei einem Kanal. |

## Ereignis {#event}

Jedes Ereignis hat diesen Kern, gleich an welchem Webhook es ankam.

| Feld | Typ | Enthält |
| --- | --- | --- |
| `.Event.Source` | String | `alertmanager`, `universal` oder `teamsv2`. |
| `.Event.Key` | String | Was das Ereignis über mehrere Posts hinweg kennzeichnet. Wird abgeleitet, wenn der Absender keinen angibt. Leer bei Teams V2. |
| `.Event.State` | String | `open`, `closed` oder leer für eine Nachricht, die einmal zugestellt wird. |
| `.Event.Labels` | Map von Strings | Worauf Routen selektieren, `teamster_source` eingeschlossen. |
| `.Event.Title` | String | Direkter Titel, wenn der Absender einen mitgeschickt hat. |
| `.Event.Text` | String | Direkter Text, wenn der Absender ihn mitgeschickt hat. |
| `.Event.Card` | JSON | Direkte Adaptive Card, wenn der Absender eine mitgeschickt hat. Bei Teams V2 die erste Karte der Nachricht. |
| `.Event.Alertmanager` | Objekt oder nil | Alertmanager-Erweiterung, siehe unten. Nil bei allen anderen Webhooks. |
| `.Event.Universal` | Objekt oder nil | Erweiterung des universellen Webhooks, siehe unten. Nil bei allen anderen Webhooks. |

### Alertmanager-Erweiterung {#alertmanager-extension}

Gesetzt für ein Ereignis von `POST /webhook/alertmanager`. Jeder Alarm einer Benachrichtigung ist
ein eigenes Ereignis; die Gruppenfelder wiederholen sich in jedem.

| Feld | Typ | Aus den Nutzdaten |
| --- | --- | --- |
| `.Event.Alertmanager.Annotations` | Map von Strings | `alerts[].annotations` |
| `.Event.Alertmanager.StartsAt` | Zeit | `alerts[].startsAt` |
| `.Event.Alertmanager.EndsAt` | Zeit | `alerts[].endsAt` |
| `.Event.Alertmanager.GeneratorURL` | String | `alerts[].generatorURL` |
| `.Event.Alertmanager.Receiver` | String | `receiver` |
| `.Event.Alertmanager.GroupKey` | String | `groupKey` |
| `.Event.Alertmanager.GroupLabels` | Map von Strings | `groupLabels` |
| `.Event.Alertmanager.CommonLabels` | Map von Strings | `commonLabels` |
| `.Event.Alertmanager.CommonAnnotations` | Map von Strings | `commonAnnotations` |
| `.Event.Alertmanager.ExternalURL` | String | `externalURL` |

Auch die Kernfelder stammen aus dem Alarm: `.Event.Key` aus `fingerprint`, `.Event.Labels` aus
`labels` und `.Event.State` aus `status` (`firing` wird `open`, `resolved` wird `closed`, alles
andere bleibt leer).

### Universelle Erweiterung {#universal-extension}

Gesetzt für ein Ereignis von `POST /webhook/universal`.

| Feld | Typ | Aus den Nutzdaten |
| --- | --- | --- |
| `.Event.Universal.Attributes` | Map von Strings | `attributes` |
| `.Event.Universal.Time` | Zeit | `time` |
| `.Event.Universal.URL` | String | `url` |
| `.Event.Universal.Recipients` | Liste von Strings | `recipients` |

### Teams V2 {#teams-v2}

Ein Ereignis von `POST /teamsv2/…` hat keine Erweiterung.

| Feld | Enthält |
| --- | --- |
| `.Event.Source` | `teamsv2` |
| `.Event.Labels` | Nur `teamster_source`. |
| `.Event.Title`, `.Event.Text`, `.Event.Card` | Die geparste Nachricht. `.Event.Text` ist bereits HTML. |
| `.Payload` | Der Körper so, wie er gesendet wurde, für Felder, die die geparste Form glättet, etwa `{{ .Payload.themeColor }}`. |

### Nil-Erweiterungen {#nil-extensions}

Das Lesen eines Felds einer nil-Erweiterung lässt das Rendern scheitern. Eine Vorlage für einen
einzigen Webhook liest dessen Erweiterung direkt. Eine Vorlage für jeden Webhook umschließt jede
Erweiterung mit `with`:

```gotemplate
{{ with .Event.Alertmanager }}{{ .Annotations.summary }}{{ end }}
{{ with .Event.Universal }}{{ .Attributes.summary }}{{ end }}
```

Einen Label-Schlüssel, der nicht hinter einem Punkt stehen kann, liest `index`:

```gotemplate
{{ index .Event.Labels "app.kubernetes.io/name" }}
```

## Empfänger {#recipient}

Gesetzt, wenn eine Route in den Chat einer Person zustellt.

| Feld | Enthält | In der Nachricht genannte Person | Verknüpfter Chat |
| --- | --- | --- | --- |
| `.Recipient.ID` | Entra-Objekt-ID | ja | ja |
| `.Recipient.DisplayName` | Anzeigename | ja | ja |
| `.Recipient.GivenName` | Vorname | ja | leer |
| `.Recipient.Surname` | Nachname | ja | leer |
| `.Recipient.UPN` | User Principal Name | ja | leer |
| `.Recipient.Mail` | E-Mail-Adresse | ja | leer |

Bei einem Kanal sind alle Felder leer.

## Funktionen {#functions}

| Funktion | Signatur | Ergibt |
| --- | --- | --- |
| `toJSON` | `toJSON <value>` | `value` als JSON kodiert. Lässt das Rendern scheitern, wenn sich der Wert nicht kodieren lässt. |
| `default` | `default <value> <fallback>` | `value`, wenn es ein nicht leerer String ist, sonst `fallback`, wenn das ein String ist, sonst einen leeren String. Funktioniert auch mit einem fehlenden Map-Schlüssel. |

Die eingebauten Funktionen von `text/template` stehen ebenfalls zur Verfügung: `and`, `or`, `not`,
`len`, `index`, `slice`, `print`, `printf`, `println`, `html`, `js`, `urlquery`, `call` und die
Vergleiche `eq`, `ne`, `lt`, `le`, `gt`, `ge`.

```gotemplate
{{ default .Event.Labels.severity "unknown" }}
{{ toJSON .Event.Labels }}
```

## Siehe auch {#see-also}

* [Vorlagen schreiben](../../guides/templates/)
* [Webhook-Nutzdaten](../webhook-payloads/)
