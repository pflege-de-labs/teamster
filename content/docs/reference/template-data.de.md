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
{{ $summary := "" }}{{ with .Event.Alertmanager }}{{ $summary = default (index .Annotations "summary") (index .CommonAnnotations "summary") }}{{ end }}{{ with .Event.Universal }}{{ $summary = index .Attributes "summary" }}{{ end }}{{ default $summary (default .Event.Labels.alertname "Update") }}
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

Gesetzt für ein Ereignis von `POST /webhook/alertmanager`. Eine Benachrichtigung ist ein
Ereignis, gleich wie viele Alarme sie gruppiert.

| Feld | Typ | Aus den Nutzdaten |
| --- | --- | --- |
| `.Event.Alertmanager.Alerts` | Liste von Alarmen | `alerts`, in der gesendeten Reihenfolge, siehe unten |
| `.Event.Alertmanager.Annotations` | Map von Strings | `alerts[0].annotations`, nur bei einer Gruppe mit einem Alarm |
| `.Event.Alertmanager.StartsAt` | Zeit | `alerts[0].startsAt`, nur bei einer Gruppe mit einem Alarm |
| `.Event.Alertmanager.EndsAt` | Zeit | `alerts[0].endsAt`, nur bei einer Gruppe mit einem Alarm |
| `.Event.Alertmanager.GeneratorURL` | String | `alerts[0].generatorURL`, nur bei einer Gruppe mit einem Alarm |
| `.Event.Alertmanager.Receiver` | String | `receiver` |
| `.Event.Alertmanager.GroupKey` | String | `groupKey` |
| `.Event.Alertmanager.GroupLabels` | Map von Strings | `groupLabels` |
| `.Event.Alertmanager.CommonLabels` | Map von Strings | `commonLabels` |
| `.Event.Alertmanager.CommonAnnotations` | Map von Strings | `commonAnnotations` |
| `.Event.Alertmanager.ExternalURL` | String | `externalURL` |

Bei einer Gruppe mehrerer Alarme sind `Annotations`, `StartsAt`, `EndsAt` und `GeneratorURL` leer.
`len .Event.Alertmanager.Alerts` unterscheidet die beiden Fälle.

Jeder Eintrag von `.Event.Alertmanager.Alerts`:

| Feld | Typ | Aus den Nutzdaten |
| --- | --- | --- |
| `.Status` | String | `alerts[].status`: `firing` oder `resolved` |
| `.Labels` | Map von Strings | `alerts[].labels` |
| `.Annotations` | Map von Strings | `alerts[].annotations` |
| `.StartsAt` | Zeit | `alerts[].startsAt` |
| `.EndsAt` | Zeit | `alerts[].endsAt` |
| `.GeneratorURL` | String | `alerts[].generatorURL` |
| `.Fingerprint` | String | `alerts[].fingerprint` |

Die Kernfelder stammen aus der Gruppe: `.Event.Key` ist ein Hash von `groupKey`, `.Event.Labels`
stammt aus `commonLabels` und `.Event.State` aus `status` (`firing` wird `open`, `resolved` wird
`closed`, alles andere bleibt leer). Für Nutzdaten, denen diese Felder fehlen, siehe
[Alertmanager](../webhook-payloads/#alertmanager).

### Universelle Erweiterung {#universal-extension}

Gesetzt für ein Ereignis von `POST /webhook/universal`.

| Feld | Typ | Aus den Nutzdaten |
| --- | --- | --- |
| `.Event.Universal.Attributes` | Map von Strings | `attributes` |
| `.Event.Universal.Time` | Zeit | `time` |
| `.Event.Universal.URL` | String | `url` |
| `.Event.Universal.Recipients` | Liste von Strings | `recipients` |
| `.Event.Universal.Broadcast` | bool | `broadcast` |

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

Gesetzt, wenn eine Route in den Chat einer Person zustellt. Die Felder stammen aus dem Entra-Profil
der Person. Ein Feld, das Entra für jemanden nicht kennt, ist leer.

| Feld | Typ | Enthält |
| --- | --- | --- |
| `.Recipient.ID` | string | Entra-Objekt-ID |
| `.Recipient.DisplayName` | string | Anzeigename |
| `.Recipient.GivenName` | string | Vorname |
| `.Recipient.Surname` | string | Nachname |
| `.Recipient.UPN` | string | User Principal Name |
| `.Recipient.Mail` | string | E-Mail-Adresse |
| `.Recipient.JobTitle` | string | Position |
| `.Recipient.Department` | string | Abteilung |
| `.Recipient.CompanyName` | string | Firmenname |
| `.Recipient.OfficeLocation` | string | Bürostandort |
| `.Recipient.EmployeeID` | string | Personalnummer |
| `.Recipient.Address.Street` | string | Straße der Geschäftsadresse |
| `.Recipient.Address.PostalCode` | string | Postleitzahl der Geschäftsadresse |
| `.Recipient.Address.City` | string | Ort der Geschäftsadresse |
| `.Recipient.Address.State` | string | Bundesland der Geschäftsadresse |
| `.Recipient.Address.Country` | string | Land der Geschäftsadresse |
| `.Recipient.BusinessPhones` | Liste von string | Geschäftliche Telefonnummern |
| `.Recipient.MobilePhone` | string | Mobilnummer |
| `.Recipient.PreferredLanguage` | string | Bevorzugte Sprache, etwa `de-DE` |
| `.Recipient.UsageLocation` | string | Nutzungsstandort, ein Ländercode wie `DE` |

Welche Felder gefüllt sind, hängt vom Chat ab:

| Chat | Felder |
| --- | --- |
| In der Nachricht genannte Person, oder per Rundsendung über das Verzeichnis erreicht | Alle |
| Verknüpfter Chat einer Person, die im Verzeichnis steht | Alle |
| Verknüpfter Chat einer Person, die nicht im Verzeichnis steht | Nur `ID` und `DisplayName` |
| Kanal | Keine |

Nach einem Upgrade von einem Release ohne diese Felder erhält eine Person, die bereits im
Verzeichnis steht, sie beim nächsten Installationslauf (`bot.reconcile-interval`) oder wenn eine
Nachricht sie nach Ablauf von `bot.directory-ttl` erneut nachschlägt. Bis dahin sind sie leer.

### Das Ereignis, das eine Person sieht {#the-event-a-person-sees}

Wenn eine Route an die in einer Nachricht genannten Personen zustellt, wird die Nachricht jeder
Person aus einer Kopie des Ereignisses gerendert, die nur sie nennt:

* `.Event.Universal.Recipients` enthält nur die Adressen, die diese Person genannt haben.
* Das Label `teamster_recipient` in `.Event.Labels`, `.Event.Alertmanager.CommonLabels` und den
  Labels jedes Alarms enthält nur die Adressen, die diese Person genannt haben. Nannte es nur
  andere, fehlt es.
* `.Event.Alertmanager.Alerts` enthält nur die Alarme, die diese Person nennen, und die Alarme, die
  niemanden nennen. Bleibt ein Alarm übrig, werden `Annotations`, `StartsAt`, `EndsAt` und
  `GeneratorURL` aus ihm gefüllt, wie bei einer Gruppe mit einem Alarm.

Ein Kanal und eine Route an einen einzelnen verknüpften Chat sehen das ganze Ereignis.

## Funktionen {#functions}

| Funktion | Signatur | Ergibt |
| --- | --- | --- |
| `toJSON` | `toJSON <value>` | `value` als JSON kodiert. Lässt das Rendern scheitern, wenn sich der Wert nicht kodieren lässt. |
| `default` | `default <value> <fallback>` | `value`, wenn es ein nicht leerer String ist, sonst `fallback`, wenn das ein String ist, sonst einen leeren String. Funktioniert auch mit einem fehlenden Map-Schlüssel. |

Die eingebauten Funktionen von `text/template` stehen ebenfalls zur Verfügung: `and`, `or`, `not`,
`len`, `index`, `slice`, `print`, `printf`, `println`, `html`, `js`, `urlquery`, `call` und die
Vergleiche `eq`, `ne`, `lt`, `le`, `gt`, `ge`.

Ebenso die Funktionen von [sprig](https://masterminds.github.io/sprig/) – für Strings, reguläre
Ausdrücke, Listen, Dicts, Semver und mehr – mit diesen Ausnahmen:

| Ausgenommen | Grund |
| --- | --- |
| `env`, `expandenv` | Sie würden Teamsters Geheimnisse jedem zeigen, der eine Vorlage bearbeiten darf. |
| `getHostByName` | Sie greift auf das Netzwerk zu. |
| `now`, `date`, `dateInZone`, `dateModify`, `htmlDate`, `htmlDateInZone`, die `rand…`-Funktionen, `uuidv4` | Ihr Ergebnis ist nicht wiederholbar. Die Zeit liefert `.Now`. |
| `bcrypt`, `htpasswd`, `derivePassword`, `encryptAES`, `decryptAES`, die `gen…`-Funktionen und `buildCustomCert` | Sie kosten bei jeder Vorschau CPU und haben in einer Nachricht keinen Nutzen. |

`toJSON` und `default` sind Teamsters eigene Funktionen und haben Vorrang vor denen von sprig.
Beachten Sie die Reihenfolge der Argumente von `default`: Der Wert kommt zuerst. Die Pipeline-Form
aus der Dokumentation von sprig, `.x | default "y"`, ergibt immer `"y"`; schreiben Sie
`default .x "y"`.

```gotemplate
{{ default .Event.Labels.severity "unknown" }}
{{ toJSON .Event.Labels }}
{{ regexFind "^[a-z]+" .Event.Universal.Attributes.commit_message }}
{{ splitList "\n" .Event.Universal.Attributes.commit_message | first | trunc 80 }}
```

## Siehe auch {#see-also}

* [Vorlagen schreiben](../../guides/templates/)
* [Webhook-Nutzdaten](../webhook-payloads/)
