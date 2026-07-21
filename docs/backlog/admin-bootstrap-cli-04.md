# The `cfg.Database` override block is written out three times

Status: open
Priority: later

Opened from session `admin-bootstrap-cli-04` (D-0131) covering two duplications,
and **narrowed** by session `cli-config-flag-uniformity` (D-0132), which
discharged the first of them. What remains is one shape, copied three times.

## What is still duplicated

`driver` and `dsn` overriding the `.joe` default is written out in three places:

- `databaseConfigFor` (`cmd/joe/db.go`) — the pure resolver every offline
  subcommand now goes through.
- `defaultOpenPanicStore` (`cmd/joe/main.go`) — its own copy of the same block.
- the daemon boot path (`cmd/joe/server.go`).

D-0132 deliberately left `defaultOpenPanicStore`'s copy in place while threading
a config into it, on the grounds that folding it into `databaseConfigFor` would
be half of this refactor performed opportunistically — which is what this item
exists to prevent.

## What was discharged

**Which config file a command reads** is settled. D-0132 put `--config` on every
subcommand a config file governs, with one name, one meaning, and one
missing-file posture held in a single shared `resolveConfigFlag`
(`cmd/joe/main.go`). `joe db backup` can now be pointed at the daemon's config
file; the consequence this item used to name — that it would happily back up a
different database — no longer holds. Two of the questions this item raised were
answered there: every offline subcommand a config governs gets the flag, and
D-0131's missing-file asymmetry is the rule for all of them rather than a
per-command choice.

## What a decision still has to settle

- **Is `JOE_CONFIG` meant to be a process-wide input or a daemon-only one?**
  Still open, and still the reason no subcommand honours it. The daemon's
  precedence is `--config` > `JOE_CONFIG` > default; every subcommand implements
  the first and third only. Making the env var process-wide would change
  behaviour when the flag is absent, so it cannot ride along with a strict
  addition — it needs its own decision.
- **Where does the shared resolver live?** A single helper taking a config path
  and returning `(*config.Config, store.DatabaseConfig)` would collapse the
  remaining shape, but it has to keep `defaultOpenBackupStore`'s no-migrate
  posture and `defaultOpenPanicStore`'s migrate posture distinguishable — that
  difference is deliberate (D-0114) and must survive the fold.

## Deliverable

A decision entry answering the two questions above, then the fold. The coherence
property D-0131 established and D-0132 generalized — one config load feeding
every config-governed use of a command — must be preserved by construction in
whatever replaces it.
