---
title: Dokumentation
---

Teamster empfängt Alarme von Prometheus Alertmanager und von jedem System, das einen JSON-Webhook
senden kann. Für jeden Alarm wählt es anhand seiner Labels einen Microsoft-Teams-Kanal oder Chat
und postet dort eine Adaptive Card. Teamster merkt sich jeden offenen Alarm und aktualisiert die
Karte, wenn sich der Alarm ändert und wenn er aufgelöst wird.

{{< cards >}}
  {{< card
    link="getting-started/"
    title="Erste Schritte"
    icon="play"
    subtitle="Teamster installieren und den ersten Alarm an einen Kanal senden."
  >}}
  {{< card
    link="guides/"
    title="Anleitungen"
    icon="book-open"
    subtitle="Entra und Teams einrichten, Absender anbinden, Vorlagen schreiben, produktiv betreiben."
  >}}
  {{< card
    link="reference/"
    title="Referenz"
    icon="document-text"
    subtitle="Konfigurationsschlüssel, CLI, Webhook-Nutzdaten, Vorlagendaten, Metriken, Helm-Werte."
  >}}
  {{< card
    link="concepts/"
    title="Konzepte"
    icon="light-bulb"
    subtitle="Wie Routing, Alarm-Lebenszyklus und Eigentum funktionieren, und warum."
  >}}
{{< /cards >}}
