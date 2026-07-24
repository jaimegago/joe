# Backlog — Skills/incident leaf help still runs behind a config load (deferred from cli-version-and-help / D-0143)

Status: open
Priority: next

D-0143 made requested help (`-h`, `--help`, `help`) print to stdout and exit 0
everywhere in the CLI. Two families are only partly there, because their config
load sits between the operator's help request and the flag set that answers it.

## What is left

`joe skills <sub> -h` and `joe incident <sub> -h` reach their sub-subcommand flag
set only after `runSkillsCommand` / `runIncidentCommand` have loaded the config
(`cmd/joe/main.go`, the load ahead of the `switch args[0]`; `cmd/joe/incident.go`,
same shape). The GROUP-level help was fixed — `joe skills -h`, `joe incident -h`
intercept ahead of the load — so what remains is the leaves:

1. **A broken or missing named config blocks help entirely.** `joe skills --config
   ./broken.yaml install -h` prints the config error and exits **1**; it never
   prints the usage text. Same for `joe incident --config ./broken.yaml status -h`
   and for a `--config` naming a file that does not exist. Requested help should
   not be able to fail this way: nothing in a config file affects what `install`'s
   flags are.

2. **A log line precedes the help text.** With a valid config, `joe skills install
   -h` emits `INFO loaded config from file path=...` before `Usage of joe skills
   install:`. Cosmetic, but it means help is not clean on stdout for a reader.

Both are pinned as-is (not as a bug) by
`TestRequestedHelp_SkillsAndIncidentLeaves` in `cmd/joe/help_test.go`, which
asserts the leaf help does exit 0 and print to stdout — under a config load that
succeeds. Changing the behaviour means updating that test.

## Why it was deferred

The fix is not a reorder within the existing shape. Both commands dispatch on
`args[0]` *after* the load, and the per-leaf flag sets are declared inside the
switch arms; `runSkillsCommand` additionally builds the skill manager from the
loaded config and the skills policy before dispatching. Parsing before loading
means hoisting the dispatch — either a pre-pass that constructs each leaf's flag
set twice, or a restructure that separates "which leaf" from "what that leaf
needs". That is a design choice about the two commands' shape, not a mechanical
change, and D-0143 deliberately did not make it while normalizing every other
command.

## Sketch of the fix

Give each leaf a `func() *flag.FlagSet` constructor keyed by name, so the
dispatcher can resolve the flag set from `args[0]` and parse it before deciding
whether it needs a config at all. `joe incident list` already demonstrates the
principle in-place — it is handled ahead of the config load precisely because it
needs no server — so the seam exists conceptually; this generalizes it to help.
