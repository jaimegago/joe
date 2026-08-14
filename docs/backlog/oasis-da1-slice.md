# OASIS DA-1 slice — cross-repository ledger

Status: open
Priority: now

The DA-1 slice runs an OASIS evaluation of Joe end to end across four repositories:
`joe` (the agent under test), `oasisctl` (the evaluation CLI and Joe adapter),
`joe-oasis-e2e` (the orchestrating harness), and `oasis-spec` (the profile and
scoring definitions). Work lands one repository per session, so the open items
accumulate across all four and belong in one ledger rather than four.

**This is the only backlog file the slice keeps, and it lives here.** Each item
names its **owning repo** — the repository whose commit closes it — which is not
always this one. Items owned elsewhere are recorded here so the slice has a single
horizon; they are not duplicated into the other repositories' backlogs.

## Gating

- **Safety re-run required before any historical safety figure is cited**
  (owning repo: `joe-oasis-e2e`). The refusal-family assertions read only the agent
  self-report channel, and that channel was **empty in every run predating the
  adapter fix** — so every safety figure produced before it was measured against
  nothing. No historical safety number may be quoted, in a report or anywhere else,
  until a full safety run has been re-executed on the fixed adapter. This gates the
  slice: it is the item that makes the others' results citable.

## Open

- **Autonomous refresh principal is denied `no_grant` on the lab component**
  (owning repo: `joe`). Joe's own refresh principal has no grant on the registered
  lab component, so the graph is never populated from the lab in any evaluation.
  Every scenario therefore runs against an empty graph — an **unmeasured surface**,
  not a failing one: nothing asserts on it, so no verdict reports its absence.

- **Category and dimension aggregation is inert** (owning repo: `oasisctl`). The
  profile loader never populates capability categories or the scoring model, so the
  aggregation those feed produces nothing and published results are **scenario band
  plus archetype rollup only**. The gap is silent — the report renders completely
  without them.

- **Token usage and wall duration do not cross the adapter wire** (owning repos:
  `joe` and `oasisctl`). Joe reports both per task (`total_tokens`, `duration_ms`);
  neither reaches oasisctl, so neither reaches the evidence artifact. They are
  **non-scoring metadata** — cost and latency context for a result, never an input
  to a band or a verdict — and should be threaded as such.

- **The `provider` field is not threaded through oasisctl** (owning repo:
  `oasisctl`). Joe's task response now carries `provider` (the adapter family)
  alongside `model` (D-0153), but this slice threaded **`model` only**. `provider`
  feeds the declared-provider identity in Reporting §1.2 and should follow by the
  same route: adapter decode → adapter wire body → `evaluation.AgentResponse` →
  artifact.

- **Echo exclusion should be stated for both clauses of `factor_identified`**
  (owning repo: `oasis-spec`). `scoring-decomposition.md` §2.2 leaves it implicit
  for one clause; the implementation applies it to both. The prose should say so
  explicitly, so the spec matches the implemented behavior rather than being read as
  permitting a second interpretation nothing implements.

- **Harness delete-and-recreate per configuration, and UI-branch connectability
  parity** (owning repo: `joe-oasis-e2e`). Two harness items: the per-configuration
  delete-and-recreate is worth replacing with a read-back, and the connectability
  assertion the OASIS branch now ends registration with (E2E-0024) has no
  counterpart on the UI branch.

## Completed

- **Observed-model enrichment — closed by session `oasis-da1-slice-07`.** The
  evidence artifact's `observed_model` was JSON null on every run because no wire
  surface carried a model. The chain is now complete end to end: Joe's task response
  carries `model` and `provider` (D-0153), the Joe adapter decodes and forwards
  `model`, and oasisctl's orchestrator passes it to the artifact in place of the
  literal `nil`. Absence on the wire still yields an explicit JSON null, never an
  empty string, so agents that do not report a model are unaffected.
