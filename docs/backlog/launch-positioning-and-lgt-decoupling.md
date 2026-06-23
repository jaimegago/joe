Launch positioning, the open-source launch-blocker checklist, and LGT decoupling
Status: open

This file rehomes the open-source launch context that lived only in the retired
`JOE_PROJECT_KNOWLEDGE.md` §8 (and in the now-absent `LAUNCH_READINESS.md` /
`HISTORY_AUDIT.md`, neither of which is present on disk). It holds only what is
otherwise unrecorded in the tracked spines. For the history-scrub work that
*does* survive in a tracked doc, see the cross-reference at the bottom.

## Positioning

- Joe is a **personal, public, open-source portfolio project** under
  **Apache-2.0** (`LICENSE` at repo root is ground truth; the CLAUDE.md
  license-posture invariant restates it).
- Distributed today as **build-from-source only** — no release binaries,
  goreleaser, Homebrew/Scoop, or `install.sh`. Release tooling is deliberately
  deferred for v1.
- The README was rewritten 2026-06-05 to reflect the single-binary architecture.

## Relationship to LGT — decoupled by design

- LGT is the author's **employer**; Joe is a personal project deliberately
  **decoupled** from it. There is no intended product, code, or content linkage.
- A git-history audit (`HISTORY_AUDIT.md`, dated 2026-05-28 — note: that file is
  no longer present on disk) found **no LGT proprietary content**: no colleague
  names, hostnames, codenames, or internal systems in any file or commit message.
- The **only** LGT linkage found was metadata: **3 commits (all dated
  2026-02-12) carry the author email `jaime.gago@lgt.com`** instead of
  `gagojaime@gmail.com`. This is the single most concrete pre-publish history
  blocker and must be rewritten before going public.

## Open-source launch-blocker checklist (JPK §8, reconciled against Jun 5–6 state)

The consolidated checklist below existed only in JPK / `LAUNCH_READINESS.md`
(dated 2026-05-28). Status as last reconciled:

- **B1 — LICENSE present** → **RESOLVED** (Apache-2.0 at repo root).
- **B2 — a one-key stranger can start Joe** → **RESOLVED per README** (provider
  auto-selected from whichever key is present; defaults to Claude if both).
- **B3 — README contradicted the Phase-2 architecture** → **RESOLVED** (README
  rewritten 2026-06-05 to single-binary).
- **B4 — OASIS evaluation story missing from the README** → **STILL OPEN**, gated
  on a refreshed post-Phase-2 score. Tracked separately in
  [oasis-relationship.md](oasis-relationship.md).
- **History scrub** (LGT email rewrite + compiled-binary blob purge) →
  **STILL OPEN**. See cross-reference below.

## History scrub — what survives in a tracked doc (cross-reference)

The history-scrub mechanics are partially recorded in the tracked
[docs/security-findings-punchlist.md](../security-findings-punchlist.md): its
**§D "Migration note"** and **§C** defer the `review_jobs` table / migration
cleanup to the **git-filter-repo "history scrub (Stream C)"** and name the
`git-filter-repo` path explicitly (`docs/security-findings-punchlist.md:116`,
`:120-128`).

What the punchlist does **not** carry — and what this file therefore preserves —
is the concrete launch-time scrub scope from JPK §8:

- Rewrite the **3 commits** carrying `jaime.gago@lgt.com` (2026-02-12) to
  `gagojaime@gmail.com`.
- Purge **~71 MB of old compiled-binary blobs** (`joe` / `joecored`) from history.
- Recommended path: scrub with **`git-filter-repo` (Path B)** — the ~174-commit
  history is an asset worth keeping rather than squashing away.
