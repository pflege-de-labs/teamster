# 0025. Shell completion is generated from the command tree

* Status: Accepted
* Date: 2026-09-14

## Context

`teamster` has grown a command tree — `serve`, `export`, `import`, `migrate up|down|status` — and
around sixty flags, several of which take one of a fixed set of values: `--database-driver`,
`--database-migrate`, `--auth-default-role`, `--metrics-otlp-protocol`. None of that is
discoverable from the shell.

Completion scripts are usually written by hand and then rot: a flag is added, the script is not
updated, and the completion quietly describes a release that no longer exists. Anything checked in
as a static file has that problem. kong already holds the whole tree — commands, flags, help text,
`enum` tags — so the scripts can be derived from the same model the parser uses, and be correct by
construction.

kong itself has no completion support.

## Decision

We will add a `teamster completion bash|zsh|fish` command that writes a script to stdout, generated
from kong's model by [github.com/miekg/king](https://github.com/miekg/king).

Not hand-written. Three shells is three dialects, and the interesting parts — positional arguments,
enum values, when to fall back to filenames, how zsh's `_arguments` specs escape a description
containing a colon — are exactly the parts that are easy to get nearly right and hard to notice
being wrong. king exists for this, takes a `*kong.Node`, and is MIT.

The command writes to stdout via `king.Completer.Out()` rather than its `Write()`, because `Write`
also drops a file in the working directory. A command whose contract is "print a script" should
print a script and create nothing.

**We correct one thing king gets wrong.** Its fish generator offers every command with
`__fish_use_subcommand`, which is true only before any subcommand has been typed. For
`teamster migrate up` that is wrong twice over: `up` is offered at the top level, where it means
nothing, and it is not offered after `migrate`, where fish falls back to listing filenames instead.
`nestFishSubcommands` rewrites those conditions to "the parent has been seen and no sibling has",
driven by the model rather than by pattern-matching the generated text, so it cannot rewrite a line
that merely looks similar. It is narrow and it comes out when king grows the same fix.

**We accept one gap.** In bash, a flag with an `enum` completes to the value of its environment
variable rather than to the enum's values. Commands and flags still complete. That is an absence
rather than a wrong answer, so it is documented in the README and left to upstream, unlike the fish
behaviour, which actively misleads.

Alternatives considered:

* **Hand-written scripts in the repository.** Rot by design, and three dialects to maintain.
* **kongplete.** Installs a hook into the user's shell rc and completes by re-invoking the binary
  at completion time, rather than emitting a script an operator can read, review and package. A
  dynamic completer also makes every `<TAB>` a process start, which for a binary that reads XDG
  config files is not free.
* **Waiting for the fish fix upstream before shipping fish.** The correction is twenty lines and
  the alternative is shipping a completion that misleads, or shipping two shells out of three.

## Consequences

Adding a command or a flag makes it completable with no further work, which is the point. Enum
values are offered in zsh and fish, so the set a flag accepts is discoverable rather than only
documented.

The scripts are verified by the shells themselves: every generated script is parsed by its own
shell, and what bash and fish offer is asserted by driving their completion for real —
`teamster <TAB>`, `teamster migrate <TAB>`, and the values after an enum flag. That is the test
that catches the class of bug found in the generator. zsh is parsed and asserted structurally, but
what it offers is not driven: doing that needs a pseudo-terminal, and a flaky test of a completion
script is worth less than none.

The dependency brings `mmark` with it, for the man pages king can also generate and we do not use.
The linker drops it: the binary grows by 0.05 MB, 0.2%.

Two things are reported upstream — the fish nesting and the bash enum gap. If both are fixed, the
local correction disappears and this decision becomes "use king".
