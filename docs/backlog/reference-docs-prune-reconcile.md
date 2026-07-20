# Backlog — Residue from the reference-docs prune reconcile

Status: open

Priority: next

The session `reference-docs-prune-reconcile` reconciled `docs/reference/security-in-layers.md`
and `docs/reference/joe-architecture.md` against the live tree, correcting the act-policy
reachability characterization and sweeping the D-0113 (knowledge store) and D-0118
(onboarding/clarifications) prunes out of both. Three threads were opened but not closed.

## 1 — The unconditional-denial property is unpinned

The load-bearing correction is that **no registered Mutate tool's `PolicyKey` resolves to a
live `ActPolicy` field**, so `CheckAccess`'s allow branch is unreachable and every registered
mutation is denied regardless of configuration. Nothing pins this.

`TestCheckAccess_MutateDefaultDeny` pins denial under `DefaultPolicy()`, which is the weaker
claim — it would still pass if a key were grantable but simply defaulted off. The test that
exercised the allow branch (`TestCheckAccess_MutateEnabled`) was deleted with
`publish_doc_update_git` rather than migrated, and the explanatory comment left in
`internal/safety/tier_test.go` is the only record of the property.

A structural test would assert the disjointness directly: for every `ActionMutate` row in
`toolRegistry`, `IsT3Allowed(row.PolicyKey)` is false under a policy with **every** `act`
toggle enabled. That fails the moment someone ships a tool with a live key — which is exactly
the event that should force this doc claim to be revisited. This is the same shape as the gaps
in [`tool-class-break-tests`](tool-class-break-tests.md).

Note this is a *documentation-integrity* test, not a safety regression test: the property it
pins is an accident of what was deleted, not a designed invariant. It should be written so its
failure reads as "the docs now overstate the denial," not as "safety broke."

## 2 — One published page implies a light revision

`docs/public/concepts/action-model.md` is substantially accurate — it already describes the
narrow comment/request-changes surface, carries no knowledge-store residue, and correctly
states that none of the mutation surface runs in any configuration Joe currently boots.

The one soft spot is the gate-chain step 4 (around `:118-121`): "each mutate class must be
enabled in the safety policy … a class that is not enabled is denied." That is not false, but
it implies enabling is an available operator act. It is not, for any tool that ships. A
sentence noting that no shipped tool can currently be enabled would match the corrected
reference docs. Deliberately deferred here rather than folded into a docs/reference commit —
publishing is its own decision surface (D-0052).

## 3 — Two backlog files cite deleted symbols

Both predate D-0113 and reference code that no longer exists, at `file:line` citations that no
longer resolve:

- [`tool-class-break-tests`](tool-class-break-tests.md) — its Gap 1 and Gap 2 are entirely
  about `detect_doc_drift` and the `publish_doc_update` family. Both gaps are now moot as
  written; the file needs re-deriving against the surviving tool set rather than editing.
- [`public-docs-feature-inventory`](public-docs-feature-inventory.md) — its MCP roster and
  doc-publishing entries describe the pre-prune tree.

Neither was in this session's scope. Note that `learn-from-sessions-current-state.md` is in the
same condition but is **already** tracked, at the tail of
[`learn-from-sessions-fate`](learn-from-sessions-fate.md) — do not open a second item for it.

## 4 — `joe-architecture.md` needs the same tense-and-subject sweep

`reference-docs-prune-reconcile-02` established the tense-and-subject convention for
`docs/reference/` (CLAUDE.md, D-0126) and swept `security-in-layers.md` under it.
`joe-architecture.md` was surveyed in the same session but deliberately **not** edited — it is
a comparable-size job and belongs in its own commit.

Phase-1 survey counts, to be re-derived against the live file before editing:

- **23 HISTORY candidates** (past tense, subject is the repo — delete under the convention).
- **7 AMBIGUOUS** (a historical clause and a live claim in one sentence — cut the clause, keep
  the claim): lines 80, 186, 288, 539, 541, 545, 555.
- Clusters, densest first: the Action Safety Framework section (~515–549, 7 items);
  Implementation Phases (~553–571, 4 items, highest per-line density); Core Agent / decision
  flow (~159–240, 4 items); Data Layer + LLM adapter (~280–394, 3 items); singletons at ~32,
  ~80, ~100.

**Three units are structurally organized around a removal**, so this is a structural edit, not
only a sentence sweep. Each must be rebuilt as a present-tense description of what the thing
now is:

- `### Self-protection invariants` (~543) — the subsection exists only to explain that guards
  *were retired* and why their absence is safe; it documents no live mechanism. Becomes a
  statement of the structural guarantee itself.
- The blockquote at ~186 — "`.joe/` ingestion removed (D-0042)", a pure deletion notice
  wrapping a live invariant (Joe ingests no repo-authored `.joe/`) and live behaviour (it still
  builds a `git_repo` node).
- The blockquote at ~569 — closing note to Implementation Phases, whose sole purpose is
  explaining which phases were deleted and why they are absent from the table.

Note `### Designed but not yet built` (~547) is organized around *never-built*, not removed —
the convention permits it; do not sweep it.

## Why this class of drift happened

Recorded in the decision entry: the D-0113 prune swept code, tests, migrations, and CLAUDE.md,
but not `docs/reference/`. Nothing in the convention pointed a pruning session at those two
files, so they kept describing a subsystem the binary no longer shipped for two full decision
cycles (D-0113 through D-0118 landed without either doc being touched). The durable fix is
either a prune checklist that names `docs/reference/` explicitly, or a test that greps the
reference docs for symbols absent from the tree.
