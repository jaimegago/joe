# Encryption key: a configurable path, and whether a keyless boot should fail loudly

Status: open
Priority: now

Filed by session `db-persistence-backup`, which documented both halves of this on
`/operations/persistence-and-backup/` as real, current behavior with real, current
consequences. The two pieces below are **separable** — the first is a small, self-contained
change; the second is a design question that deserves its own session. Do not let the second
hold the first.

## 1. Make the key path configurable (the small change)

The database path is configurable via `database.dsn` / `JOE_DATABASE_DSN`. The encryption
key path is **not**: it is computed from Joe's `.joe` directory under the home directory of
the account running `joe`, takes no config input, and therefore does not follow a relocated
database.

The consequence is a live footgun, and it is documented as one on the backup page: an
operator who relocates the database onto a persistent volume has silently split their
durable state across two locations. If the home directory is ephemeral — the default
situation in a container — Joe mints a **fresh key on every restart** and reaches none of
its components, while the database it was told to persist survives perfectly.

The change: a config key plus an environment variable mirroring the DSN pattern exactly, so
the key can be placed on the same volume as a relocated database. Cite the current shape
structurally rather than by line: the key path is resolved by the `paths` helper that
composes the `.joe` directory with the key filename, called from the composition root's
encryption wiring, which then hands the key to the encrypted component repository.

Two things to get right, neither of which is incidental:

- **Match the DSN's semantics deliberately, including the sharp edge.** The DSN takes an
  absolute path and does **not** expand `~`. Mirroring that pattern means inheriting that
  trap for a second key. Either inherit it knowingly and document it in the same breath, or
  fix both together — but do not fix expansion for the key alone and leave the DSN
  inconsistent, which is the worst of the three outcomes.
- **The register entry that goes with it.** `docs/project/SITE-CLAIMS.md` records the
  "relocating the database does not move the key" claim as **unguarded** — it holds by the
  absence of a config input. Landing this item changes that claim's shape, and the register
  and the backup page both revise with it.

## 2. Should a keyless boot fail loudly? (the design question — its own session)

Current posture, stated as it is rather than as a bug: the key loader treats a **missing
key file as a first-run condition** and writes a fresh random key rather than failing. That
is correct and necessary for a first install — it is how a new Joe gets a key at all. The
problem is that it is indistinguishable, from inside that function, from a disaster: an
existing install whose key was lost takes exactly the same path.

Downstream, the connect path lists components, the decrypt fails under the new key, the
error is logged at Warn, the loop registers **zero adapters**, and boot **succeeds**. So the
failure presents as a Joe that starts cleanly, serves its API, and quietly does nothing —
the worst possible shape for an operator to diagnose, and permanent by the time they notice.

The question is genuinely open and is not "add an error":

- **What distinguishes the two cases?** A missing key with **zero component rows** is a
  first run. A missing key with **component rows present** is a disaster. That distinction is
  available at boot and is probably the whole answer — but it lives at the composition root,
  not inside the key loader, which is why this is a design question about where the check
  belongs rather than a one-line fix.
- **What should the failure be?** Refusing to boot is defensible (fail-closed, consistent
  with Joe's posture elsewhere) but it turns a degraded install into a down install, and an
  operator who genuinely wants to start over now needs an escape hatch. A loud, unmissable
  boot-time error that still serves may be the better trade — that is the call to make.
- **Does it interact with the write floor?** Booting into a broken-decrypt state under
  observation mode is inert anyway. Under full mode it may not be.

Whatever is decided, it invalidates the current claim on
`/operations/persistence-and-backup/` — which says plainly that Joe boots cleanly and breaks
silently — and the corresponding `SITE-CLAIMS.md` entry, whose pinning is recorded as
**partial**: the silent-generation half is covered by the key-loader tests, but no test
asserts that a wrong key fails to decrypt a component through the repository, and none
asserts that a keyless boot succeeds while connecting nothing. Whichever way this lands,
that missing coverage is part of the work.

## Related

- `docs/backlog/export-import-components.md` — the recovery path for an install that has
  already lost its key. This item is the prevention; that one is the cure. Prevention is
  cheaper.
