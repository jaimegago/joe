# Config-path resolution is duplicated and inconsistent across the `joe` subcommands

Status: open
Priority: later

Opened from session `admin-bootstrap-cli-04` (D-0131), which added `--config` to
`joe admin bootstrap` and deliberately did **not** consolidate the surrounding
duplication. This item is a **refactor needing its own decision**, not a bug fix
to attempt opportunistically: the inconsistency below is partly accidental and
partly load-bearing, and nobody has decided which is which.

## What is duplicated

Two separate shapes are copied around, and they are not the same shape.

**1. Which config file a command reads.** Four different answers ship today:

- The daemon (`resolveConfigPath`, `cmd/joe/server.go`) honours `--config`, then
  `JOE_CONFIG`, then the default path.
- `joe panic` (`cmd/joe/main.go`) honours `--config` only, with
  `paths.DefaultConfigPath()` as the flag's default value — so it never
  distinguishes "not passed" from "passed the default", and it does **not**
  honour `JOE_CONFIG`.
- `joe admin bootstrap` (`cmd/joe/admin.go`) honours `--config` with an empty
  default so the two cases stay distinguishable, and does **not** honour
  `JOE_CONFIG`. D-0131 scoped it that way on purpose: the flag was the stated
  gap, and adding an env-var path was a second decision.
- `joe unlock`, `joe db backup`, `joe db restore`, `joe skills`, and
  `joe incident` read `paths.DefaultConfigPath()` unconditionally, with no way
  to point them anywhere else.

So an operator running the daemon from `/etc/joe/config.yaml` can now point
`joe admin bootstrap` at it, and still cannot point `joe db backup` at it — that
command will happily back up a different database.

**2. The `cfg.Database` override shape.** `driver` and `dsn` overriding the
`.joe` default is written out three times: `databaseConfigFor` (`cmd/joe/db.go`),
`defaultOpenPanicStore` (`cmd/joe/main.go`), and the daemon boot path
(`cmd/joe/server.go`). `resolveDatabaseConfig`'s own doc comment has said "folding
all three together is a separate change and is deliberately not attempted here"
since it was written.

## What a decision has to settle first

- **Is `JOE_CONFIG` meant to be a process-wide input or a daemon-only one?** If
  process-wide, every offline subcommand should honour it and the `joe panic` /
  `joe admin bootstrap` divergence is a defect. If daemon-only, the current state
  is nearly right and only wants documenting.
- **Should every offline subcommand grow `--config`?** `joe db backup` and
  `joe db restore` have the strongest case (they act on a database the config
  names). `joe skills` reads only `skills.trusted_sources` and the server
  address, so the argument is weaker.
- **What is the missing-file posture for a flag added to an existing command?**
  D-0131 chose: default path missing is fine, explicitly-named path missing exits
  1. That asymmetry should be the rule for any further `--config`, not
  re-litigated per command.
- **Where does the shared resolver live?** A single helper taking a config path
  and returning `(*config.Config, store.DatabaseConfig)` would collapse both
  shapes, but it has to keep `defaultOpenBackupStore`'s no-migrate posture and
  `defaultOpenPanicStore`'s migrate posture distinguishable — that difference is
  deliberate (D-0114) and must survive the fold.

## Deliverable

A decision entry answering the four questions above, then the refactor, then the
per-command flag additions the decision sanctions. The coherence property D-0131
established — one config load feeding both principal validation and database
targeting — must be preserved by construction in whatever replaces it.
