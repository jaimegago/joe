# Component export/import — a portable registration format

Status: open
Priority: later

Deferred from session `db-persistence-backup`, which shipped `joe db backup` and the
`/operations/persistence-and-backup/` page. Backup answers "how do I not lose this
install"; it does **not** answer "how do I move registrations between installs, or
re-register them after losing the encryption key". That is this item.

The motivating case is the one the backup page names as unrecoverable: an operator who
restores a database without its matching `encryption.key` has a Joe that boots, connects
nothing, and cannot be repaired — re-registering every component by hand is the whole of
the recovery procedure. An export/import path would make that a restore rather than a
rebuild. It is also the natural answer to promoting a dev install's registrations to
staging.

## What needs designing

- **A new API surface.** Export and import are not reachable through any existing endpoint.
  Both are admin-gated by nature; import is a **write** to the component set and therefore
  lands squarely under the write floor, which is up by default in observation mode. Whether
  import is even reachable pre-full-mode is part of the scope question, not an
  implementation detail.
- **A serialization format.** Versioned, and stable enough to be worth calling a format —
  an import path that only accepts exports from its own build is not portability. This is
  the bulk of the design.
- **The encrypted-config problem, which is the crux.** Component `config` blobs are
  encrypted at rest with the install's key, and the whole point of exporting is to reach an
  install that does **not** have that key. Two shapes, neither free:
  - *Plaintext export* — the export file becomes a credential store holding every
    component's secrets in the clear. That is a real secret-handling surface with real
    disclosure risk, and it would need to be treated as such (operator warnings at minimum,
    plausibly an explicit opt-in flag).
  - *Re-encrypt on import* — needs the target install's key at import time, which is
    tractable, but the exported blob still has to exist in *some* transportable form in
    between. This collapses back into the first problem unless the export carries
    ciphertext plus a key-wrapping scheme, which is a design of its own.
  - A third option worth weighing: export **structure without secrets** — coordinates,
    types, zone assignments — and require credentials to be re-supplied at import. Less
    convenient, dramatically smaller blast radius, and it may well be the right answer for
    v1.
- **Scope, which needs a decision before any of the above.** Components alone are not the
  install's durable state. Zones and RBAC grants, admin principals, and audit history all
  live in the same database, and a "moved" install missing its zones and grants is not
  meaningfully moved — it is a component list with no authorization model. Either the scope
  widens beyond components (and the name of this item is wrong) or the docs must be explicit
  that export/import is a component-registration convenience and **not** a migration path.
  Decide that first; it determines everything else.

## Related

- `docs/backlog/encryption-key-path.md` — the key-handling gap that makes this item feel
  urgent. Note the relationship honestly: a configurable key path plus a loud boot failure
  would remove much of this item's motivation, since the unrecoverable case would become
  preventable and detectable. Weigh that before investing here.
