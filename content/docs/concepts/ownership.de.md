---
title: Rollen und Eigentum
weight: 3
---

Teamster entscheidet in zwei Ebenen, wer was darf: eine **Rolle**, die aus Ihrem Identitätsanbieter
kommt und für die gesamte Konfiguration gilt, und **Berechtigungen an einzelnen Datensätzen**, die
Eigentümer vergeben. Beide werden als [Cedar](https://www.cedarpolicy.com/)-Richtlinien ausgewertet,
die in das Binary eingebaut sind. Wie Sie Rollen zuweisen, Datensätze teilen und Gruppen verwalten,
beschreibt [Rollen und Zugriff verwalten](../../guides/roles/).

## Rollen {#roles}

Drei Rollen sind eingebaut; jede schließt die darunterliegende ein:

| Rolle | Darf |
| --- | --- |
| `admin` (**Administrator**) | alles, auch alles, was spätere Releases hinzufügen |
| `editor` (**Bearbeiter**) | die Konfiguration lesen und ändern |
| `viewer` (**Betrachter**) | sie lesen und den eigenen Chat verknüpfen oder die Verknüpfung aufheben |

**Rollen werden eins zu eins über den Namen zugeordnet.** Eine Rolle `editor` beim Anbieter ist
die Teamster-Rolle `editor`. Eine Rolle mit einem anderen Namen, etwa `auditor`, kommt unter ihrem
eigenen Namen an. Sie gewährt nichts, solange keine Richtlinie sie nennt; deshalb ist es sicher,
alle Rollen durchzureichen.

**Die Rolle wird bei der Anmeldung festgelegt** und reist mit der Sitzung. Eine Änderung beim
Anbieter gilt ab der nächsten Anmeldung der Person. Wer in seinem Claim keine der drei Rollen
trägt, erhält `auth.default-role`; bleibt der Wert leer, meldet sich die Person ohne Zugriff an.

**Die lokale Anmeldung ist immer Administrator.** Sie ist der Weg zurück, wenn der
Identitätsanbieter ausgefallen oder sein Claim falsch ist.

### Eine Rolle auf Teams und Kanäle einschränken {#narrowing-a-role-to-teams-and-channels}

Ein Administrator kann einschränken, wohin eine Rolle zustellen darf, indem er ihr bestimmte Teams
oder Kanäle freigibt. Eine Rolle ohne Freigabe erreicht jedes Team und jeden Kanal. Sobald eine
Freigabe eine Rolle nennt, erreicht sie nur noch das, was ihre Freigaben nennen. Freigaben
begrenzen auch, was die Rolle sieht: Auswahllisten und Übersichten zeigen nur freigegebene Teams
und Kanäle. Administratoren werden nie eingeschränkt.

## Datensätze besitzen und teilen {#owning-and-sharing-records}

Wer eine Vorlage, ein Ziel, eine Route, einen Webhook-Endpunkt oder eine Gruppe anlegt, **besitzt**
sie. Der Eigentümer oder jeder, der `share` daran hält, kann einem Benutzer, einer Gruppe, einer
Anbietergruppe oder einer Rolle Aktionen an genau diesem Datensatz geben:

| Aktion | Erlaubt |
| --- | --- |
| `read` | den Datensatz sehen |
| `update` | ihn ändern |
| `delete` | ihn löschen |
| `attach` | Routen und Webhooks darauf verweisen lassen, etwa an ein Ziel routen oder mit einer Vorlage rendern |
| `share` | anderen geben, was man selbst hält |
| `own` | alles, auch das Eigentum weitergeben |

Niemand kann mehr vergeben, als er selbst hält. Wer `own` vergibt oder entzieht, wird dadurch
ebenfalls Eigentümer.

Teilen ergänzt Rollen, es ersetzt sie nicht. Bearbeiter dürfen weiterhin alles bearbeiten. Wer
keine Rolle hat, aber einen geteilten Datensatz, sieht unter `/admin` nur das Geteilte. Seiten,
die aus der gesamten Konfiguration entstehen, bleiben für diese Person geschlossen: das
Routing-Bild, Vorschauen und der Export.

## Gruppen {#groups}

Eine Gruppe fasst Benutzer, andere Gruppen und die Gruppen zusammen, die Ihr Identitätsanbieter in
`auth.groups-claim` sendet. Berechtigungen an einem Datensatz vergeben Sie an eine Gruppe genauso
wie an einen Benutzer, sodass der Zugriff der Mitgliedschaft folgt. Gruppen lassen sich
verschachteln, aber eine Gruppe kann nie sich selbst enthalten.

Gruppen gehören zu einer Installation: Ein Konfigurationspaket nimmt sie nicht mit.

## Zugriff auf Webhooks {#webhook-access}

An einen Webhook zu senden ist eine eigene Berechtigung. Bearbeiter und Administratoren dürfen
beide Webhooks verwenden. Alle anderen brauchen eine Webhook-Stufe, die ihnen, ihrer Gruppe oder
ihrer Rolle gewährt wurde: **keine**, **Alertmanager**, **Universal**, **beide Webhooks** oder
**beide, und Token verwalten**; letztere Stufe verwaltet zusätzlich die Token aller.

Ein Webhook-Token sendet nur dorthin, wohin sein Ersteller senden darf, und das wird bei jeder
Anfrage erneut geprüft. Wird der Ersteller deaktiviert oder verliert er seine Stufe, sind seine
Token sofort widerrufen. Siehe
[Einen Webhook-Absender authentifizieren](../../guides/webhook-tokens/).

Personen in einer Nachricht zu nennen ist ebenfalls eine Berechtigung. Jeder darf sich selbst
nennen; andere zu nennen erfordert eine Freigabe, die Administratoren haben. Ein Token nennt
Personen nur, soweit seine eigene Nachrichtenstufe und die Berechtigung seines Erstellers es beide
erlauben. Siehe
[Jemanden Personen anschreiben lassen](../../guides/roles/#let-someone-message-people).

## Jede Änderung wird aufgezeichnet {#every-change-is-recorded}

Änderungen an Berechtigungen, Gruppenmitgliedschaften und deaktivierten Benutzern werden in das
Änderungsprotokoll geschrieben. Eine Änderung einer Gruppenmitgliedschaft wird in derselben
Transaktion aufgezeichnet: Lässt sich der Eintrag nicht schreiben, findet die Änderung nicht statt.
Siehe [Ein Änderungsprotokoll führen](../../guides/audit-trail/).
