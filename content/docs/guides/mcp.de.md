---
title: Einen KI-Assistenten über MCP anbinden
weight: 23
---

Lassen Sie einen KI-Assistenten wie Claude Code Ihre Vorlagen, Ziele und Routen über den
MCP-Endpunkt von Teamster lesen und optional ändern, und zwar als die Person, die ihm ein Token
gegeben hat.

MCP ist aus, bis ein Betreiber es einschaltet. Bis dahin funktioniert nichts von dem, was folgt.

## MCP aktivieren {#enable-mcp}

```yaml
mcp:
  enabled: true     # serve /mcp and let users mint MCP tokens at /admin/tokens
  read-only: false  # refuse every tool that writes, whoever the user is
```

Oder `TEAMSTER_MCP_ENABLED=true` und `TEAMSTER_MCP_READ_ONLY=true`. Beide sind standardmäßig
`false`. Siehe [Konfiguration](../../reference/configuration/#mcp).

`read-only` ist ein Schalter für die gesamte Installation. Ist er an, bietet der Endpunkt kein
Schreib-Tool an und weist einen Schreibzugriff ab, selbst wenn ein Client ein solches Tool aufruft,
gleich was das Token oder sein Benutzer erlaubt.

## Ein Token erstellen {#mint-a-token}

Jeder angemeldete Benutzer, auch ein Betrachter, kann ein MCP-Token erstellen. Es ist von den
[Zugriffstoken für Webhooks](../webhook-tokens/) auf derselben Seite getrennt.

{{% steps %}}

### Das Token erstellen {#create-it}

Öffnen Sie **Zugriff → Zugriffstoken** (**/admin/tokens**) und suchen Sie **MCP-Zugriffstoken**.
Benennen Sie das Token nach dem Client, zum Beispiel `claude-code-laptop`; Namen sind pro Benutzer
eindeutig. Wählen Sie den **Zugriff**: **nur lesen** oder **lesen und schreiben**. Wählen Sie
**Token erstellen**.

### Das Token kopieren {#copy-it}

Das Token beginnt mit `tmcp_` und wird nur einmal angezeigt. Teamster speichert nur einen Digest und
kann es daher nicht erneut anzeigen. Legen Sie es im Secret-Speicher des Clients ab.

{{% /steps %}}

Sie sehen und widerrufen die Token, die Sie erstellt haben, mit **Widerrufen**.
Webhook-Administratoren und Administratoren sehen und widerrufen die aller Personen. Ein widerrufenes
Token wird ab seiner nächsten Anfrage abgewiesen.

Erstellen und Widerrufen werden im [Änderungsprotokoll](../audit-trail/) festgehalten.

## Was ein Token darf {#what-a-token-can-do}

Ein Token handelt als die Person, die es erstellt hat, mit deren aktuellen Berechtigungen und nie
mit mehr.

* Es funktioniert nicht mehr, wenn sein Ersteller deaktiviert wird.
* **nur lesen** kann nicht schreiben, gleich was der Ersteller darf. **lesen und schreiben** kann
  ändern, was der Ersteller ändern darf, und nur solange `mcp.read-only` aus ist.
* Es öffnet nur `/mcp`. Die Webhooks und `/api` öffnet es nicht.
* Jeder Aufruf läuft als der Ersteller über die Admin-API, daher gelten Rollen, Freigaben und
  Eigentum wie in der Verwaltungsoberfläche. Siehe [Rollen und Zugriff verwalten](../roles/).

Die Tools:

| Zugriff | Tools |
| --- | --- |
| Lesen | `list_templates`, `get_template`, `list_destinations`, `get_destination`, `list_routes`, `get_route`, `preview_template`, `match_route`, `list_teams`, `list_channels`, `list_sampled_labels` |
| Schreiben | `create_template`, `update_template`, `delete_template`, `create_destination`, `update_destination`, `delete_destination`, `create_route`, `update_route`, `delete_route` |

Schreib-Tools stehen nur einem Token mit **lesen und schreiben** zur Verfügung, und nur solange
`mcp.read-only` aus ist. `list_sampled_labels` setzt eingeschaltete
[Ereignis-Stichproben](../../reference/configuration/#event-samples) voraus und einen Benutzer, der
Vorlagen oder Routen bearbeiten darf.

## Einen Assistenten anbinden {#connect-an-assistant}

Richten Sie den Client auf `/mcp` Ihrer Teamster-URL, mit dem Token als Bearer-Zugangsdaten.

{{< tabs >}}

{{< tab name="Claude Code" >}}

```bash
claude mcp add --transport http teamster https://teamster.example.com/mcp \
  --header "Authorization: Bearer <token>"
```

{{< /tab >}}

{{< tab name="Beliebiger MCP-Client" >}}

Verwenden Sie den Streamable-HTTP-Transport mit der URL `https://teamster.example.com/mcp` und dem
Header `Authorization: Bearer <token>`. Der Endpunkt ist zustandslos: Er hält zwischen den Anfragen
keine Sitzung.

{{< /tab >}}

{{< /tabs >}}

## Das Claude-Code-Plugin installieren {#install-the-claude-code-plugin}

Das Teamster-Repository ist ein Plugin-Marketplace für Claude Code. Das Plugin `teamster` verbindet
Claude Code mit dem MCP-Endpunkt und fügt den Skill `teamster-routing` hinzu.

```text
/plugin marketplace add pflege-de-labs/teamster
/plugin install teamster@teamster
```

Claude Code fragt zwei Werte ab:

* **Teamster URL**: die Basis-URL ohne `/mcp`, zum Beispiel `https://teamster.example.com`.
* **MCP token**: das `tmcp_`-Token, das Sie erstellt haben. Das Plugin legt es im sicheren
  Anmeldedatenspeicher von Claude Code ab.

Der Skill führt den Assistenten durch das Routen eines Alarms: die erfassten Labels lesen, mit
`match_route` einen Probelauf machen, ein Ziel wählen, die Vorlage in der Vorschau prüfen, die Route
schreiben und mit `match_route` erneut bestätigen. Bevor er etwas löscht, fragt er Sie.

## Sicher bleiben {#keep-it-safe}

Label-Werte, Annotationen, Vorlagentext und die Namen, die andere gewählt haben, erreichen den
Assistenten, und wer ein Ereignis senden oder einen Datensatz bearbeiten kann, kann dort Text
platzieren. Behandeln Sie ihn als nicht vertrauenswürdige Eingabe.

* **Bevorzugen Sie ein Token mit nur lesen.** Erstellen Sie ein Token mit **lesen und schreiben**
  nur für einen Assistenten, der etwas ändern muss.
* **Setzen Sie `mcp.read-only`** in einer Installation, in der kein Assistent je schreiben soll.
* **Geben Sie jedem Client ein eigenes Token**, damit Sie eines widerrufen können, ohne die anderen
  zu stören.
* **Prüfen Sie es.** Änderungen über MCP erscheinen im Änderungsprotokoll mit `via` = `mcp`, unter
  dem Namen des Token-Erstellers.

Den Entwurf beschreiben
[ADR 0106](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0106-mcp-tokens-answer-to-their-creator.md)
und
[ADR 0107](https://github.com/pflege-de-labs/teamster/blob/main/docs/adr/0107-mcp-endpoint-over-the-admin-api.md).
