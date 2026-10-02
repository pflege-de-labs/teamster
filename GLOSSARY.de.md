# German glossary

Terms the German documentation uses. UI labels come from the application's
`internal/i18n/locales/de.json` at the documented version and win over this list; quote them
exactly, in bold, as the English pages do.

| English | Deutsch | Note |
| --- | --- | --- |
| access token (OAuth) | Access-Token | "Zugriffstoken" is the webhook token |
| access token, webhook token | Zugriffstoken | "Zugriffstoken für Webhooks" when ambiguous |
| admin UI | Verwaltungsoberfläche | the app calls itself "Teamster-Verwaltung" |
| alert | Alarm | plural "Alarme"; Alertmanager's own term stays English in its context |
| app registration | App-Registrierung | "Entra-App-Registrierung" on first use |
| attestation | Attestierung | |
| audit trail | Änderungsprotokoll | |
| backfill | Nachholen | |
| broadcast | Rundsendung | as in the UI; "an alle senden" for the verb |
| bundle (export) | Bundle | |
| card (Adaptive Card) | Karte (Adaptive Card) | product name stays English |
| channel | Kanal | Teams channel: "Teams-Kanal" |
| chat | Chat | |
| claim (post claim) | Anspruch, beanspruchen | OIDC claim stays "Claim" |
| client secret | Client-Secret | |
| creator | Ersteller | |
| credentials | Zugangsdaten | |
| default route | Standardroute | |
| deliver | zustellen | "Zustellung" for delivery |
| destination | Ziel | |
| digest | Digest | |
| endpoint | Endpunkt | |
| event samples | Ereignis-Stichproben | |
| event | Ereignis | |
| fallback | Rückfallebene | |
| grant (narrowing a role) | Freigabe | the UI panel is **Zustellberechtigungen** |
| identity provider | Identitätsanbieter | provider group: Anbietergruppe |
| issue / revoke (a token) | ausstellen / widerrufen | |
| label selector | Label-Selektor | |
| link code; link / unlink a chat | Verknüpfungscode; verknüpfen / Verknüpfung aufheben | |
| nested route; root route; parent / child route | verschachtelte Route; Wurzelroute; übergeordnete / untergeordnete Route | |
| owner; own; share; record | Eigentümer; besitzen; teilen; Datensatz | |
| payload | Nutzdaten | plural; the UI also says "Nutzlast" — the docs do not |
| permission | Berechtigung | |
| picker | Auswahlliste | |
| policy (Cedar) | Richtlinie | |
| preset | vorgefertigte Vorlage | preset names are English in the UI too; quote them as they are |
| priority | Priorität | |
| recipient | Empfänger | |
| reconcile | Abgleich | |
| refuse (a request) | abweisen | |
| release | Release | "Version" where the reader picks one |
| replica | Replikat | |
| resolve (an alert) | auflösen; "aufgelöst" | |
| role: administrator / editor / viewer | Administrator / Bearbeiter / Betrachter | |
| route | Route | |
| routing graph | Routing-Bild | as in the UI |
| sanitize | bereinigen | |
| scope (token) | Bereich | OAuth/OIDC scope stays "Scope" |
| sender | Absender | |
| session | Sitzung | |
| sign in / sign out | anmelden / abmelden | |
| target (of a route) | Zustellziel | keeps it apart from destination, "Ziel" |
| template | Vorlage | |
| tenant | Mandant | "Mandanten-ID (Tenant ID)" in Entra contexts |
| webhook level | Webhook-Stufe | |
| webhook | Webhook | |

Section headings: Getting started — Erste Schritte, Guides — Anleitungen, Reference — Referenz,
Concepts — Konzepte, Quick start — Schnellstart, Release notes — Versionshinweise, Related —
Verwandte Themen, Next steps — Nächste Schritte, Before you start — Bevor Sie beginnen, See also —
Siehe auch.

Stay in English: configuration keys, flags, environment variables, metric names, code, file
paths, HTTP headers, product names (Microsoft Teams, Entra ID, Microsoft Graph, Alertmanager,
Keycloak, Helm, Kubernetes, NATS JetStream), Kubernetes kinds (Secret, Pod, Deployment,
StatefulSet), Keycloak's UI labels, and Hextra/Hugo syntax.
