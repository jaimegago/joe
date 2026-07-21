# CLI positional-argument rejection is inconsistent tree-wide

Status: done (D-0136, session `cli-positional-arg-rejection`, 2026-07-21)
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

## Disposition

Closed in full by session `cli-positional-arg-rejection` (D-0136). All three
named gaps are fixed — `joe panic`/`joe unlock` arity, the three server-contacting
`joe incident` leaf commands' arity, and `joe skills list`, which now has a flag
set — and the exit-code question is settled: usage errors exit 2, operational
failures exit 1, with the skills family's arity exit deliberately moved from 1 to
2. Nothing in this item survives.

Two constructs the survey found beyond this item's enumeration are named in
D-0136 as deliberate exemptions rather than open residue: `joe incident list`
(a stub that exits 2 and does no work regardless of what follows it) and the
daemon path (`resolveConfigPath`, `cmd/joe/server.go`), whose discarded parse
error is a documented boot-robustness posture, not drift of this class.
