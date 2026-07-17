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

For the config keys named here — `database.dsn`, `database.driver`, and their environment
equivalents — see [Configuration](../../configuration/); this page names the keys but does
not restate their defaults.

## What to persist

Joe's durable state is its **`.joe` directory**, which holds two things that only make
sense together:

- **The database** — an embedded SQLite store (`joe.db`). Everything Joe knows lives here;
  see [What the database holds](#what-the-database-holds) below.
- **The encryption key** — `encryption.key`, a single file. Every registered component's
  configuration is encrypted at rest with it. The database is where the ciphertext lives;
  this file is the only thing that turns it back into a working component.

**Persist the directory, not the database file.** The two are useless apart: a database
without its key restores into a Joe that starts and connects nothing, and a key without its
database is a key to nothing. In Kubernetes, that means a PersistentVolume covering the
`.joe` directory — mounting only the database file reproduces exactly the failure this page
exists to prevent.

The [session archive](../#state-on-disk) directory is separate state with its own key
(`server.session_archive_dir`); if the terminal retention action is *archive*, back it up
too.

### If you relocate the database

`database.dsn` (or `JOE_DATABASE_DSN`) moves the database file. **The encryption key does
not follow it.** The key path is not configurable: it is resolved from Joe's `.joe`
directory under the home directory of the account running `joe`, regardless of where the
DSN points. Relocate the database and you have split your durable state across two
locations, and **both must be persistent**. A container that mounts a volume at the
relocated DSN path and leaves the home directory ephemeral will mint a fresh key on every
restart — see [What a restore does not bring back](#what-a-restore-does-not-bring-back).

Two constraints on the DSN value itself:

- It must be an **absolute path**, or one relative to the working directory.
- **A leading `~` is not expanded.** Joe does not perform tilde expansion on this value; it
  is passed to the storage engine as written, and `~/joe.db` creates a directory literally
  named `~`. Write the path out in full.

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

1. Stop Joe.
2. Put the backup file at the configured database path (the `.joe` directory's `joe.db`,
   or wherever `database.dsn` points).
3. Put the **matching** `encryption.key` back in the `.joe` directory — the key that was
   current when that backup was taken.
4. Start Joe.

Step 3 is the one that gets skipped, and it fails quietly. Read the next section before
deciding you can do without it.

### What a restore does not bring back

**Without the matching key, Joe boots cleanly and is broken.** There is no guard on this
path, and the behavior is worth stating exactly:

- Joe finds no key file, **generates a fresh one**, and continues starting. This is not an
  error condition; it is the same code path that gives a first-run install its key.
- The server comes up. The API serves. Nothing announces a problem at the top of the log.
- But every registered component's configuration was encrypted with the *old* key and
  cannot be decrypted with the new one. Joe connects to **nothing**, warns in its logs, and
  the components API returns errors.

There is **no recovery** from this and no repair path: no key rotation, no re-encrypt, no
prompt to supply the original key. The component configuration in that database is
permanently unreadable. The only fix is restoring the original key file — so if it is gone,
re-registering every component by hand is the whole of the recovery procedure.

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
