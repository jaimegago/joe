---
title: Persistence and backup
weight: 10
description: What Joe's durable state actually is, how to back it up against a live daemon, and what a restore does and does not bring back.
---

# Persistence and backup

Joe's durable state is **a directory, not a file**. Backing up the database alone
produces something that looks like a backup and restores into a Joe that cannot reach any
of its components. This page covers what to persist, how to copy it safely while Joe is
running, and what a restore honestly recovers.

For the config keys named here — `database.dsn`, `database.driver`,
`database.encryption_key_path`, and their environment equivalents — see
[Configuration](../../configuration/); this page names the keys but does not restate their
defaults.

## What to persist

Joe's durable state is its **`.joe` directory**, which holds two things that only make
sense together:

- **The database** — an embedded SQLite store (`joe.db`). Everything Joe knows lives here;
  see [What the database holds](#what-the-database-holds) below.
- **The encryption key** — `encryption.key`, a single file. Every registered component's
  configuration is encrypted at rest with it. The database is where the ciphertext lives;
  this file is the only thing that turns it back into a working component.

**Persist the directory, not the database file.** The two are useless apart: a database
without its key restores into a Joe that refuses to start, and a key without its database is
a key to nothing. In Kubernetes, that means a PersistentVolume covering the
`.joe` directory — mounting only the database file reproduces exactly the failure this page
exists to prevent.

The [session archive](../#state-on-disk) directory is separate state with its own key
(`server.session_archive_dir`); if the terminal retention action is *archive*, back it up
too.

### If you relocate the database

`database.dsn` (or `JOE_DATABASE_DSN`) moves the database file. **The encryption key does
not follow it automatically — move it deliberately** with
`database.encryption_key_path` (or `JOE_DATABASE_ENCRYPTION_KEY_PATH`). Set only the DSN
and the key stays in Joe's `.joe` directory under the home directory of the account running
`joe`, which splits your durable state across two locations that must **both** be
persistent. A container that mounts a volume at the relocated DSN path and leaves the home
directory ephemeral loses its key on every restart.

Point both at the same volume:

```yaml
database:
  dsn: /var/lib/joe/joe.db
  encryption_key_path: /var/lib/joe/encryption.key
```

The same two constraints apply to **both** values, deliberately — they behave identically:

- Each must be an **absolute path**, or one relative to the working directory.
- **A leading `~` is not expanded.** Joe does not perform tilde expansion on either value;
  each is used as written, and `~/joe.db` creates a directory literally named `~`. Write
  the paths out in full.

If the key does go missing, Joe will not start rather than starting broken — see
[What a restore does not bring back](#what-a-restore-does-not-bring-back).

This page is SQLite-only, because SQLite is the only functional store. A PostgreSQL
(`pgx`) driver value exists in the configuration surface but is not operational — see the
note under [`database`](../../configuration/#database). If that changes, none of the backup
mechanics below carry over; PostgreSQL would be backed up with its own tooling.

## Backing up

### `joe db backup` — the primary method

```bash
joe db backup /backups/joe-2026-07-17.db
```

This is **safe to run against a live Joe**. It opens the database directly rather than
talking to the daemon, takes a consistent copy of committed data, and leaves the source
untouched — including its schema version, so a backup taken from a database you suspect is
damaged does not alter it on the way out. It writes a single standalone file.

The command refuses an existing destination rather than overwriting it; pass `--force` to
replace one deliberately. It will not create the destination's parent directory — a
mistyped path fails instead of quietly depositing a backup somewhere nobody meant.

**Why the command exists, rather than "just copy the file".** Joe runs SQLite in WAL
(write-ahead logging) mode. Under WAL, committed data is not necessarily *in* `joe.db`:
it is written first to a `joe.db-wal` sidecar and only folded into the main file when a
checkpoint happens. Copy `joe.db` alone from a running Joe and you leave whatever is
currently in the sidecar behind — which, on a young database, can be everything including
the schema.

The result is the worst kind of backup: a **valid SQLite file that opens without error and
is quietly missing recent data, or every table**. Worse, you cannot catch it by inspecting
the copy. It is not *corrupt* — it is an older, internally consistent snapshot — so it has
all its tables and it **passes an integrity check** while still being short of the rows you
thought you saved. Nothing about the file announces the loss; it fails at restore time, not
at backup time. `joe db backup` reads through a real transaction, so it captures committed
data wherever it currently lives.

Back up the encryption key in the same operation. The backup command reminds you on every
successful run, because a database copied without its key is the failure mode this page
opens with.

### Stop-then-copy — the dependency-free alternative

If you would rather not rely on the command, stop Joe first:

```bash
# stop joe, then:
cp -a ~/.joe /backups/joe-2026-07-17/
```

On a clean shutdown SQLite checkpoints the WAL into the main database and removes the
sidecars, leaving a single complete file. Copying the whole `.joe` directory then captures
the database and key together. The cost is downtime; the benefit is that it needs nothing
but `cp`. Copying a *running* Joe's directory is not a substitute — the sidecars are live,
and you are racing the daemon.

## Restoring

### `joe db restore` — the primary method

```bash
# stop joe first, then:
joe db restore /backups/joe-2026-07-17.db --force
```

Restore puts a backup back at the configured database path. **Stop Joe first.** Restore
refuses to run when it finds a process holding the database open, and that refusal has no
override — but treat it as a backstop that catches the common mistake, not a licence to skip
stopping Joe. It cannot promise to notice every running process, and nothing stops a daemon
starting the moment after it looks.

Before it writes anything, it checks the things that otherwise fail silently later:

- **The backup is sound.** It opens the file read-only and runs an integrity check. A
  damaged backup is refused, and nothing is touched.
- **The backup is Joe's.** A valid SQLite database that is not a Joe database is refused,
  naming what it looked for.
- **The key is present.** If the backup carries encrypted component configuration and no
  `encryption.key` is in place, restore refuses and names the path it looked at — because
  restoring without it produces a database Joe will refuse to boot from (see
  [below](#what-a-restore-does-not-bring-back)). Boot checks this too; restore checks it
  *first*, which is the point — refusing here means nothing has been overwritten yet.
  `--allow-missing-key` accepts that outcome deliberately. It is a separate flag from
  `--force`, and neither implies the other.
- **The database is not visibly in use.** If it finds another process holding the database
  open, restore stops and tells you to stop Joe. See the caveat above: a clean result here is
  the absence of a signal, not a guarantee.
- **It clears stale WAL sidecars.** Restoring over a database left behind by an unclean stop
  is safe: the leftover `joe.db-wal` and `joe.db-shm` are removed as part of the restore, so
  the file you asked for is the file you get.

An existing database is replaced only with `--force`. Back it up first — `joe db restore`
overwrites, and the database it overwrites is not recoverable from anything but a backup.
Restore never writes to the backup file; it reads it through a read-only handle throughout.

Afterwards, put the **matching** `encryption.key` back where Joe expects it — the path in
`database.encryption_key_path`, or the `.joe` directory if that is unset — if it is not
already there, and start Joe.

### The manual procedure

If you would rather not use the command, the equivalent by hand is:

1. Stop Joe.
2. **Delete the `-wal` and `-shm` files beside the configured database path** — for the
   default location, `joe.db-wal` and `joe.db-shm`. Do this even though they look like
   scratch files.
3. Put the backup file at the configured database path (the `.joe` directory's `joe.db`, or
   wherever `database.dsn` points).
4. Put the **matching** `encryption.key` back where `database.encryption_key_path` points,
   or in the `.joe` directory if it is unset — the key that was current when that backup was
   taken.
5. Start Joe.

**Do not skip step 2.** If a `joe.db-wal` from the previous database is still sitting there
when Joe next starts, SQLite treats it as unwritten changes belonging to the file you just
put down and applies it on top. The backup you restored is silently discarded and **the old
database comes back in its place** — all of it. You get no error. The restored file passes
an integrity check, because the result is not corrupt: it is simply the wrong database,
intact. Then the leftover file is folded in and deleted, so the evidence of what happened
removes itself. Sidecars are only absent after a clean shutdown; after a crash, a `kill -9`,
or a container stop that did not wait, they are still there. This is the trap `joe db
restore` exists to close.

Step 4 is the other one that gets skipped. It no longer fails quietly — Joe refuses to
start without the key — but the database is just as unusable either way. Read on before
deciding you can do without it.

### What a restore does not bring back

**Without the matching key, Joe refuses to start.** This is checked in two places: `joe db
restore` catches it before anything is overwritten, and boot catches it if you restored by
hand or overrode the check with `--allow-missing-key`. Two distinct refusals:

- **The key file is missing** and the database holds encrypted component configuration. Joe
  does **not** mint a replacement — a fresh key cannot read a single stored value, and
  writing one would make the loss permanent and silent. It stops, names the key path it
  looked at, and names the database. A genuine first run — no key, nothing encrypted yet —
  still generates a key and starts normally; the database is what tells the two apart.
- **The key file is present but wrong.** Joe stops and lists **every component whose
  configuration it could not decrypt**. Read that list: if *every* component is named, the
  key is the wrong one and the fix is finding the right one. If a *single* component is
  named, that row is damaged rather than the key wrong — restore from a backup, or delete
  that component and re-register it.

That list is the diagnosis, and it is worth knowing why Joe cannot narrow it further: the
encryption cannot distinguish "wrong key" from "altered data". Both are one failure, and
which one it is can only be inferred from how many components failed. That is also why a
single damaged row stops boot rather than being skipped — Joe will not guess which of the
two it is looking at.

There is **no recovery** from a genuinely lost key and no repair path: no key rotation, no
re-encrypt, no prompt to supply the original. The component configuration in that database
is permanently unreadable. The only fix is restoring the original key file — so if it is
gone, re-registering every component by hand is the whole of the recovery procedure. What
changed is that you find this out at start-up, immediately, instead of discovering weeks
later that Joe has been running and reaching nothing.

**Service-account keys are not in the database.** They live in the config file under
`server.service_accounts`, so restoring a database does not restore them, and losing the
config file does not cost you the database. Back the config file up as the separate secret
it is.

## What the database holds

Structurally, by class — so you know what a restore returns and what a lost database costs:

- **Components and their encrypted configuration** — the registered external systems, with
  their credentials and coordinates as ciphertext.
- **Zones and RBAC grants** — the authorization model.
- **Admin principals** — who holds admin.
- **Auth sessions** — live human login state.
- **Chat sessions and their transcripts** — including incident linkage.
- **The audit log** — append-only, and the record of every governed decision.
- **The infrastructure graph** — nodes and edges. This one self-heals: the graph is
  rebuilt deterministically by the refreshers from the live systems, so a lost graph
  re-derives once Joe reconnects.
- **LLM usage records and settings** — per-call accounting and model configuration.
- **Panic and regime state** — the sticky panic row and incident regime.

Most of that does not regenerate. The graph is the exception; the audit log is the
opposite, being both irreplaceable and the thing a compliance story rests on. Size backup
retention against [the tables that only grow](../#tables-that-only-grow), and note that the
retention sweeper **deletes** expired sessions on a timer by default — a backup is the only
way back to a session Joe has purged.
