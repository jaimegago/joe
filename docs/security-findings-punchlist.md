# Security / cleanup punch-list

> **Status: open-items tracker.** Date opened: 2026-06-09.
>
> These items were surfaced during the security-architecture design session and
> the egress / direct-HTTP-surface investigations. They are **not** part of the
> security-architecture *direction* (see `docs/decisions/security-architecture-direction.md`
> for design commitments) — this is the concrete to-do residue: confirmed
> findings, correctness bugs, deferred cleanups, and doc drift. Each item names
> where it came from so it can be re-verified, since investigation findings are
> leads to confirm against the live tree, not ground truth.
>
> Remove items as they land. Add the commit that closes each.

---

## A. Live-path findings (real gaps on code that is actually used)

### A1. `publishProposalToTarget` performs no RBAC zone check — INVESTIGATE
The live agentic tool path (`publish_doc_update` → in-process
`PublishProposal` → `publishProposalToTarget`) writes to the **first source of
the matching target type regardless of caller zone**. This is an authorization
gap on a *live* managed-system mutation — it survives the direct-HTTP-surface
removal (that removal deleted the dead HTTP entry, not this dispatch).
- **Source:** egress audit (2026-06-09) §1.B; direct-HTTP investigation
  (2026-06-09) §5(a).
- **Connects to:** security-architecture-direction §9 (publish target +
  credential should be component-and-zone-scoped, resolved at the accessor seam)
  and §3 (target-class). The fix likely belongs in the §9 credential/accessor
  work, not a standalone patch — confirm during that session.
- **Next step:** read-only investigation of how `publishProposalToTarget`
  resolves its target source and whether the caller principal/zone is even
  available at that point; then decide fix shape. Do **not** patch blind.

### A2. `publish_doc_update` policy-key bug — CORRECTNESS
The generic `publish_doc_update` tool is policy-gated under a single PolicyKey
`confluence_publish` (`safety/tier.go:233`) for **all three** target types — so a
Notion or Git publish is checked against the Confluence policy key. Per-target
classification entries `publish_doc_update_{confluence,notion,git}`
(`tier.go:229-231`) exist but no tool by those names is registered.
- **Source:** egress audit (2026-06-09) §1.B "Minor."
- **Next step:** small, self-contained. Decide whether per-target policy keys
  should be wired (and the generic tool split), or whether one key is intended.

### A3. Non-SELECT SQL relies on adapter rejection, not classification — GAP
`postgres_query` / `mysql_query` run raw SQL but are classified `ActionRead`
(`tier.go:115,117`) and gated `rbac.ActionRead`. Read-only is enforced **inside
the adapter's `Query()`**, not by the safety classification or the floor. A
non-SELECT statement reaching these tools would **not** be floor-gated — it
relies entirely on the adapter rejecting it.
- **Source:** egress audit (2026-06-09) §2 note.
- **Why it matters:** this is a classification-correctness hole of the kind the
  whole binary read/mutate model depends on being airtight (cf.
  security-architecture-direction §4 — "a tool misclassified Read that mutates
  walks through the floor"). It's the concrete instance of that abstract risk.
- **Next step:** confirm the adapter rejection is robust, and/or reclassify or
  split read vs write SQL paths so the classification — not the adapter — is the
  guarantee.

---

## B. Deferred cleanup (dead surface, not dangerous)

### B1. Possible third vestigial-surface pass — alerting / datastore / networking / registry client methods
The direct-HTTP-surface removal (2026-06-09) flagged that the
alerting/datastore/networking/registry `*client.Client` methods appear equally
**test-only** (two-binary thin-client remnant), but their HTTP routes were out of
that task's scope, so they were left.
- **Source:** direct-HTTP removal manifest (2026-06-09) §3 "EXPLICITLY NOT
  TOUCHED" note.
- **Judgment:** diminishing returns. The dangerous (mutation) and the bulk of the
  dead read surface are already removed. Only do this pass if a clean "single live
  entry path" surface is wanted for launch; otherwise low priority.

### B2. k8s GET route — RESOLVED (deleted, not kept)
The route was **deleted** (commit `60ac5af`): the auth/RBAC regression
assertions were moved onto the accessor seam directly (Option 2) and a test-only
probe handler (Option 1, `_test.go`-only, never in the production binary). No
production route now exists solely to serve tests, and there is **no remaining
exception** to "the vestigial direct-HTTP managed-system surface is gone." Closed;
left here as a record.

---

## C. Documentation drift (state-docs now wrong; user-maintained)

Two removals this session (review-agent subsystem; direct-HTTP managed-system
surface) deleted features/routes that several **current-state** docs still
describe. These are user-maintained, not CC-actionable.

**Distinguish state-docs (fix) from history-docs (leave — they are true about the
past):**
- **History docs — LEAVE** (accurately record what was built/done):
  `docs/milestones-completed.md` (Phase 8/9/10 entries), `docs/may_16th_refactor_plan.txt`.
  Optionally add a forward-note that a subsystem was later removed, but do not
  rewrite history.
- **State docs — FIX** (describe how Joe works *now*, now wrong):
  - `JOE_PROJECT_KNOWLEDGE.md` — §1/§2 still list `joe review`; §1.1 could gain a
    row (review agent → deleted, like the REPL). Update for both removals. This is
    the pinned Project knowledge — highest-priority because every new chat inherits
    it.
  - `CLAUDE.md` — `joe review` in subcommand list; migration count; any
    direct-HTTP route descriptions now removed.
  - `README.md` — `joe review` line.
  - `docs/joe-architecture.md` — the "Review Agent — Phase 10" section, ascii box,
    webhook endpoints, schema diagram, and any of the removed direct-HTTP route
    descriptions (lines ~133-134 ascii diagram, ~210-228 HTTP API reference block:
    the removed k8s/git/aws GET routes).
  - `docs/joe-dataflow.md` — **RESOLVED by deletion (D-0042, session
    joefile-removal):** the whole doc was dominated by the deleted `.joe/`
    ingestion model and the retired two-binary `joecored` world, so it was
    removed wholesale rather than patched.
- **Accurate, leave:** `docs/joe-architecture.md:1925` `review_jobs:` schema
  diagram — the table still exists (migration option A kept it). Accurate until/
  unless the table is dropped in the history scrub (Stream C).

---

## D. Migration note (deferred to history scrub)

Migration `007_review_jobs` and the (now-unused) `review_jobs` table were
**deliberately kept** (option A) during the review-agent removal — `023` mutates
the column, so deleting `007` breaks replay, and an empty unused table is
harmless. The cleaner end state (the table never existing) belongs to the
git-filter-repo **history scrub (Stream C)**, not a forward migration now.
- **Source:** review-agent removal manifest (2026-06-09) migrations section.
- **Action:** revisit during Stream C; until then, leave all migration files and
  the table untouched.
