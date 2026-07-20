Launch positioning, the open-source launch-blocker checklist, and decoupling from a former employer
Status: done — discharged by launch (backlog-priority-triage, D-0127)

This file rehomes the open-source launch context that lived only in the retired
`JOE_PROJECT_KNOWLEDGE.md` §8 (and in the now-absent `LAUNCH_READINESS.md` /
`HISTORY_AUDIT.md`, neither of which is present on disk). It holds only what is
otherwise unrecorded in the tracked spines. For the history-scrub work that
*does* survive in a tracked doc, see the cross-reference at the bottom.

## Positioning

- Joe is a **personal, public, open-source portfolio project** under
  **Apache-2.0** (`LICENSE` at repo root is ground truth; the CLAUDE.md
  license-posture invariant restates it).
- Distributed today as **build-from-source only** — no release binaries have
  been published yet (Homebrew/Scoop/`install.sh` remain undecided). The
  goreleaser release pipeline is **armed** (D-0091, `release-pipeline-01`): it
  publishes a GitHub Release on a `v`-prefixed tag push; no tag has been cut
  yet, so distribution is unchanged in practice. See
  `docs/backlog/release-pipeline.md` for the tag-cut work.
- The README was rewritten 2026-06-05 to reflect the single-binary architecture.

## Relationship to a former employer — decoupled by design

- The company is the author's **former employer**; Joe is a personal project deliberately
  **decoupled** from it. There is no intended product, code, or content linkage.
- A git-history audit (`HISTORY_AUDIT.md`, dated 2026-05-28 — note: that file is
  no longer present on disk) found **no proprietary content from that employer**: no colleague
  names, hostnames, codenames, or internal systems in any file or commit message.
- The **only** linkage to the former employer found was metadata: **3 commits (all dated
  2026-02-12) carried a former-employer email address** instead of
  `gagojaime@gmail.com`. This was the single most concrete pre-publish history
  blocker and has since been rewritten (see `docs/project/DECISIONS.md` D-0089/D-0090).

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
- **History scrub** (former-employer email rewrite + compiled-binary blob purge) →
  **RESOLVED** by the `history-scrub` session (see `docs/project/DECISIONS.md`
  D-0089). See cross-reference below.

## History scrub — what survives in a tracked doc (cross-reference)

The history-scrub mechanics are partially recorded in the security-findings
punchlist (since archived out of the repo): its
**§D "Migration note"** and **§C** defer the `review_jobs` table / migration
cleanup to the **git-filter-repo "history scrub (Stream C)"** and name the
`git-filter-repo` path explicitly.

What the punchlist does **not** carry — and what this file therefore preserved —
is the concrete launch-time scrub scope from JPK §8, **executed by the
`history-scrub` session** (see `docs/project/DECISIONS.md` D-0089):

- Rewrote the **3 commits** carrying a former-employer email address (2026-02-12) to
  `gagojaime@gmail.com`, on both the author and committer fields; no other
  identity (GitHub `noreply`, dependabot) was touched.
- Purged the old compiled-binary blobs (`joe` / `joecored`) from history.
- Path used: **`git-filter-repo`**, with empty/degenerate-commit pruning
  disabled so the **full commit history is preserved unsquashed** rather than
  squashed away. The one commit whose only content was the binary removal is
  retained as an empty commit.
