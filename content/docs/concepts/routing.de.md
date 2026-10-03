---
title: Routing
weight: 1
---

Das Routing entscheidet für jedes eingehende Ereignis, wohin es zugestellt wird und welche Vorlage
es rendert. Dabei betrachtet es ausschließlich die Labels des Ereignisses.

## Label-Selektoren {#label-selectors}

Eine Route hat einen Label-Selektor: eine Menge von `key: value`-Paaren, etwa
`{"severity": "critical"}`. Ein Selektor trifft auf ein Ereignis zu, wenn jeder seiner Schlüssel
unter den Labels des Ereignisses mit genau demselben Wert vorkommt. Weitere Labels am Ereignis
spielen keine Rolle.

* Der Vergleich ist exakt. Es gibt keine Platzhalter, regulären Ausdrücke oder Negationen.
* Ein leerer Selektor trifft auf nichts zu. Eine Route, die alles auffangen soll, ist die
  Standardroute, die weiter unten beschrieben ist.

Bei einer Benachrichtigung aus Alertmanager sind es ihre `commonLabels`, die Labels, die alle
Alarme der Gruppe gemeinsam haben; bei einer Gruppe mit einem Alarm die Labels dieses Alarms. Bei
einem universellen Ereignis sind es die Einträge seines `labels`-Objekts.

### Das Label `teamster_source` {#the-teamster_source-label}

Vor dem Routing setzt Teamster das Label `teamster_source` auf den Webhook, an dem das Ereignis
angekommen ist: `alertmanager` oder `universal`. Einen vom Absender gesetzten Wert überschreibt es.
Eine Route kann daher nach der Quelle auswählen, zum Beispiel mit
`{"teamster_source": "alertmanager"}`.

Teams-V2-Nachrichten werden nicht geroutet: Jeder Teams-V2-Endpunkt postet in den Kanal, den er
nennt. Ihre Vorlagen sehen trotzdem `teamster_source=teamsv2`.

## Wurzelrouten und Priorität {#root-routes-and-priority}

Eine Route ohne übergeordnete Route ist eine Wurzelroute. Teamster wertet jede Wurzelroute aus, in
absteigender Priorität, bei gleicher Priorität nach Namen sortiert. **Jede Wurzelroute, deren
Selektor zutrifft, stellt zu.** Die Priorität legt die Reihenfolge der Zustellungen und der Routen
in der Verwaltungsoberfläche fest; sie lässt nicht einen Treffer einen anderen unterdrücken.

Zwei unabhängige Wurzelrouten, die beide `severity=critical` auswählen, lösen also beide aus. Soll
eine Route eine andere ablösen, ordnen Sie sie unter dieser Route ein und machen sie verdrängend,
wie im nächsten Abschnitt beschrieben.

## Verschachtelte Routen {#nested-routes}

Eine Route kann eine andere Route verfeinern. Eine untergeordnete Route wird nur betrachtet, wenn
ihre übergeordnete zugetroffen hat, und greift, wenn auch ihr eigener Selektor zutrifft. Eine
untergeordnete Route braucht einen eigenen Selektor.

* **Zusätzlich.** Standardmäßig stellt eine zutreffende untergeordnete Route zusätzlich zu ihrer
  übergeordneten zu.
* **Stattdessen.** Eine *verdrängende* (greedy) untergeordnete Route stellt statt ihrer
  übergeordneten zu. Sie unterdrückt alle Zustellungen der übergeordneten Route, nicht nur die
  eine derselben Art.
* **Vererbung.** Eine untergeordnete Route ohne eigenes Zustellziel oder ohne eigene Vorlage
  übernimmt die des nächsten Vorfahren. „Dieselbe Karte an einen weiteren Kanal“ ist eine
  untergeordnete Route, in der ein einziges Feld gesetzt ist.
* Jede zutreffende untergeordnete Route stellt zu, nicht nur die erste.
* Routen lassen sich höchstens fünf Ebenen tief verschachteln.

Eine Route mit untergeordneten Routen lässt sich nicht löschen. Entfernen Sie zuerst die
untergeordneten Routen oder hängen Sie sie um: Eine verwaiste Route würde zur Wurzelroute und auf
Alarme zutreffen, die ihre übergeordnete Route bisher herausgefiltert hat.

## Zustellziele {#targets}

Eine Route stellt an genau eine Art von Zustellziel zu:

| Zustellziel | Stellt zu an |
| --- | --- |
| Ein Ziel | einen Teams-Kanal |
| Eine Person | den Chat dieser Person mit dem Bot, sobald sie ihn verknüpft hat |
| Jede in der Nachricht genannte Person | alle, die das Ereignis in `recipients` oder im Label `teamster_recipient` nennt |

Eine Wurzelroute muss ein Zustellziel nennen. Eine untergeordnete Route, die das Zustellziel ihrer
übergeordneten übernimmt, muss eine andere Vorlage verwenden, sonst brächte sie nichts hinzu. Als
Person kann eine Route nur Ihren eigenen Chat nennen, es sei denn, Sie sind Administrator.

Wer Routen bearbeiten darf, darf auch eine Route an **Jede in der Nachricht genannte Person**
anlegen. Wen sie erreicht, entscheidet der Absender, nicht die Route: Ein Token, das nur seinen
Ersteller nennen darf, erreicht über sie nur diesen, wie weit der Selektor auch gefasst ist. Siehe
[Nachrichten an einzelne Personen senden](../../guides/direct-messages/).

Routen gelten allerdings global. Eine solche Route trifft auf jede Nachricht zu, die ihr Selektor
erfasst, gleich von welchem Absender, und rendert sie mit der Vorlage ihres Autors. Eine Route mit
weit gefasstem Selektor, etwa `{"teamster_source": "universal"}`, sendet daher den Personen, die
andere Absender nennen, eine zusätzliche Nachricht mit den Worten ihres Autors. Sie erreicht
niemanden, den diese Absender nicht ohnehin anschreiben dürften. Solche Routen finden Sie im
[Routing-Bild](#seeing-the-routes) und im [Änderungsprotokoll](../../guides/audit-trail/).

## Rückfallebenen {#fallbacks}

Trifft keine Wurzelroute zu, fällt Teamster in zwei Stufen zurück:

1. **Die Standardroute.** Eine als Standard markierte Wurzelroute wird nur verwendet, wenn keine
   andere Wurzelroute zugetroffen hat. Sie löst nie neben einem echten Treffer aus.
2. **Das globale Standardziel.** Wenn nichts zugetroffen hat und es keine Standardroute gibt, auch
   wenn überhaupt keine Routen existieren, geht das Ereignis an das globale Standardziel. Das erste
   Ziel, das Sie anlegen, wird dazu, und ein Administrator kann ein anderes Ziel zum Standard
   machen. Seine Vorlage wählen Sie in seiner Zeile im Bereich **Routen**; ohne Vorlage sendet es
   eine eingebaute Nachricht.

Nur wenn überhaupt kein Ziel existiert, wird ein Ereignis nirgendwohin zugestellt.

## Die Routen ansehen {#seeing-the-routes}

`/admin/routing` zeichnet den Weg eines Ereignisses: den Webhook, an dem es ankommt, die Routen in
der Reihenfolge ihrer Auswertung mit ihren untergeordneten Routen daran, und die Kanäle und
Personen, an die sie zustellen.

* Routenknoten zeigen die Labels, die sie auswählen, und die Vorlage, mit der sie rendern; eine
  geerbte Vorlage ist gekennzeichnet.
* Ein gestrichelter Pfeil zwischen zwei Routen ist eine Verfeinerung, beschriftet mit
  *zusätzlich zur übergeordneten* oder *statt der übergeordneten*.
* Eine Route, die auf ein gelöschtes Ziel oder eine gelöschte Person zeigt, erscheint als
  fehlender Knoten.
* Ein zweites Bild ordnet Vorlagen den Routen und Webhooks zu, die sie verwenden. Eine Vorlage,
  neben der nichts steht, wird nicht verwendet.

Tragen Sie `key=value`-Labels auf der Seite ein, um zu fragen, welche Routen ein bestimmtes
Ereignis nehmen würde. Jede Route, die zustellt, wird genannt und begründet, und ihr Weg wird
hervorgehoben, während der Rest verblasst.

## Verwandte Themen {#related}

* Wie eine zugestellte Karte aktualisiert und aufgelöst wird:
  [Lebenszyklus eines Alarms]({{< ref "/docs/concepts/alert-lifecycle" >}}).
* Was eine Vorlage sieht: [Vorlagendaten](../../reference/template-data/).
