# Architecture Decision Records

Architectural decisions are recorded here, one file per decision, in
[ADR](https://adr.github.io/) format.

## Rules

* Copy [0000-template.md](0000-template.md) to `NNNN-short-title.md`, numbered sequentially.
* An accepted ADR is immutable. To change a decision, write a new ADR and set the old one to
  `Superseded by …`; only the status line of the old record may be edited.
* Any change to how components are structured, how they communicate, or which external
  dependency is used needs an ADR before the PR is merged.

## Index

| ADR | Title | Status |
| --- | --- | --- |
| [0001](0001-kong-xdg-configuration.md) | Configuration through kong with XDG file locations | Accepted |
