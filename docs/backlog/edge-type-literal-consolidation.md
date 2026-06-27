Edge-type literal consolidation and constraining graph_edges.relation
Status: open

Rehomes the edge-type smell established by the edge-type-count arbitration
(evidence: [docs/backlog/investigations/edge-type-count-arbitration.md](investigations/edge-type-count-arbitration.md)).
This is a code-hygiene item, not a correctness bug — the graph behaves correctly
today.

## The smell

`internal/graph/relations.go` declares **19** named edge-type constants — the
authoritative *named* set. But the true set of edge types the graph actually
stores is larger: **5 additional edge types are emitted only as inline string
literals** at their emit sites in `internal/coreagent/`, never declared as
constants and bypassing the constant set entirely:

| Value | Emit site |
|-------|-----------|
| `contains` | [internal/coreagent/k8s_refresh.go:130](../../internal/coreagent/k8s_refresh.go:130) |
| `routes_to` | [internal/coreagent/k8s_refresh.go:151](../../internal/coreagent/k8s_refresh.go:151) |
| `references` | [internal/coreagent/k8s_refresh.go:167](../../internal/coreagent/k8s_refresh.go:167), `:178` |
| `in_vnet` | [internal/coreagent/azure_refresh.go:80](../../internal/coreagent/azure_refresh.go:80), `:121`, `:162` |
| `in_vpc` | [internal/coreagent/aws_refresh.go:94](../../internal/coreagent/aws_refresh.go:94), `:143`, `:193` |

These persist through the **free-form `TEXT` `graph_edges.relation` column, which
has no `CHECK` constraint** ([internal/store/migrations/002_graph.up.sql:18](../../internal/store/migrations/002_graph.up.sql:18)),
via the same upsert path as the constants
([internal/coreagent/graphdelta.go:128](../../internal/coreagent/graphdelta.go:128)).
Because the column is unconstrained, the emit sites — not the constant set — are
the only real registry of edge types, and the literals are invisible to anyone
reading `relations.go`. The 19-vs-20 documentation dispute that triggered the
arbitration was a direct symptom of this: counting only `relations.go` misses the
5 literals (19 + 5 = 24 distinct edge types defined in code).

## Open work (the decision)

Decide whether to **consolidate the 5 inline literals into declared constants in
`internal/graph/relations.go`** so the constant set is the single registry, and
**whether to constrain `graph_edges.relation`** (a `CHECK` against the named set,
or an enforced lookup) so new edge types cannot enter as ad-hoc literals again.

Trade-off to weigh: a `CHECK`/enum trades the current schema flexibility (any
emit site can mint a relation) for a guarantee that the constant set stays
authoritative. If consolidation lands without a constraint, the literals could
silently reappear, so the two halves are best decided together.

## Evidence

Full enumeration, exhaustive emit-site search, and the 19-vs-20-vs-24 resolution:
[docs/backlog/investigations/edge-type-count-arbitration.md](investigations/edge-type-count-arbitration.md).
The structural-not-count framing of these edge types in CLAUDE.md is recorded as
**D-0032** in [docs/project/DECISIONS.md](../project/DECISIONS.md).
