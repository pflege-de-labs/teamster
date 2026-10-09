# 0091. Templates get sprig's hermetic functions

* Status: Accepted
* Date: 2026-10-09

## Context

Templates had `toJSON`, `default` and text/template's builtins. A sender that sends raw data, such
as Argo CD posting a commit message, needs the template to take it apart: split lines, match a
Conventional Commit header, strip a prefix. Without string and regex functions every sender has to
format its own text, and templating moves out of teamster.

[sprig](https://masterminds.github.io/sprig/) is the function library Helm, Argo CD and most Go
template users already know.

## Decision

`funcs()` starts from `sprig.HermeticTxtFuncMap()`, removes sprig's crypto functions and puts
teamster's own `toJSON` and `default` on top.

* **Hermetic only.** The full map has `env` and `expandenv`, which would let anyone who may edit a
  template post the Graph or bot client secret into a channel, and `getHostByName`, which reaches
  the network. The hermetic map also leaves out `now`, `date` and the random functions; `.Now`
  already gives a template the time.
* **No crypto.** Key, certificate and password generation is hermetic but costs CPU on every
  preview and has nothing to do in a card.
* **teamster's `default` wins.** It takes `(value, fallback)`, sprig's takes
  `(fallback, value)`. Stored templates and `DefaultTitle` rely on teamster's order, so it stays.
  The pipeline form from sprig's docs, `.x | default "y"`, therefore always yields `"y"`; write
  `default .x "y"`.
* sprig's `toJson` stays alongside `toJSON`; both encode the same way.

The editor's vocabulary is derived from `funcs()`, so it completes every function a template may
call and none it may not. A test pins the left-out functions.

## Consequences

* Templates can derive text from raw payloads, so senders send data and teamster formats it.
* Every sprig function becomes part of what a stored template may depend on. Removing one later
  breaks templates.
* A sprig update that adds an unsafe function to the hermetic map would reach templates; the test
  only names the functions known today.
