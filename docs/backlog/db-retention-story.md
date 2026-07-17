# Backlog — Whole-database retention story (audit rotation, LLM-usage/review-jobs pruning, legacy-session disposition, DB-size observability)

Status: open

This item collects the deferred, whole-database retention work that the
`db-retention-story` session documented but did not build. The operator-facing posture —
what the session sweeper deletes by default, and which tables grow without bound — is now
described in [`docs/public/operations/_index.md`](../public/operations/_index.md)
("Retention and growth"). **None of the work below is launch-blocking;** the shipped
behavior is honest and documented. This is the roadmap for retiring the "grows forever"
footnotes, not a bug list.

Basis for the current state: the `A001-SESSION-RETENTION-VERIFY` read-only investigation
and the `db-retention-story` documentation session that followed it (see the DECISIONS.md
entry ratifying the posture).

## Scope

### 1. Audit-log rotation (v2)

The `audit_log` table is append-only and grows monotonically by design: inserts only,
enforced both in application code (an insert-only repository) and in the database (triggers
that reject `UPDATE`/`DELETE`). D-0009 (Identity Phase F) fenced retention/rotation out of
v1 explicitly and named the only sanctioned escape today — `DROP TABLE audit_log` via the
Phase F down migration, then re-migrate — which discards the entire audit history. D-0009
also named the intended v2 shape: a **separate insert-rotate-only repository** behind which
a retention policy lives, leaving the existing insert-only `Repository` interface as-is.
Build that: a rotation/retention mechanism that preserves the dual (code + DB)
append-only-within-a-window guarantee while allowing bounded history.

### 2. LLM-usage retention or roll-up

`llm_usage` accrues one row per LLM call and has no prune path. Decide between a
time-window retention policy and a roll-up/aggregation mechanism (e.g. periodic compaction
into summary rows), and implement it. The Prometheus token counters already give
live spend signal, so this is about bounding the per-call detail table, not about metrics.

### 3. Disposition for review-jobs

`review_jobs` accrues with use and has no deletion path. Decide its retention posture
(purge-after-completion, time-window, or roll-up) and implement it, or explicitly record
that unbounded growth is acceptable for its expected volume.

### 4. Legacy `sessions` / `session_messages` disposition

The migration-001 `sessions` / `session_messages` tables are frozen: no live writer, no
deletion path, one dormant reader (the orphaned learn-from-sessions extractor). Their fate
is **not** an open question here — it is settled by
[`learn-from-sessions-fate`](learn-from-sessions-fate.md) (status: decided), which requires
the tables be **retained, not dropped**, because a future knowledge-extraction feature
depends on them. This item only cross-references that decision: any whole-DB retention
design must treat those tables as retained-but-frozen, and must not schedule them for
deletion absent an explicit reversal of `learn-from-sessions-fate`.

### 5. Database-size observability for operators

Operators currently have no first-class signal for how large the SQLite file or its
biggest tables have grown. Consider a size/row-count gauge (or a status-endpoint field) so
the unbounded-growth tables above are observable before they become a disk problem, rather
than only visible by inspecting the file on the host.

## Non-goals

- Changing the session sweeper's default behavior. Trash-grace auto-purge on / inactivity
  expiry off is the ratified posture; this item does not revisit it.
- Dropping or repointing the legacy session tables (see item 4).
- Weakening the audit-log append-only invariant. Any rotation design must keep the dual
  code + DB enforcement.
