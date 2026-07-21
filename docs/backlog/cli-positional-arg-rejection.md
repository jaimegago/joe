# CLI positional-argument rejection is inconsistent tree-wide

Status: open
Priority: later

Opened from session `transport-cmd-flag-rejection` (D-0133), which fixed `joe
mcp` and `joe slack` (they took an unused `args` parameter and parsed nothing,
so both unknown flags and surplus positionals were silently accepted and
ignored) but was scoped to those two commands only. The wider tree was
surveyed while re-deriving that fix's scope and is not uniform.

## What is already correct

Every command that defines a real `flag.FlagSet` and calls `fs.Parse` rejects
an **unknown flag** (`flag.ContinueOnError` surfaces the parse error, the
command returns exit 2). That much is tree-wide.

## What is still inconsistent or missing

- **`joe panic` and `joe unlock`** (`cmd/joe/main.go`) parse flags correctly
  but never check `fs.NArg()`. A surplus positional — `joe panic extra` — is
  silently accepted and ignored rather than rejected.
- **`joe incident status`, `joe incident declare`, `joe incident resolve`**
  (`cmd/joe/incident.go`) have the same gap: flags are parsed and unknown ones
  rejected, but no `NArg()` check exists, so a surplus positional after a
  legitimate flag is silently ignored.
- **`joe skills list`** (`cmd/joe/main.go`) does no flag parsing at all — it
  reads `args[0]` to dispatch and never looks at `args[1:]`, so both unknown
  flags and surplus positionals are silently accepted. This is the same shape
  the `transport-cmd-flag-rejection` session fixed on `joe mcp`/`joe slack`,
  just for a sub-subcommand rather than a top-level one.
- **The exit code for a bad positional count is not settled.** `joe db
  backup`/`joe db restore`/`joe admin bootstrap` return 2 (the same code as a
  flag-parse failure); `joe skills install`/`remove`/`update`/`approve`/
  `reject`/`reload` return 1. `transport-cmd-flag-rejection` picked 2 for `joe
  mcp`/`joe slack`, following the more recent precedent, but did not reconcile
  the older skills-subcommand code — that reconciliation is exactly the kind
  of opportunistic cross-cutting change a narrowly-scoped session should not
  make.

## Deliverable

A decision settling the exit code for a bad positional count tree-wide, then
one pass adding the missing `NArg()` checks (panic, unlock, the three incident
leaf commands) and the missing flag set (skills list), reconciling the older
skills-subcommand exit code to match.
