---
title: Einen Absender von einem Teams-Workflows-Webhook umziehen
weight: 15
---

Ersetzen Sie die URL eines Webhooks aus Microsoft Teams „Workflows“ (Power Automate) durch eine von
Teamster. Teamster akzeptiert dieselben Nutzdaten wie dieser Webhook, ein Umzug bedeutet also,
eine URL zu ändern.

Der Endpunkt ist `POST /teamsv2/{team}/{channel}/{token}`. Es gibt keinen Header: Die URL ist der
Berechtigungsnachweis, wie zuvor auch.

## Den Absender umziehen {#move-the-sender}

{{% steps %}}

### Das Ziel anlegen {#create-the-destination}

Der Kanal, in den der Absender postet, muss in Teamster als Ziel existieren. Legen Sie ihn unter
**Konfiguration → Ziele** mit **Neues Ziel** an, falls nicht.

### Den Endpunkt anlegen {#create-the-endpoint}

Wählen Sie unter **Konfiguration → Webhooks** im Bereich **Teams-V2-Webhooks** das Ziel und geben
Sie der URL zwei lesbare Segmente für das Team und den Kanal. Segmente bestehen aus
Kleinbuchstaben, Ziffern und
Bindestrichen, sind bis zu 64 Zeichen lang und beginnen und enden mit einem Buchstaben oder einer
Ziffer.

Die Seite, die auf das Formular antwortet, zeigt die vollständige URL samt Token einmal an.
Teamster speichert vom Token nur einen Digest und kann es nicht erneut anzeigen.

Die URL beginnt mit `server.webhook-url`, wenn es gesetzt ist, sonst mit Schema und Host, unter
denen Sie die Verwaltungsoberfläche aufrufen. Liegt die Verwaltungsoberfläche auf einem eigenen
Hostnamen, setzen Sie `server.webhook-url` auf die Adresse, die Absender erreichen; sonst nennt die
URL einen Host, den sie nicht erreichen. Eine bereits ausgegebene URL mit falschem Host korrigieren
Sie im Absender; das Token bleibt gleich.

### Den Absender darauf ausrichten {#point-the-sender-at-it}

Ersetzen Sie in der Konfiguration des Absenders die alte Workflows-URL durch die neue, zum Beispiel
`https://teamster.example/teamsv2/platform/alerts/<token>`. Halten Sie sie so geheim wie die alte.

### Ausprobieren {#test-it}

```bash
curl -H 'Content-Type: application/json' -d '{"text": "something broke on node-3"}' \
  https://teamster.example/teamsv2/platform/alerts/<token>
```

Die Antwort `200` bedeutet, dass die Nachricht zugestellt wurde. Der Body ist der Bericht des
universellen Webhooks, etwa `{"status":"ok"}`.

{{% /steps %}}

Geht die URL verloren oder wird sie bekannt, verwenden Sie **Neues Token** am Endpunkt. Das ersetzt
das Token, und die alte URL funktioniert sofort nicht mehr.

## Was der Absender posten darf {#what-the-sender-may-post}

Teamster erkennt die Form am Body:

{{< tabs >}}
{{< tab name="Adaptive Card" >}}

```json
{
  "type": "message",
  "attachments": [
    {
      "contentType": "application/vnd.microsoft.card.adaptive",
      "content": {"type": "AdaptiveCard", "version": "1.4", "body": []}
    }
  ]
}
```

Die Karte wird Byte für Byte an Teams weitergereicht.

{{< /tab >}}
{{< tab name="Text" >}}

```json
{"text": "something broke on node-3"}
```

Der Text wird gegen dieselbe Allowlist bereinigt, die auch für Vorlagen gilt.

{{< /tab >}}
{{< tab name="MessageCard" >}}

```json
{
  "@type": "MessageCard",
  "themeColor": "D70000",
  "title": "Disk almost full",
  "text": "node-3 is at 94%",
  "sections": [{"facts": [{"name": "severity", "value": "critical"}]}],
  "potentialAction": [
    {"@type": "OpenUri", "name": "Open runbook", "targets": [{"os": "default", "uri": "https://example.test/runbook"}]}
  ]
}
```

Die veraltete Connector-Karte wird in eine Adaptive Card umgewandelt. Zwei Dinge gehen dabei
verloren:

* Andere Aktionen als `OpenUri` (`HttpPOST`, `ActionCard`, `InvokeAddInCommand`) entfallen. Jede
  davon setzt voraus, dass der Connector den Absender zurückruft.
* `themeColor` wird nach Farbton zu einem der Container-Stile von Adaptive Cards: Rot wird
  `attention`, Orange und Gelb `warning`, Grün `good`, Blau und Lila `accent`, Grau zu keinem Stil.

{{< /tab >}}
{{< /tabs >}}

Die Beispiele
[`teamsv2-card.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-card.json),
[`teamsv2-text.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-text.json)
und
[`teamsv2-messagecard.json`](https://github.com/pflege-de-labs/teamster/blob/main/samples/teamsv2-messagecard.json)
liegen im Repository.

## Die Nachricht mit einer Vorlage gestalten {#shape-the-message-with-a-template}

Standardmäßig werden die Nutzdaten so gepostet, wie sie ankommen, gefolgt von einer kleinen Karte,
die darauf hinweist, dass keine Vorlage festgelegt ist. Um die Nachricht zu gestalten, wählen Sie
im Formular des Endpunkts eine **Vorlage**. Angeboten werden nur Vorlagen, die Nutzdaten des
Teams-V2-Webhooks verarbeiten; siehe [Vorlagen schreiben](../templates/).

In einer solchen Vorlage gilt:

* `.Event.Title`, `.Event.Text` und `.Event.Card` enthalten die geparste Nachricht. `.Event.Text`
  ist bereits HTML.
* `.Event.Source` ist `teamsv2`. Es gibt kein `.Event.Alertmanager` und kein `.Event.Universal`.
* `.Payload` ist der Body wie gesendet, für Felder, die die geparste Form einebnet, etwa
  `{{ .Payload.themeColor }}` oder `{{ range .Payload.sections }}`.

Die Vorschau der Vorlage bietet ein Beispiel **Teams-V2-Webhook**, um sie daran auszuprobieren, und
ein Beispiel **Karte des Teams-V2-Webhooks mit langen Fakten**.

## Eine Karte korrigieren, die Teams schlecht darstellt {#fix-a-card-that-teams-renders-badly}

Teams quetscht die Titelspalte eines FactSets auf einen Buchstaben pro Zeile, wenn die Werte lang
sind. Die Karte des Absenders können Sie nicht ändern, aber eine Vorlage kann sie mit `cardParts`
zerlegen und denselben Inhalt anders anordnen. Zwei vorgefertigte Vorlagen tun das:

| Vorgefertigte Vorlage | Sendet |
| --- | --- |
| **Nachricht des Teams-V2-Webhooks als Text** | Den ersten Text der Karte als Titel, die übrigen Texte, die Fakten in einem Codeblock und die Links |
| **Karte des Teams-V2-Webhooks, Fakten untereinander** | Eine Karte, in der jeder Fakt als Überschrift über seinem Wert steht |

{{% steps %}}

### Die Vorlage anlegen

Klicken Sie unter **Konfiguration → Vorlagen** auf **Neue Vorlage** und wählen Sie unter **Mit einer
Vorlage beginnen** eine der beiden. Prüfen Sie sie in der Vorschau mit dem Beispiel **Karte des
Teams-V2-Webhooks mit langen Fakten**.

### Die Vorlage verwenden

Wählen Sie die Vorlage im Formular des Endpunkts. Um alle Endpunkte ohne eigene Vorlage zu
korrigieren, wählen Sie sie stattdessen unter **Standardvorlage je Webhook** als Vorlage für Teams
V2; siehe [Vorlagen schreiben](../templates/#set-the-default-template-per-webhook).

{{% /steps %}}

Die vorgefertigten Vorlagen werden nicht von selbst gespeichert: Der Teams-V2-Standard einer neuen
Installation reicht die Karte weiterhin unverändert durch. Nur Texte, Fakten und
`Action.OpenUrl`-Links der Karte bleiben erhalten; siehe
[`cardParts`](../../reference/template-data/#cardparts).

## Die Nachrichten an weitere Kanäle senden {#send-the-messages-to-more-channels}

Jeder Endpunkt hat eine Route, die seine Nachrichten an das eigene Ziel des Endpunkts sendet.
Fügen Sie Routen hinzu, die dieselben Nachrichten auswählen, um sie zusätzlich anderswohin
zuzustellen.

{{% steps %}}

### Die Route des Endpunkts finden {#find-the-endpoints-route}

Öffnen Sie **Konfiguration → Routen**. Die Route mit dem Namen
`webhook <team>/<channel>` ist mit **Verwaltet vom Webhook `<team>/<channel>`** gekennzeichnet.
Ihr Selektor ist `teamster_source=teamsv2`, `teamster_team=<team>` und
`teamster_channel=<channel>`. Das Formular des Endpunkts verweist als **Route dieses Webhooks**
darauf.

### Eine Route hinzufügen {#add-a-route}

Legen Sie eine Route mit einem Selektor aus den drei Labels an, oder mit `teamster_source=teamsv2`
und einem der beiden anderen, um mehrere Endpunkte zu erfassen. Wählen Sie Ziel und Vorlage. Beide
Routen stellen die Nachricht nun zu.

Für eine zusätzliche Bedingung machen Sie die Route zur untergeordneten Route der Endpunkt-Route
und nehmen ein eigenes Label in den Selektor auf, oder lassen den Selektor leer, um alles zu
erhalten, was die Endpunkt-Route erhält. Eine untergeordnete Route gilt zusätzlich zur
übergeordneten; sie kann nicht verdrängend sein.

{{% /steps %}}

{{< callout type="info" >}}
Eine untergeordnete Route der Endpunkt-Route stellt immer zusätzlich zu ihr zu, sodass der eigene
Kanal des Endpunkts seine Nachrichten weiterhin erhält. Bei der Route ist das Häkchen
**Untergeordnete Routen stellen zusätzlich zu dieser Route zu, nie an ihrer Stelle** gesetzt und
gesperrt, und das Speichern einer verdrängenden untergeordneten Route darunter wird abgewiesen.
Eine verdrängende untergeordnete Route, die schon vorher bestand, stellt jetzt zusätzlich zur Route
zu; erneutes Speichern wird abgewiesen, bis Sie
„verdrängend“ abwählen. Der Endpunkt lässt sich nicht löschen, solange er untergeordnete Routen
hat. Siehe [Routing]({{< ref "/docs/concepts/routing" >}}).
{{< /callout >}}

Schlägt eine Zustellung fehl, lautet die Antwort `502`, und ein Absender, der es erneut versucht,
kann die erfolgreichen Zustellungen doppelt auslösen.

## Was er nicht kann {#what-it-does-not-do}

* **Keine Verfolgung.** Die Nutzdaten tragen weder Schlüssel noch Zustand, eine Nachricht wird also
  nie aktualisiert oder geschlossen. Dafür gibt es den [universellen Webhook](../universal-webhook/).

## Die Antwort prüfen {#check-the-answer}

| Status | Bedeutung |
| --- | --- |
| `200` | Gepostet. |
| `400` | Der Body enthält weder Text noch eine Karte. |
| `401` | Falsches Token. |
| `404` | Für dieses Team und diesen Kanal ist kein Endpunkt eingerichtet. |
| `413` | Der Body ist größer als 128 KiB. |
| `502` | Teams oder die Datenbank sind ausgefallen, oder die Vorlage des Endpunkts ließ sich nicht rendern. |
