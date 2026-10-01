# 0068. Name the bot's commands without a slash

* Status: Accepted
* Date: 2026-10-01

## Context

[ADR 0048](0048-bot-answers-commands-in-the-personal-chat.md) named the bot's commands `/help`,
`/status`, `/test` and `/unlink`, and listed them under those titles in the manifest's
`commandLists`.

In Teams, typing `/` in the compose box opens a menu that Teams shares between its own commands and
every installed app. Nothing namespaces it, so another app's `/status` showed up next to ours or
in its place. Teams has no way for an app to reserve a slash name.

What reaches the bot is not ambiguous. Teams delivers to the bot only the messages in its personal
chat and, in a channel, the messages that @mention it. The clash is in the picker alone.

## Decision

We will name the commands without a slash: `help`, `status`, `test`, `unlink`.

* The manifest titles drop the `/`. Teams shows them in the bot's own command menu, which opens in
  the bot's chat and after `@Teamster`, and which no other app shares.
* The replies refer to the commands by their bare names.
* Parsing is unchanged. A bare word is a command when it is the whole message (ADR 0032). A first
  word starting with `/` is still a command, so habit and old instructions keep working.

Alternatives considered:

* **A prefix, such as `/teamster-status` or `/ts status`.** It is unique only by convention, it is
  long to type, and it still competes in the shared menu.
* **Keep the slash.** It leaves the clash as it is.

## Consequences

* The commands no longer compete with other apps in the `/` picker.
* A person who types `/status` may still get Teams' or another app's menu first. Typing `status`, or
  picking from the bot's menu, avoids it.
* Installed apps keep the old titles until the app package is uploaded with a higher `version`. The
  commands work either way.
* A future command that takes arguments, such as `route key=value`, needs the parser to accept a
  known command word followed by more words. Today only a slash command may carry them.
* The metric route label `bot /test` stays as it is, so existing dashboards still match.
* No schema change.
