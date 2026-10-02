---
title: CLI
weight: 2
---

Alle Befehle und Flags des Binaries `teamster`. `teamster <command> --help` gibt dasselbe aus, mit
den Werten, die sich aus der aktuellen Konfiguration ergeben.

## Aufruf {#synopsis}

```text
teamster [<command>] [flags]
```

| Befehl | Argumente | Wirkung |
| --- | --- | --- |
| [`serve`](#serve) | — | Startet den HTTP-Server der Webhook-Brücke. Standard, wenn kein Befehl angegeben ist. |
| [`export`](#export) | — | Schreibt die Konfiguration in ein Bundle. |
| [`import`](#import) | `<file>` | Wendet ein Konfigurations-Bundle an. |
| [`migrate up`](#migrate-up) | — | Wendet alle ausstehenden Migrationen an. |
| [`migrate down`](#migrate-down) | — | Nimmt die jüngste Migration zurück. |
| [`migrate status`](#migrate-status) | — | Zeigt, welche Migrationen angewendet sind. |
| [`completion`](#completion) | `<shell>` | Schreibt ein Skript zur Shell-Vervollständigung nach stdout. |

## Globale Flags {#global-flags}

Jeder Befehl akzeptiert diese Flags und alle Konfigurations-Flags.

| Flag | Umgebungsvariable | Beschreibung |
| --- | --- | --- |
| `-h`, `--help` | — | Zeigt kontextabhängige Hilfe an und beendet sich. |
| `-c`, `--config=FILE` | `TEAMSTER_CONFIG` | Lädt die Konfiguration aus `FILE` zusätzlich zu den XDG-Orten. |
| `--version` | `TEAMSTER_VERSION` | Gibt die Version aus und beendet sich. |
| `--<section>-<key>=VALUE` | `TEAMSTER_<SECTION>_<KEY>` | Ein Flag je Konfigurationsschlüssel, etwa `--server-addr` oder `--database-postgres-host`. Siehe [Konfiguration](../configuration/). |

Jeder Befehl öffnet die Datenbank, die die Konfiguration nennt. Nur `serve` prüft den Rest der
Konfiguration; die anderen Befehle laufen daher ohne Zugangsdaten für Graph, Bot oder Verwaltung.

## Exit-Status {#exit-status}

| Status | Bedeutung |
| --- | --- |
| `0` | Der Befehl war erfolgreich, oder `--help` bzw. `--version` wurde angegeben. |
| `1` | Der Befehl ist fehlgeschlagen, oder die Argumente ließen sich nicht auswerten. Der Fehler geht als `teamster: <error>` nach stderr. |

`SIGINT` und `SIGTERM` brechen den laufenden Befehl ab. Ein zweites Signal beendet den Prozess
sofort.

## serve {#serve}

```text
teamster serve [flags]
```

Betreibt den HTTP-Server, bis er `SIGINT` oder `SIGTERM` erhält, und arbeitet dann laufende Anfragen
bis zu `server.shutdown-timeout` lang ab.

| Schritt | Verhalten |
| --- | --- |
| Start | Prüft die Konfiguration und startet bei einem Fehler nicht; siehe [Prüfungen beim Start](../configuration/#startup-checks). |
| Datenbank | Öffnet sie und behandelt ausstehende Migrationen so, wie `database.migrate` es vorgibt: `auto`, `verify` oder `off`. |
| Listener | `server.addr`; zusätzlich `metrics.addr`, wenn `metrics.enabled` und `metrics.prometheus` eingeschaltet sind. |

Keine eigenen Flags.

## export {#export}

```text
teamster export [-o FILE] [flags]
```

Schreibt die Konfiguration – Vorlagen, Ziele, Routen, Berechtigungen und Standardvorlagen – als
JSON-Bundle. Der Befehl liest die Datenbank direkt und funktioniert daher auch bei einer
angehaltenen Installation.

| Flag | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `-o`, `--output=FILE` | `TEAMSTER_OUTPUT` | stdout | Schreibt nach `FILE` statt nach stdout. |

| Verhalten | Detail |
| --- | --- |
| Migrationen | Werden nie angewendet. Der Export verweigert eine Datenbank mit ausstehenden Migrationen, unabhängig von `database.migrate`. |
| Team- und Kanalnamen | Nicht enthalten; das Bundle enthält die IDs. Der Export der Verwaltungsoberfläche ergänzt die Namen. |
| Nicht exportiert | Alles andere, etwa Zugriffstoken für Webhooks, Teams-V2-Endpunkte, Benutzer, Gruppen, aktive Ereignisse und das Änderungsprotokoll. |

## import {#import}

```text
teamster import <file> [--mode=merge|replace] [--dry-run] [flags]
```

Wendet ein von `export` geschriebenes Bundle auf die konfigurierte Datenbank an.

| Argument | Beschreibung |
| --- | --- |
| `<file>` | Zu importierendes Bundle; `-` liest von stdin. |

| Flag | Umgebungsvariable | Standard | Beschreibung |
| --- | --- | --- | --- |
| `--mode=STRING` | `TEAMSTER_MODE` | `merge` | `merge` legt an oder aktualisiert, was das Bundle enthält; `replace` löscht außerdem, was es nicht nennt. |
| `--dry-run` | `TEAMSTER_DRY_RUN` | `false` | Meldet, was sich ändern würde, ohne etwas zu ändern. |

| Verhalten | Detail |
| --- | --- |
| Migrationen | Werden so behandelt, wie `database.migrate` es vorgibt. |
| Audit | Jede Änderung wird in den konfigurierten Audit-Sinks festgehalten, mit dem Benutzer des Betriebssystems als Akteur. Ereignisse für NATS warten im Änderungsprotokoll der Datenbank, bis ein laufender Server sie veröffentlicht. |
| Ausgabe | Eine Zeile je Änderung: Aktion (`create`, `update`, `delete`), Art, Name und ID. Danach `applied <n> changes in <mode> mode`, mit `--dry-run` `would apply …`. `nothing to change`, wenn das Bundle dem Bestand entspricht. |

## migrate {#migrate}

Öffnet die Datenbank ohne Migration, unabhängig von `database.migrate`, und ändert das Schema nur
so, wie der Unterbefehl es verlangt.

### migrate up {#migrate-up}

```text
teamster migrate up [flags]
```

Wendet alle ausstehenden Migrationen an. Gibt für jede `applied <version> <source> in <duration>`
aus, oder `already up to date`.

### migrate down {#migrate-down}

```text
teamster migrate down [flags]
```

Nimmt die jüngste Migration zurück, eine je Aufruf. Gibt
`rolled back <version> <source> in <duration>` aus.

### migrate status {#migrate-status}

```text
teamster migrate status [flags]
```

Gibt eine Zeile je Migration aus.

| Spalte | Enthält |
| --- | --- |
| `VERSION` | Nummer der Migration. |
| `STATE` | Ob sie angewendet ist oder aussteht. |
| `APPLIED` | Wann sie angewendet wurde, `YYYY-MM-DD HH:MM:SS`, oder `-`. |
| `SOURCE` | Die Migrationsdatei oder `go migration, registered in code`. |

## completion {#completion}

```text
teamster completion <shell> [flags]
```

Schreibt ein Vervollständigungsskript für den gesamten Befehlsbaum nach stdout.

| Argument | Werte |
| --- | --- |
| `<shell>` | `bash`, `zsh`, `fish` |

```bash
teamster completion bash > /etc/bash_completion.d/teamster
teamster completion zsh  > "${fpath[1]}/_teamster"
teamster completion fish > ~/.config/fish/completions/teamster.fish
```

In bash vervollständigt ein Enum-Flag zum Wert seiner Umgebungsvariablen statt zu den Werten, die
es akzeptiert. zsh und fish bieten die akzeptierten Werte an.

## Siehe auch {#see-also}

* [Konfiguration](../configuration/)
* [Die Konfiguration sichern und übertragen](../../guides/backup-and-migration/)
* [Speicher wählen und betreiben](../../guides/storage/)
