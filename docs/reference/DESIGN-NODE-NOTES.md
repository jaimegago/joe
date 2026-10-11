# DESIGN — Node-anchored operational notes (Joe's memory surface)

Status: **design of record; nothing built.** Requirements R1–R16 and a five-slice
build order. The build item is `docs/backlog/node-anchored-notes.md`; the decisions
are D-0161 through D-0167 in `docs/project/DECISIONS.md`.

Source: the joe-pm design session `threads/node-anchored-notes.md` (2026-09-01),
whose report is the design record this file transcribes. That session was
clean-room: every fact it used arrived as a bounded, cited investigation report.
**Citations below are `file:line` into the tree at commit `a829e96`** and were not
re-derived for this filing; a slice that relies on one re-checks it first.

This document is at requirements altitude. It says what must be true, not how to
build it. Where it is silent on a mechanism, the mechanism is the slice's to choose.

## 1. The problem

Joe's graph is a deterministic snapshot of current topology. It answers what is
connected to what and nothing else — no change history, no memory of what a
previous session concluded about a node, no place for an operator to record "known
bad, ticket 123".

Knowledge store v1 was pruned (D-0113) for three reasons: no producer, an
embeddings dependency the native adapters never implemented, and an ungoverned
write path. It left one forward dependency open — where LLM-derived inferences go,
given D-0110 bars them from the graph (`done/knowledge-store-maturation.md` § 6,
relayed by `done/knowledge-store-prune.md`). `learn-from-sessions-fate` inherited
the same question. **This design answers it: they go in the notes table.**

Scope as given: human-authored docs (runbooks, wiki) are **out**; sources are live
infrastructure and repositories only; memory is **not** RAG.

## 2. Ground truth the design rests on

The design session found the tree differing from its starting snapshot in four
places. The corrections are load-bearing and are recorded so they are not
re-assumed.

- **Findings are not the substrate.** A finding is a human-authored cross-session
  annotation keyed on `agent_sessions(id)`, written only by an HTTP handler and
  never read back into a loop (`api/findings.go:82`,
  `findings/repository.go:16-21`, `011_findings_warnings.up.sql:13-14`). It carries
  no `component_id`. `joe_warnings` is the same shape and has no producer. Two
  dormant annotation tables already exist — the disease that killed v1.
- **The safety vocabulary is binary and the gate is a write floor.** `ActionRead`
  and `ActionMutate` only (D-0018/D-0019/D-0020; `safety/tier.go:8-27`). Unknown
  tools default to `ActionMutate` (`tier.go:248-257`).
- **The graph read path is not authorized per component.** The three graph read
  tools permit once against the pseudo-component `"graph"`
  (`access/graph.go:17,50,61`) and the SQL carries no component predicate. The
  open investigation `docs/backlog/investigations/agentic-path-rbac-read-enforcement.md`
  already logs the consequence.
- **The graph delta is not a change feed.** Every desired node is upserted
  unconditionally, so new and unchanged nodes are indistinguishable; changed
  entries carry only the after value; edges are deduped on three fields only; the
  delta is never persisted and no audit action exists for a graph mutation
  (`coreagent/graphdelta.go:11-16,43,53-57,102-105`). Refresh runs every five
  minutes per component. A vanished node is **hard-deleted** with no tombstone.
  **No change-history table exists anywhere in the schema.**

Two facts were confirmed rather than corrected: D-0110 holds structurally (the
three graph-write tools are parked because "nothing reconciles or removes what
they add", `coreagent/agent.go:163-181`); and the relation vocabulary is
convention, not constraint (twenty constants, free-TEXT column, no validator —
total over live data by construction, so any mapping over it needs a default).

## 3. Requirements

### 3.1 Storage and authorization

**R1 — Notes live in their own table, never in node metadata.** `AddNode` replaces
`metadata` wholesale (`graph/sqlite.go:75`), so a note stored there is destroyed by
the next refresh. Notes are many-per-node with lifecycles independent of the
node's, and refresh must never contend with a session on one row.

**R2 — A note exists only where authorization can exist.** `component_id` is
**required** on every note. `node_id` is nullable, with an `ON DELETE CASCADE`
foreign key matching the existing FK shape into `graph_nodes`. A note against an
unattributed node is **refused at the write path** — never written with a sentinel
— because an unattributed node cannot be authorized per component
(`graph/store.go:57`).

**R3 — Notes are authorized per component, through the existing chokepoint.**
`permit` (`access/access.go:159`) is the sole authorization point;
`ComponentBindings`' memoised per-component closure is the pattern. **The notes
surface is therefore strictly stricter than the graph surface that carries it, and
this is deliberate.** Topology is a fact about the estate; a note is judgment. The
asymmetry is a symptom of the carrier and is not repaired here.

### 3.2 Admissibility of the write

**R4 — A note-writing tool is `ActionRead`, and this is not a dodge.** D-0018's
definition of a write governs only managed-system mutations, and
`safety/tier.go:195-203` already states that tools which only record observed state
into Joe's own store are reads. The precedent is `register_component`: `ActionRead`,
writes Joe's own store, and commits its audit row in the write's transaction.

**R5 — Mandatory expiry is what makes R4 admissible.** The write floor is
boot-resolved, process-global, defaults to observation, and has no lowering path
(D-0073). An `ActionRead` write into Joe's own store therefore passes the floor
unconditionally, everywhere — which is exactly why the graph-write tools were
parked. **The discriminator is that a note decays and graph structure did not.**
Expiry is the reconciler the parked tools lacked. It is an **admissibility
property, not a retention policy**, and may not be made optional per author kind
except as R9 allows.

### 3.3 Producing notes from change

**R6 — The delta must express change before anything derives a note from it.**
`BuildGraphDelta` classifies added / changed-with-before-value / removed, for nodes
and edges, and stays pure (no context, no store). Without this, a hook in
`ApplyGraphDelta` emits one note per node per component every five minutes — noise,
not memory. What "changed" means for a node whose `metadata` is a free JSON blob is
settled inside that slice; a naive comparison marks every node changed on every
re-serialization.

### 3.4 Retrieval

**R7 — Traversal is filtered by intent: one parameter, four values.** `cause` (what
could have caused this), `impact` (what this breaks), `observe` (where I look at
this), `control` (what changes this). The map is over **`(relation, direction)`
pairs**, not relations, because propagation is directional and one edge means
opposite things walked each way. The map carries a default for unrecognised
relations. Direction is already modelled in the store
(`graph/sqlite.go:474,486`).

> **UNVERIFIED — the `(relation, direction) → intent` table.** The design session
> reasoned from relation *names* and never verified which way each edge points. The
> table must be built against the refresher construction sites, in slice 2. Nothing
> in this document settles a single row of it.

The four-class taxonomy of the starting snapshot (failure-propagating /
observation / management / discovery) is **overturned**: `discovery` is empty
under honest assignment, and class and direction are not independent axes.

**R8 — Notes are delivered in the tool payload and announced nowhere else.** The
task system prompt holds exactly one tool-steering paragraph by deliberate,
test-pinned design (`tools/core/resolvecomponent.go:99-105`,
`prompts/tasksystem_resolve_test.go:7-29`). The precedent points the other way and
is stronger: `resolve_component` attaches directives *into its payload* because
"prose in the prompt was ignored" (`resolvecomponent.go:36-57,222-233`).

### 3.5 Authorship and trust

**R9 — Three author kinds, always shown, with expiry graded by kind.**

- `refresher` — template-rendered from the classified delta of R6, never prose.
  Ages out by time and a per-node cap, and is **summarised in the payload, not
  listed**. A note about a removed node anchors on `component_id` alone with
  `node_id` NULL, so it survives the hard delete.
- `session` — the loop supplies a node id and short text and nothing else. Code
  validates the node id against the graph and rejects anything unregistered, then
  stamps session, principal, timestamp, confidence and expiry, and writes the audit
  row in the same transaction per R4. Re-confirmation by a later session resets the
  TTL.
- `operator` — human, via the UI. **The only kind permitted to outlive the default
  TTL**, and the only exemption R5 allows.

**R10 — The existing confidence vocabulary is reused.** `ConfidenceLevel`
(`graph/store.go:102,128-136`): refresher → `Explicit`, session → `Inferred`,
operator → `UserConfirmed`. The legacy ambiguity where `Explicit` and
`UserConfirmed` both shipped as `3` affects existing edge rows only; note rows start
clean. Author kind is kept alongside confidence because they answer different
questions: provenance and strength.

**R11 — Notes are probabilistic input. They never permit.** A note may narrow what
a future session does; it may never widen what a session is allowed to do. The
governance seam is unchanged and the graph stays structure-only, so D-0110 is
untouched — notes are not structure.

### 3.6 Payload shape and budget

**R12 — Notes ride at the head of the payload.** Over-budget tool results are
elided head-60% / tail-40% with the middle dropped (`agentloop/tokens.go:88-133`).
A notes section after an unbounded subgraph is the first thing to vanish. Head
placement is a requirement, not a preference.

**R13 — Capping `graph_related` is a prerequisite to putting anything else in it.**
`depth` is passed through unclamped and the recursive CTE carries no `LIMIT`
(`tools/core/graphrelated.go:55-64`, `graph/sqlite.go:203-222`); the 1–3 depth clamp
exists only on the MCP transport (`mcp/dispatcher.go:47-53`).

**R14 — The note budget is absolute, not a fraction of the turn.** The tool-result
cap is 25% of the turn budget, which resolves to ~16.5k tokens on the unknown-model
fallback (`agentloop/constants.go:48-50`, `llmusage/capabilities.go:26-61`). A
fractional budget gives the weakest deployment the smallest allowance. Bounds are
fixed counts — notes per node, characters per note, notes per payload — set against
the smallest configuration, and expressed in **characters**, because token
accounting is a chars/4 heuristic (`tokens.go:13-38`).

**R15 — `graph_related` returns a wrapper, not a naked subgraph.** It returns
`*graph.Subgraph` directly today (`graphrelated.go:65`). Carrying notes is a
**breaking change to the tool's result contract**, called out so it is planned
rather than discovered.

**R16 — Delivery is not storage.** Tool results do not survive the turn: only
`user` and `assistant` rows persist (`api/tasks.go:757-783,952-968`). The notes table
is the sole durable half. A withheld note is withheld per read, with no cached copy
to leak later.

## 4. Slice order

Each slice is independently mergeable. **Slices 1 and 2 produce no notes; they are
prerequisites**, and are stated as such so neither is mistaken for a stalled
feature.

1. **Classify the delta.** `BuildGraphDelta` gains added / changed-with-before /
   removed, staying pure. Settles metadata-blob comparison (R6). Ships no notes.
2. **Cap and shape `graph_related`.** Bounds per R13/R14; intent filter per R7,
   including building and verifying the `(relation, direction)` table; wrapper
   result per R15. Ships no notes.
3. **The notes table and the refresher producer.** R1, R2, R9's refresher kind,
   R10; the `ApplyGraphDelta` hook over the classified delta; summarised in the
   payload per R9.
4. **The session producer.** The `ActionRead` tool per R4, node-id validation,
   audit row in-transaction, TTL and re-confirmation per R5/R9.
5. **The operator producer.** UI write path, and the TTL exemption of R9.

## 5. Not settled by the design

Carried forward from the design session as it left them.

- **The `(relation, direction) → intent` table itself.** It must be built against
  the refresher construction sites, because the session reasoned from relation
  names and never verified which way each edge points. **UNVERIFIED**, assigned to
  slice 2.
- **Exact numeric bounds for R14.** Assigned to slice 2.
- **What "changed" means for a metadata blob.** Assigned to slice 1.
- **Whether the graph read path should become per-component.** Out of scope; it
  belongs to `docs/backlog/investigations/agentic-path-rbac-read-enforcement.md`.
  If it is ever done, the asymmetry of R3 disappears on its own.

## 6. Out of scope, deliberately

- `joe_warnings` and findings. This design does not consume or extend either.
  Whether they should be closed the way knowledge v1 was is a separate question.
- `Path` on the graph store is dead code (no non-test callers, absent from the
  RBAC accessor surface). R7 does not resurrect it.
- `Edge.ComponentID` is never persisted (`graph/store.go:90-99`,
  `graph/sqlite.go:97`). Unrelated to this design; tracked in
  `component-delete-graph-orphans`.
- The legacy migration-001 `sessions` / `session_messages` tables. This design reads
  neither; their disposition is `legacy-sessions-tables-fate`.

## 7. Provenance of the calls

The maintainer settled the design session's first three questions and delegated the
remainder after the fourth ("either explain it simply or decide"). Questions four and
five, and the ratification of the starting snapshot's decisions Two, Six and Seven,
were decided by that session on the reasoning above, not by the maintainer. The
design thread marks which line is a maintainer ruling and which is an agent's call.
