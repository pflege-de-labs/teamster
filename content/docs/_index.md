---
title: Documentation
---

Teamster receives alerts from Prometheus Alertmanager and from any system that can send a JSON
webhook, picks a Microsoft Teams channel or chat for each one by its labels, and posts an Adaptive
Card there. It keeps track of every open alert, so the card is updated when the alert changes and
when it clears.

{{< cards >}}
  {{< card
    link="getting-started/"
    title="Getting started"
    icon="play"
    subtitle="Install Teamster and send your first alert to a channel."
  >}}
  {{< card
    link="guides/"
    title="Guides"
    icon="book-open"
    subtitle="Configure Entra and Teams, connect senders, write templates, run in production."
  >}}
  {{< card
    link="reference/"
    title="Reference"
    icon="document-text"
    subtitle="Configuration keys, CLI, webhook payloads, template data, metrics, Helm values."
  >}}
  {{< card
    link="concepts/"
    title="Concepts"
    icon="light-bulb"
    subtitle="How routing, the alert lifecycle and ownership work, and why."
  >}}
{{< /cards >}}
