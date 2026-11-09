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
* Ein leerer Selektor trifft auf jeden Alarm zu. Eine Wurzelroute ohne Selektor erhält jeden Alarm
  von jedem Webhook, neben jeder anderen Wurzelroute, die zutrifft. Die Standardroute bleibt eine
  Rückfallebene, sodass eine Wurzelroute ohne Selektor ihr nichts übrig lässt. Siehe
  [Rückfallebenen](#fallbacks).

Bei einer Benachrichtigung aus Alertmanager sind es ihre `commonLabels`, die Labels, die alle
Alarme der Gruppe gemeinsam haben; bei einer Gruppe mit einem Alarm die Labels dieses Alarms. Bei
einem universellen Ereignis sind es die Einträge seines `labels`-Objekts.

### Das Label `teamster_source` {#the-teamster_source-label}

Vor dem Routing setzt Teamster das Label `teamster_source` auf den Webhook, an dem das Ereignis
angekommen ist: `alertmanager` oder `universal`. Einen vom Absender gesetzten Wert überschreibt es.
Eine Route kann daher nach der Quelle auswählen, zum Beispiel mit
`{"teamster_source": "alertmanager"}`.

### Die Labels `teamster_team` und `teamster_channel` {#the-teamster_team-and-teamster_channel-labels}

Eine Teams-V2-Nachricht kommt mit `teamster_source=teamsv2` an, dazu mit den beiden URL-Segmenten,
an die sie gepostet wurde, als `teamster_team` und `teamster_channel`. Nur die URL setzt sie:
Teamster entfernt alle drei aus den Labels eines Absenders an jedem anderen Webhook.

Teams-V2-Nachrichten werden wie jedes andere Ereignis geroutet. Jeder Teams-V2-Endpunkt hat eine
verknüpfte Route, die diese drei Labels selektiert und mit der Vorlage des Endpunkts an sein Ziel
zustellt. Sie wird mit dem Endpunkt angelegt, aktualisiert und gelöscht, und der Bereich **Routen**
kennzeichnet sie mit **Verwaltet vom Webhook `<team>/<channel>`**. Ihr Selektor, Zustellziel,
Vorlage, übergeordnete Route, Standard-Markierung und die Einstellung „untergeordnete Routen stellen
ebenfalls zu“ (siehe [Verschachtelte Routen](#nested-routes)) sind gesperrt; Name, Priorität und
die Markierung „verdrängend“ nicht. Sie können die Route nicht löschen, solange ihr Endpunkt besteht,
und den Endpunkt nicht, solange die Route untergeordnete Routen hat.

Weil die Nachricht durch das Routing läuft, stellt auch jede andere Route zu, die dieselben Labels
selektiert. Siehe
[Die Nachrichten an weitere Kanäle senden](../../guides/teams-v2-webhook/#send-the-messages-to-more-channels).
Eine untergeordnete Route der verknüpften Route stellt immer zusätzlich zu ihr zu; der eigene Kanal
des Endpunkts erhält die Nachricht also immer.

Ein Endpunkt ohne verknüpfte Route, etwa einer aus einem früheren Release, postet direkt an sein
Ziel. Teamster legt die fehlende Route beim Start an.

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
untergeordnete Route ohne Selektor erhält alles, was ihre übergeordnete erhält: So senden Sie alle
Alarme einer Route in einen weiteren Kanal. Sie muss dennoch ein Ziel oder eine Vorlage ergänzen
und kann nicht verdrängend sein; das Speichern wird abgewiesen.

* **Zusätzlich.** Standardmäßig stellt eine zutreffende untergeordnete Route zusätzlich zu ihrer
  übergeordneten zu.
* **Stattdessen.** Eine *verdrängende* (greedy) untergeordnete Route stellt statt ihrer
  übergeordneten zu. Sie unterdrückt alle Zustellungen der übergeordneten Route, nicht nur die
  eine derselben Art.
* **Vererbung.** Eine untergeordnete Route ohne eigenes Zustellziel oder ohne eigene Vorlage
  übernimmt die des nächsten Vorfahren. „Dieselbe Karte an einen weiteren Kanal“ ist eine
  untergeordnete Route, in der ein einziges Feld gesetzt ist.
* **Ersetzen verhindern.** Setzen Sie bei einer Route das Häkchen bei
  **Untergeordnete Routen stellen zusätzlich zu dieser Route zu, nie an ihrer Stelle**, und jede
  untergeordnete Route darunter stellt zusätzlich zu ihr zu. Das Speichern einer verdrängenden
  untergeordneten Route unter einer solchen Route wird mit `route "<name>" keeps its own
  deliveries, so a child route can only deliver as well as it` abgewiesen. Hat die Route bereits
  verdrängende untergeordnete Routen, können Sie das Häkchen trotzdem setzen: Sie stellen dann
  zusätzlich zu ihr zu, und die Routenliste zeigt sie als *zusätzlich zur übergeordneten*. Das
  Häkchen schützt nur diese Route; eine verdrängende Enkelroute ersetzt weiterhin ihre eigene
  übergeordnete Route. In der API heißt das Feld `no_greedy_children`, und Export und Import
  übernehmen es.
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

`/admin/routing` zeichnet den Weg eines Ereignisses. Der Graph füllt die Seite und liest sich von
links nach rechts: die Webhooks und Teams-V2-Endpunkte, an denen Ereignisse ankommen, die Routen,
ihre untergeordneten Routen in einer Spalte rechts daneben, und die Kanäle und Personen, an die sie
zustellen.

Jede Spalte außer der ersten ist so geordnet, dass sich möglichst wenige Linien kreuzen. Der Knoten
**Global default** und die Standardroute stehen oben in der Routenspalte, damit die Linien, die
Ereignisse ohne passende Route nehmen, gerade oben entlang verlaufen. Die Spalte zeigt deshalb nicht
die Reihenfolge, in der die Routen ausgewertet werden: Die Priorität einer Route steht an ihrer
Beschriftung (zum Beispiel `· p100`) und in ihrer Hover-Karte, und der Bereich **Routen** listet die
Routen in der Reihenfolge ihrer Auswertung. In der Zielspalte können Kanäle und Personen gemischt
stehen.

Verbindungen mit denselben Vorgängern teilen sich eine Spur, wie in einem Liniennetzplan. Jede
Linie hat die Farbe des Webhooks oder Teams-V2-Endpunkts, von dem ihre Alarme stammen, und eine
Linie, die Alarme mehrerer Webhooks trägt, verläuft als mehrere Stränge nebeneinander. Auf dem Tab
**Vorlagen** ist stattdessen die Vorlage die Farbe.

* Ein Symbol zeigt, was ein Knoten ist: ein Winkel für einen Webhook, ein doppelter Winkel für
  einen Teams-V2-Endpunkt, eine Gabelung für eine Route, ein Pfeil in einen Balken für ein Ziel,
  eine Person für einen Chat und `@` für jeden, der in der Nachricht genannt wird. Fehlende Knoten
  (rot) und Teams ohne installierte Teams-App (gelb) haben eigene Markierungen.
* Eine Route zeigt die Labels, die sie auswählt, oder *no selector — matches every alert*, und die
  Vorlage, mit der sie rendert; eine geerbte Vorlage ist gekennzeichnet. Die Routenliste sagt bei
  einem leeren Selektor **trifft auf jeden Alarm zu** und bei einer Standardroute mit Selektor
  **fängt auf, was keine andere Route trifft**.
* Eine gestrichelte Linie zwischen zwei Routen ist eine untergeordnete Route, die ihre
  übergeordnete verfeinert. Sie endet in einem Balken, wenn die untergeordnete Route *statt* der
  übergeordneten zustellt; sonst stellt sie *zusätzlich* zu ihr zu. Eine gepunktete Linie ist ein
  Teams-V2-Endpunkt, der ohne Routing direkt in seinen Kanal postet.
* Eine Route, die auf ein gelöschtes Ziel oder eine gelöschte Person zeigt, erscheint als
  fehlender Knoten.
* Der Tab **Vorlagen** ordnet Vorlagen den Routen und Webhooks zu, die sie verwenden. Eine Vorlage,
  neben der nichts steht, wird nicht verwendet. Direkt öffnen Sie ihn mit
  `/admin/routing?tab=templates`.

Linien tragen keinen Text. Fahren Sie über einen Knoten oder eine Linie, um zu sehen, was sie ist
und tut: Selektor, Priorität, Vorlage und Warnungen der Route sowie eine Vorschau ihrer
Verbindungen. Klicken Sie einen Knoten an, oder fokussieren Sie ihn mit `Tab` und drücken Sie
`Enter`, um den Graphen nur mit diesem Knoten, allem, was zu ihm führt, und allem, wohin er führt,
neu zu zeichnen. Die Farben bleiben die des ganzen Graphen. Klicken Sie in diesem Ausschnitt einen
Knoten an, wird dessen Pfad gezeichnet. Zurück zum ganzen Graphen kommen Sie, indem Sie auf den
Hintergrund klicken, `Esc` drücken oder **Ganzen Graphen zeigen** nutzen; die Schaltfläche erscheint
nur, solange ein Ausschnitt gezeichnet ist. Ziehen Sie den Hintergrund zum Verschieben, scrollen Sie
zum Zoomen und nutzen Sie **Alles zeigen**, um das Gezeichnete einzupassen. Knoten lassen sich nicht
verschieben. Die Info-Schaltfläche (ⓘ) erklärt, wie Sie den Graphen lesen: Darüberfahren öffnet sie,
ein Klick hält sie fest, `Esc` schließt sie.

Tragen Sie `key=value`-Labels unter **Welche Routen treffen zu?** ein, um zu fragen, welche Routen
ein bestimmtes Ereignis nehmen würde. Jede Route, die zustellt, wird genannt und begründet (eine
Route ohne Selektor sagt, dass sie jeden Alarm nimmt), und ihr Weg bleibt hervorgehoben, während
der Rest verblasst.

Das Label-Feld und die **Legende** schweben über dem Graphen. Ziehen Sie sie an ihrem Titel,
klappen Sie sie ein oder aus (**Ein- oder ausklappen**), oder blenden Sie sie aus (**Ausblenden**)
und holen Sie sie über die Schaltflächen oben links zurück. Die Legende startet eingeklappt. Ihr
Browser merkt sich, wo Sie sie abgelegt haben.

## Verwandte Themen {#related}

* Wie eine zugestellte Karte aktualisiert und aufgelöst wird:
  [Lebenszyklus eines Alarms]({{< ref "/docs/concepts/alert-lifecycle" >}}).
* Was eine Vorlage sieht: [Vorlagendaten](../../reference/template-data/).
