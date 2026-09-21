# 0035. A message posted without a status is delivered once, not tracked

* Status: Accepted
* Date: 2026-09-21

## Context

`/webhook/universal` was not, in practice, universal. `processAlert` rejected any `status` outside
`{"firing", "resolved"}` outright, so every sender had to speak Alertmanager's alert lifecycle
whether or not it had one — a deployment publishing "build finished" or "deploy rolled out"
notifications had no honest payload to send, even though nothing about routing or templates
required one. Labels already drive `routing.Plan`, annotations already reach templates, and the
claim protocol behind the tracked lifecycle already keys purely on `(fingerprint, team_id,
channel_id)` / `(fingerprint, recipient_id)` — `status` rides along as a stored column, never a
query predicate. The only place a status value was mandatory was one validation gate at the HTTP
boundary.

[ADR 0030](0030-teams-v2-compatible-webhooks.md) already established, for `/teamsv2/...`, that
"post now, track nothing" is a legitimate first-class shape in this codebase for a sender with no
lifecycle to report. That endpoint goes further than this decision needs: it also skips routing
and templates, because its senders have already decided the message and the destination. A general
message hook should keep routing and templates — that is what makes it "universal" rather than a
second `/teamsv2` — while adopting the same "nothing to track" posture for the case where there is
genuinely nothing to track.

## Decision

We will treat `Status` as the message's lifecycle signal, not as a precondition for accepting it.
`firing` and `resolved` keep their existing, unchanged meaning: they opt into the claim protocol,
so a repeat post with the same fingerprint edits the card in place and a resolve clears it. Any
other value — including an absent one, which is the ordinary shape for a sender with no lifecycle
— is delivered once: rendered, routed to its destination, posted or sent, and tracked nowhere.
Nothing is written to `active_alerts`.

`/webhook/alertmanager` is unaffected by construction: it only ever produces `firing`/`resolved`
values from Alertmanager's own payload, so it never reaches the new branch.

**The alternative — claim-and-update by fingerprint for a message with no status — was rejected.**
It would have reused the existing machinery with zero new delivery code, but it has a real
footgun: two unrelated one-off messages that happen to share the same labels, generator and
(absent) start time compute the same fingerprint, and the second would silently *edit* the first's
card instead of posting its own. An alert's identity is meant to survive across its own repeats;
a general message's is not, and there is no field in the baseline payload that says otherwise. A
sender that *wants* update-in-place semantics can still ask for them explicitly, by setting
`status: "firing"` — that capability is not lost, it becomes opt-in rather than the default for
every sender regardless of whether it applies.

**No change to routing or the payload's field names.** Route selectors remain label-only;
annotations continue to flow to templates exactly as before — this decision adds no new matching
capability, only relaxes what `status` is allowed to be. `models.Alert`, `UniversalWebhookPayload`,
`processAlert` and the rest keep their existing names: they now represent "one webhook-delivered
message, alert-shaped or not," which is a documentation change, not a Go identifier one. The
`.Alert.*` field name every stored template already references is unaffected either way.

## Consequences

`/webhook/universal` becomes what its name always implied: a sender posts labels and annotations
and gets routed and rendered like anything else, and Alertmanager's firing/resolved vocabulary is
one specialization of that rather than the whole contract. No schema change — `active_alerts` and
its claim protocol were already status-agnostic, so this is a validation relaxation plus one new
delivery path (`deliverOnce`, `deliverToChannelOnce`, `deliverToRecipientOnce`) that reuses the
existing render/target/message-building helpers and simply skips the claim/complete/release calls.

The template preview gains a third sample (`previewSamples()` → `"message"`) so a template author
can see how their template renders for the untracked case, alongside the existing firing/resolved
samples.

Two things this does not do, on purpose. It does not give a one-shot message any way to be updated
later — nothing in the baseline payload identifies a later post as the same event, matching
`/teamsv2`'s own unresolved question in ADR 0030. And it does not change what happens when no route
matches: a message with no status and no matching route is still rejected with `502`, exactly as a
firing alert is today.
