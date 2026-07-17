Component-delete graph-orphans — deferred residue (cross-component edges, write-only Edge.ComponentID, refresher UI visibility, other FK-less component_id tables)

Status: open

Deferred residue from session `component-delete-graph-orphans` / D-0117, which added the
transactional component-delete → graph_nodes cascade (edges follow by FK `ON DELETE
CASCADE`) and the one-time migration-032 orphan sweep. The items below were found while
implementing that and deliberately left out of its scope.

(a) **Cross-component edges are unmanaged by per-component delta reconcile.**
`edgesBetween` (`internal/graph/sqlite.go`, behind `ListEdgesForNodes`) filters
`WHERE from_node IN (…) AND to_node IN (…)` — **both** endpoints must be in the loaded
node set. `LoadGraphStateForComponent` (`internal/coreagent/graphdelta.go`) loads one
component's nodes only, so a cross-component edge never appears in either side's existing
set, never enters `EdgesToDelete` in `BuildGraphDelta`, and is only ever removed by
**endpoint-node death** (the FK cascade — which D-0117 now reliably triggers on component
delete). It is therefore **never reconciled on a relationship *change*** while both
endpoints stay alive: if the underlying cross-component relationship goes away but neither
node is deleted, the stale edge persists. A cross-component reconcile pass (or an explicit
edge-ownership model) is the real fix; deferred.

(b) **`graph.Edge.ComponentID` is a write-only field, never persisted.** `AddEdge`
(`internal/graph/sqlite.go`) omits it from the INSERT column list, `graph_edges` has no
`component_id` column (migration 002), and `scanEdges` cannot populate it — so the field on
`graph.Edge` (`internal/graph/store.go`) is set by callers and silently dropped. It is a
trap for a reader assuming edge-level component scoping exists, and it is exactly why the
D-0117 cleanup rides the endpoint FK cascade rather than any `component_id` predicate on
`graph_edges`. Either persist it (and reconcile on it) or delete the field; deferred.

(c) **Refresher visibility in the UI beyond per-component Last Sync.** D-0117 removed the two
client-side Refresh buttons on the grounds that per-component Last Sync already covers
freshness. A richer post-launch affordance — refresh cadence, per-component refresh status
(active / degraded / error), last-error surfacing — is deferred. This is a product/UX
decision, not a bug.

(d) **Other FK-less `component_id` tables orphan on component delete by the same mechanism.**
`onboarding_facts` and `action_ledger` each carry a `component_id` with **no foreign key to
`components`** (renamed, not FK'd, by migration 023), so deleting a component strands their
rows exactly as it stranded `graph_nodes` before D-0117. This session scoped the cascade to
the graph only; extending it (or adding FKs / a sweep) to these tables is out of scope and
deferred. `review_jobs` is the third such table but is already tracked separately in
`docs/backlog/review-jobs-orphaned-table.md` (it additionally has zero live Go references).
