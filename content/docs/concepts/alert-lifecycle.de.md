---
title: Lebenszyklus eines Alarms
weight: 2
---

Teamster merkt sich, welche Nachricht es zu welchem Alarm gesendet hat. Nur deshalb kann es eine
Karte aktualisieren, solange ein Alarm aktiv ist, und sie ändern, wenn der Alarm aufgelöst wird,
statt jedes Mal eine neue Karte zu posten.

## Ereignisse und Keys {#events-and-keys}

Jeder Webhook wird in ein *Ereignis* umgewandelt. Jeder Alarm in einer Alertmanager-Benachrichtigung
ist ein eigenes Ereignis; ein Aufruf des universellen Webhooks ist ein Ereignis.

Der **Key** eines Ereignisses gibt an, welches frühere Ereignis es fortsetzt:

* Alertmanager: der Fingerprint des Alarms.
* Universeller Webhook: der `key` der Nutzdaten. Fehlt er, leitet Teamster einen Key aus der
  Quelle, der `url`, der `time`, den Labels und den Empfängern ab. Senden Sie einen eigenen `key`,
  wenn sich einer dieser Werte zwischen Aktualisierungen desselben Alarms ändert.

## Zustand {#state}

Der Zustand eines Ereignisses entscheidet, was mit ihm geschieht.

| Zustand | Alertmanager | Universeller Webhook | Was Teamster tut |
| --- | --- | --- | --- |
| offen | `firing` | `"state": "open"` | postet eine Nachricht oder aktualisiert die, die es für diesen Key gepostet hat |
| geschlossen | `resolved` | `"state": "closed"` | aktualisiert die Nachricht für diesen Key oder sendet eine Folgenachricht und vergisst ihn dann |
| keiner | jeder andere Status | kein `state` | stellt einmal zu und merkt sich nichts |

Einen universellen `state` außer `open` oder `closed` lehnt Teamster mit `400` ab.

## Offen: einmal posten, dann aktualisieren {#open-post-once-then-update}

Das erste offene Ereignis für einen Key postet an jedes Zustellziel, das seine Routen nennen, eine
Nachricht. Teamster speichert die Nachrichten-ID je Kanal und je Person. Ein wiederholtes offenes
Ereignis mit demselben Key bearbeitet diese Nachrichten an Ort und Stelle.

## Geschlossen: auflösen {#closed-resolve}

Ein geschlossenes Ereignis sucht die Nachrichten, die zu seinem Key gespeichert sind, nicht die, die
die Routen jetzt wählen würden. Eine Karte wird also auch dann aufgelöst, wenn Sie die Routen
geändert haben, über die sie kam.

* **In einem Kanal** wird die Karte ein letztes Mal bearbeitet und zeigt den aufgelösten Zustand.
* **In einem Chat** sendet Teamster eine neue Nachricht. Teams benachrichtigt bei einer Bearbeitung
  niemanden; eine an Ort und Stelle geschlossene Karte ließe die Person in Rufbereitschaft nie
  erfahren, dass der Alarm aufgelöst ist.

Danach wird der gespeicherte Datensatz gelöscht. Ein geschlossenes Ereignis für einen Key ohne
offene Nachricht stellt nichts zu.

{{< callout type="info" >}}
Alertmanager sendet Benachrichtigungen über aufgelöste Alarme nur mit `send_resolved: true` in
seiner Webhook-Konfiguration. Ohne diese Einstellung bleiben Karten offen. Siehe
[Alarme aus Alertmanager senden](../../guides/alertmanager/).
{{< /callout >}}

## Kein Zustand: senden und vergessen {#no-state-fire-and-forget}

Ein Ereignis ohne Zustand wird einmal gerendert, geroutet und zugestellt. Nichts wird gespeichert;
sendet man es erneut, entsteht eine zweite Nachricht. Diese Form passt zu Absendern ohne eigenen
Lebenszyklus und entspricht dem Verhalten des Teams-V2-Webhooks.

## Mehrere Zustellziele und Fehler {#fan-out-and-failures}

Ein Ereignis kann mehrere Kanäle und Personen erreichen. Jedes Zustellziel wird einzeln zugestellt
und verfolgt:

* Schlägt ein Zustellziel fehl, hält das die anderen nicht auf. Der Webhook antwortet dann mit
  `502`.
* Die Wiederholung des Absenders aktualisiert die bereits angekommenen Nachrichten, statt sie zu
  verdoppeln.
* Eine Person, die den Bot deinstalliert oder blockiert hat, zählt als `blocked` statt als
  fehlgeschlagen und wird für dieses Ereignis nicht erneut versucht. Das nächste Ereignis versucht
  es wieder.

## Eine Karte, auch bei gleichzeitigen Zustellungen {#one-card-even-under-concurrent-deliveries}

Vor dem Posten beansprucht Teamster das Recht, die Karte für diesen Key und Kanal zu posten. Kommt
dasselbe Ereignis zweimal gleichzeitig an, etwa bei zwei Replikaten, gewinnt eines und postet. Das
andere erhält ein `502`, und seine Wiederholung bearbeitet die Karte, die der Gewinner gepostet hat.

Einen Anspruch, den ein abgestürzter Prozess hinterlassen hat, übernimmt der nächste Versuch, sobald
`max(30s, 3 × bot.timeout-sec)` vergangen ist. Es gibt keinen Aufräumjob; die Wiederherstellung
geschieht bei der nächsten Zustellung.

## Verwandte Themen {#related}

* Welche Zustellziele ein Ereignis erreicht: [Routing]({{< ref "/docs/concepts/routing" >}}).
* Die Felder der Nutzdaten: [Webhook-Nutzdaten](../../reference/webhook-payloads/).
* Zustellergebnisse als Metriken: [Metriken](../../reference/metrics/).
