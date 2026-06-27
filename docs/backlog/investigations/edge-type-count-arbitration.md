# Edge-type count arbitration

> **Status: OPEN** — live finding; feeds the open `edge-type-literal-consolidation` backlog item.

**Session:** edge-type-count-arbitration — read-only investigation, no code or docs modified.

## Verified answer

**24 distinct edge types are defined in the code**, across **two** locations (not one).

The "19 vs 20" dispute is itself a symptom of looking at only one file. Resolving it directly:

- **19 is correct** for `internal/graph/relations.go` — it defines exactly **19** named edge-type constants, all distinct, no aliases.
- **20 is wrong and unbacked** — it is stale prose in `JOE_PROJECT_KNOWLEDGE.md:568`; no 20th constant exists and no enumeration anywhere lists 20 values. (Details in "Explaining 19 vs 20" below.)
- **Neither 19 nor 20 is the true count of distinct edge types**, because `relations.go` is not the only place edge types are defined. **5 additional edge types** exist only as inline string literals at their emit sites and are never registered as constants. 19 + 5 = **24**.

### The full enumeration (evidence)

**Location 1 — named constants in `internal/graph/relations.go` (19):**

| # | Value | File:line |
|---|-------|-----------|
| 1 | `metrics_in` | [relations.go:5](../../../internal/graph/relations.go:5) |
| 2 | `logs_in` | [relations.go:6](../../../internal/graph/relations.go:6) |
| 3 | `traces_in` | [relations.go:7](../../../internal/graph/relations.go:7) |
| 4 | `alerts_in` | [relations.go:8](../../../internal/graph/relations.go:8) |
| 5 | `paged_via` | [relations.go:9](../../../internal/graph/relations.go:9) |
| 6 | `dashboard_in` | [relations.go:10](../../../internal/graph/relations.go:10) |
| 7 | `is_k8s_node` | [relations.go:11](../../../internal/graph/relations.go:11) |
| 8 | `stores_in` | [relations.go:14](../../../internal/graph/relations.go:14) |
| 9 | `queues_in` | [relations.go:15](../../../internal/graph/relations.go:15) |
| 10 | `managed_by` | [relations.go:18](../../../internal/graph/relations.go:18) |
| 11 | `provisions` | [relations.go:19](../../../internal/graph/relations.go:19) |
| 12 | `ingress_for` | [relations.go:22](../../../internal/graph/relations.go:22) |
| 13 | `proxies` | [relations.go:23](../../../internal/graph/relations.go:23) |
| 14 | `mesh_for` | [relations.go:24](../../../internal/graph/relations.go:24) |
| 15 | `policy_enforces` | [relations.go:27](../../../internal/graph/relations.go:27) |
| 16 | `scaled_by` | [relations.go:30](../../../internal/graph/relations.go:30) |
| 17 | `secures` | [relations.go:31](../../../internal/graph/relations.go:31) |
| 18 | `image_stored_in` | [relations.go:34](../../../internal/graph/relations.go:34) |
| 19 | `publishes_to` | [relations.go:35](../../../internal/graph/relations.go:35) |

**Location 2 — inline string literals at emit sites in `internal/coreagent/` (5), never declared as constants:**

| # | Value | File:line(s) |
|---|-------|--------------|
| 20 | `contains` | [k8s_refresh.go:130](../../../internal/coreagent/k8s_refresh.go:130) |
| 21 | `routes_to` | [k8s_refresh.go:151](../../../internal/coreagent/k8s_refresh.go:151) |
| 22 | `references` | [k8s_refresh.go:167](../../../internal/coreagent/k8s_refresh.go:167), [:178](../../../internal/coreagent/k8s_refresh.go:178) |
| 23 | `in_vnet` | [azure_refresh.go:80](../../../internal/coreagent/azure_refresh.go:80), [:121](../../../internal/coreagent/azure_refresh.go:121), [:162](../../../internal/coreagent/azure_refresh.go:162) |
| 24 | `in_vpc` | [aws_refresh.go:94](../../../internal/coreagent/aws_refresh.go:94), [:143](../../../internal/coreagent/aws_refresh.go:143), [:193](../../../internal/coreagent/aws_refresh.go:193) |

These 5 are genuine edge types, not throwaway strings: every one is assigned to `graphdelta.Edge.Relation` and persisted to the `graph_edges.relation` column by the same upsert path as the constants ([graphdelta.go:128](../../../internal/coreagent/graphdelta.go:128)).

## Why these are all the locations

- **The schema does not constrain edge types.** `graph_edges.relation` is `relation TEXT NOT NULL` with no `CHECK`/enum ([002_graph.up.sql:18](../../../internal/store/migrations/002_graph.up.sql:18)). There is therefore no canonical schema-level registry; the set of edge types is whatever the emit sites write. So edge types must be counted at emit sites, and `relations.go` is only the *named* subset.
- **Exhaustive search for emit sites.** `grep` for `Relation:\s*"…"` across `internal/**.go` (excluding tests) returns exactly the 5 literals above and nothing else; `grep` for `graph.Relation…` symbol uses returns only references to the 19 constants. A sweep for other assignment shapes (`rel := "…"`, `AddEdge("…")`, `Edge{…"…"}`) found no further edge literals (the only `rel="next"` hits are HTTP `Link`-header parsing in [oci.go:318](../../../internal/adapters/registry/oci/oci.go:318), unrelated to the graph).
- **Doc-only edge names that are NOT in code.** `docs/reference/joe-architecture.md` tables mention `has_sg`, `has_nodegroup`, `has_nodepool`, `has_pe`, `targets_service`, `depends_on`, `calls`, etc. None of these are emitted anywhere in the Go code — they are illustrative/aspirational and were correctly excluded from both counts and from the 24.

## Explaining 19 vs 20 concretely

**There is no 20th constant.** The "20" is not a count of any enumerated set — it is a stale prose figure with nothing behind it:

- The only live source of "20 edge types" is `JOE_PROJECT_KNOWLEDGE.md:568` — *"Graph store is SQLite-backed (20 edge types) — no Cayley."* It is unaccompanied by any list.
- `relations.go` has **never** held 20 constants. Its full git history is: phase 6.1 created it, phase 6.12 left it at **17** constants, and phase 6.13 (commit `39995ed`, the file's last change, 2026-02-21) added `image_stored_in` + `publishes_to` to reach **19**. It went 17 → 19; it was never 20 and nothing was removed. So "20" cannot be explained by a since-deleted or renamed constant.
- The same repo's own enumerations agree on 19 and contradict the "20" prose: `docs/reference/joe-architecture.md:2355` lists exactly these 19 values and calls them *"all 19 types"*, and the completed-milestones log says *"19 graph relation types in `internal/graph/relations.go`"* and lists the same 19.
- `CLAUDE.md:12` likewise says *"(19 edge types)"*.

So the 19↔20 gap is **off-by-one documentation drift in `JOE_PROJECT_KNOWLEDGE.md`**, not a real disagreement about a specific edge value. The investigation that "counted 19 against `relations.go`, verified inclusively" counted the file correctly. The "20" report simply repeated the stale JPK prose; the prior coherence audit had already flagged that "20" as *"unverified against code"* ([docs/coherence-audit.md:32](../coherence-audit.md)), and the JPK-migration triage flagged it as staleness to fix at the source ([docs/backlog/investigations/jpk-migration-triage.md:112](jpk-migration-triage.md)).

**The substantive miss shared by both counts** is not the phantom 20th constant — it is that **both stopped at `relations.go`** and never looked at the literal emit sites. The five edge types `contains`, `routes_to`, `references`, `in_vnet`, and `in_vpc` are the values that distinguish "edge types named in `relations.go`" (19) from "edge types actually defined in the codebase" (24).

## Bottom line

- Count of **named edge-type constants** in `internal/graph/relations.go`: **19** (this is what both docs were trying to state; the one that said "19" was right).
- Count of **distinct edge types defined anywhere in the code** (constants + inline literals, the count that matches what the graph actually stores): **24**.
- The figure **"20" corresponds to no enumerable set** and should be treated as stale drift in `JOE_PROJECT_KNOWLEDGE.md`, not as a third candidate.
