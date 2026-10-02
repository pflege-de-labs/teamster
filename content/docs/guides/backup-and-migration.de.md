---
title: Die Konfiguration sichern und übertragen
weight: 5
---

Die Konfiguration von Teamster zieht als ein JSON-Bundle um. Damit sichern Sie eine Installation,
halten die Konfiguration in einem Repository oder bringen sie zu einer anderen Installation oder
Datenbank.

## Wissen, was das Bundle enthält {#know-what-the-bundle-carries}

| Enthalten | Nicht enthalten |
| --- | --- |
| Vorlagen, einschließlich der Standardvorlagen | Zugangsdaten, auch Zugriffstoken für Webhooks |
| Ziele, mit Team- und Kanalnamen neben den IDs | Gruppen und Berechtigungen an Datensätzen |
| Routen | verknüpfte Personen (Empfänger) |
| Zustellberechtigungen | Sitzungen, Anmeldevorgänge und offene Alarme |

Weil das Bundle keine Zugangsdaten enthält, können Sie es neben der übrigen Konfiguration einer
Installation einchecken. Dort, wo das Bundle ankommt:

* Stellen Sie neue Zugriffstoken für Webhooks aus; siehe
  [Webhook-Absender authentifizieren](../webhook-tokens/).
* Datensätze, die das Bundle anlegt, gehören den Administratoren, bis jemand sie teilt; siehe
  [Rollen und Freigaben verwalten](../roles/).
* Personen verknüpfen ihre Chats erneut; siehe
  [Den Teams-Bot einrichten](../teams-bot/#link-a-persons-chat).

{{< callout type="warning" >}}
Ein Import lehnt ein Bundle mit einer Route ab, die an eine Person zustellt, statt eine Route
anzulegen, die nie zustellt. Entfernen Sie die Person vor dem Export aus solchen Routen.
{{< /callout >}}

## Die Konfiguration exportieren {#export-the-configuration}

Auf der Kommandozeile:

```bash
teamster export -o teamster.json
```

`teamster export` liest die Datenbank direkt und funktioniert daher, ob der Server läuft oder
nicht. Ohne `-o` schreibt es auf stdout. Es migriert das Schema nie; eine Datenbank mit
ausstehenden Migrationen wird abgelehnt. Der Export auf der Kommandozeile enthält nur Team- und
Kanal-IDs, ohne ihre Namen.

Über HTTP, als Administrator:

```bash
curl -u admin:<password> http://localhost:8080/api/config/export > teamster.json
```

Der HTTP-Export ergänzt zusätzlich die Team- und Kanalnamen, die er über Graph auflösen kann.

## Ein Bundle importieren {#import-a-bundle}

Erst die Vorschau, dann anwenden:

```bash
teamster import teamster.json --dry-run
teamster import teamster.json
```

`-` als Dateiname liest das Bundle von stdin. Wählen Sie mit `--mode` einen Modus:

| Modus | Was passiert |
| --- | --- |
| `merge` (Standard) | Anlegen oder aktualisieren, was das Bundle enthält; alles andere bleibt unberührt. |
| `replace` | Die Installation an das Bundle angleichen und löschen, was es nicht nennt. |

```bash
teamster import teamster.json --mode replace --dry-run
```

Beide Modi prüfen zuerst das ganze Bundle und wenden es in einer Transaktion an; ein Bundle, das
sich nicht anwenden lässt, ändert also nichts. `--dry-run` führt den echten Import aus und rollt
ihn zurück, daher ist die Vorschau exakt.

Über HTTP, als Administrator:

```bash
curl -u admin:<password> -X POST --data-binary @teamster.json \
  'http://localhost:8080/api/config/import?mode=replace&dry-run=true'
```

## In einen anderen Tenant umziehen {#move-to-another-tenant}

IDs bleiben erhalten; ein Bundle erneut dort zu importieren, wo es herkommt, ändert also nichts.
Team- und Kanal-IDs gehören allerdings zu einem Tenant: Ein Bundle, das in einen anderen Tenant
gebracht wird, lässt sich sauber importieren und stellt dann nirgendwohin zu.

Der HTTP-Import nennt die Ziele, die dieser Tenant nicht auflösen kann. Korrigieren Sie nach dem
Import die Team- und Kanal-IDs dieser Ziele
([ADR 0013](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0013-configuration-transfer.md)).

Um zwischen Datenbanken statt zwischen Tenants umzuziehen, siehe
[Von SQLite zu Postgres wechseln](../storage/#move-from-sqlite-to-postgres).
