---
title: Speicher wählen und betreiben
weight: 4
---

Teamster hält seine Konfiguration und den Zustand offener Alarme in einer Datenbank. Diese
Anleitung hilft Ihnen, zwischen SQLite und Postgres zu wählen, die Datenbank zu konfigurieren,
Schemamigrationen auszuführen und von einer zur anderen zu wechseln.

## Ein Backend wählen {#pick-a-backend}

| `database.driver` | Was es ist |
| --- | --- |
| `sqlite` (Standard) | Eine Datei, keine weitere Laufzeitabhängigkeit und genau eine Instanz: SQLite erlaubt nur einen Schreiber. |
| `postgres` | Mehrere Instanzen, die sich eine Datenbank teilen. |

Mehrere Instanzen heißt: ein paar Replikate hinter einem Service, von denen jedes jede Anfrage
annehmen kann. Es gibt keine Queue, keine Leader-Wahl und kein Sharding.

Die Einstellungen des Treibers, den Sie nicht gewählt haben, werden ignoriert. Beide Blöcke können
also in der Konfigurationsdatei bleiben, während Sie wechseln.

## SQLite verwenden {#use-sqlite}

SQLite braucht nur einen Pfad:

```yaml
database:
  driver: sqlite
  path: "/data/teamster.db"
```

`database.path` (`TEAMSTER_DATABASE_PATH`) ist standardmäßig `teamster.db` im Arbeitsverzeichnis.
Das Container-Image setzt `TEAMSTER_DATABASE_PATH=/data/teamster.db`; hängen Sie ein Volume unter
`/data` ein.

Mit dem Helm-Chart verwaltet das Chart `database.path`, betreibt ein StatefulSet mit einem Volume
Claim und lehnt einen `replicaCount` über 1 ab. `persistence.enabled=false` läuft auf einem
`emptyDir`, das beim Neustart des Pods den gesamten Zustand verliert.

## Postgres verwenden {#use-postgres}

Konfigurieren Sie Postgres mit einzelnen Einstellungen und übergeben Sie das Passwort über die
Umgebung:

```yaml
database:
  driver: postgres
  postgres:
    host: pg.internal
    port: 5432
    dbname: teamster
    user: teamster
    sslmode: require
```

```bash
export TEAMSTER_DATABASE_POSTGRES_PASSWORD='<password>'
```

{{< callout type="warning" >}}
Schreiben Sie das Passwort nicht in die Konfigurationsdatei, auch nicht als `password: ""`. Ein
Wert aus der Konfigurationsdatei gewinnt gegen die Umgebungsvariable.
{{< /callout >}}

`sslmode` ist standardmäßig `require`. `verify-ca` und `verify-full` prüfen den Server gegen die
CA-Datei in `database.postgres.sslrootcert`. Ein gemanagtes Postgres braucht das meist, weil das
Container-Image nur die öffentlichen Root-Zertifikate enthält, nicht die eigenen des Anbieters.

Für das, was die einzelnen Felder nicht ausdrücken können, etwa einen Connection Pooler oder
`target_session_attrs`, setzen Sie stattdessen eine vollständige Verbindungs-URL. Sie enthält das
Passwort; übergeben Sie sie daher als `TEAMSTER_DATABASE_POSTGRES_URL`. Teamster lehnt eine URL in
Kombination mit `host`, `password` oder `sslrootcert` ab.

### Den Connection Pool dimensionieren {#size-the-connection-pool}

| Schlüssel | Standard |
| --- | --- |
| `database.max-open-conns` | `0`: 10 für Postgres, unbegrenzt für SQLite |
| `database.max-idle-conns` | `0`: 5 für Postgres |
| `database.conn-max-lifetime` | `0s`: keine Grenze |
| `database.connect-timeout` | `10s` bis zur ersten Verbindung beim Start |

Bevor Sie `max-open-conns` erhöhen, teilen Sie `max_connections` des Servers durch die Zahl der
Instanzen.

### Postgres mit dem Helm-Chart betreiben {#run-postgres-with-the-helm-chart}

Das Chart installiert kein Postgres. Richten Sie es auf eines und lesen Sie das Passwort aus dem
Secret, das Ihr Postgres-Operator angelegt hat:

```yaml
database:
  driver: postgres
  postgres:
    host: teamster-pg-rw
    passwordFrom:
      secretName: teamster-pg-app
      key: password
replicaCount: 3
```

Mit `postgres` betreibt das Chart ein Deployment und rollt die Replikate mit einem Surge-Pod aus.
Siehe die [Referenz der Helm-Values](../../reference/helm-values/).

## Schemamigrationen ausführen {#run-schema-migrations}

Das Schema ist versioniert. Standardmäßig wendet der Serverstart an, was fehlt; ein Upgrade braucht
also keinen zusätzlichen Schritt. Unter Postgres verhindert ein Advisory Lock, dass zwei gleichzeitig
startende Instanzen beide Migrationen anwenden.

Um Migrationen von Hand auszuführen:

```bash
teamster migrate status      # was angewendet ist und was nicht
teamster migrate up          # alles Ausstehende anwenden
teamster migrate down        # die jüngste Migration zurücknehmen
```

Diese Befehle lesen dieselbe Konfiguration wie der Server. `migrate down` nimmt pro Aufruf eine
Migration zurück und kann Daten, die eine Migration entfernt hat, nicht zurückholen.

`database.migrate` (`TEAMSTER_DATABASE_MIGRATE`) legt fest, was der Server mit einer Datenbank
macht, deren Schema zurückliegt:

| Wert | Was passiert |
| --- | --- |
| `auto` (Standard) | Die fehlenden Migrationen anwenden. |
| `verify` | Den Start verweigern und dabei `teamster migrate up` nennen. |
| `off` | Die Datenbank so öffnen, wie sie ist. |

Setzen Sie `verify`, wenn eine Schemaänderung ein Schritt sein soll, den Sie beobachten: bei einer
Installation, die `teamster migrate up` vor dem Ausrollen der neuen Version ausführt, oder bei
einer, deren Datenbankbenutzer das Schema nicht ändern darf. Mit dem Helm-Chart setzen Sie
`config.settings.database.migrate=verify` und führen den Befehl aus einem Job in `extraObjects`
aus; das Chart hat keinen Migrations-Hook.

`teamster export` migriert nie, gleich wie die Einstellung lautet. Stattdessen lehnt es eine
Datenbank mit ausstehenden Migrationen ab.

## Von SQLite zu Postgres wechseln {#move-from-sqlite-to-postgres}

{{< callout type="warning" >}}
Nur die Konfiguration zieht um: Vorlagen, Ziele, Routen und Zustellberechtigungen. Sitzungen,
Anmeldevorgänge und offene Alarme bleiben zurück. Alle melden sich erneut an, und ein Alarm, der
während der Umstellung offen ist, bekommt eine zweite Karte in Teams; sein Schließen bearbeitet nie
die erste. Schließen Sie vorher, was Sie können.
{{< /callout >}}

{{% steps %}}

### Die Konfiguration exportieren {#export-the-configuration}

Solange die alte Konfiguration noch auf SQLite zeigt:

```bash
teamster export -o teamster.json
```

### Die Konfiguration auf Postgres richten {#point-the-configuration-at-postgres}

Setzen Sie `database.driver: postgres` und die Einstellungen unter `database.postgres`, wie unter
[Postgres verwenden](#use-postgres) beschrieben.

### Das Schema anlegen {#create-the-schema}

```bash
teamster migrate up
```

### Das Bundle importieren {#import-the-bundle}

```bash
teamster import teamster.json
```

### Teamster starten {#start-teamster}

Starten Sie den Server gegen Postgres und skalieren Sie ihn hoch, wenn Sie mehr Replikate möchten.

{{% /steps %}}

Das Bundle und seine Grenzen beschreibt
[Die Konfiguration sichern und übertragen](../backup-and-migration/).
