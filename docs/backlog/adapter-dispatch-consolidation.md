# Adapter construction is fragmented across divergent type-keyed paths

Status: open
Depends on: source→component rename — LANDED 2026-06-08 (D-0021, migration 023). This is now unblocked.
Severity: latent correctness bug — type→adapter coverage gaps, not a safety issue

## Symptom

The type→adapter decision is duplicated across multiple sites with divergent type coverage, and one site's comment claiming to be the canonical mapping is false. A source/component whose type is handled by one path but not another can construct in one context and fall through to nil in another.

## Leads to verify (re-derive exact file:line from live code; do not trust these)

- A construction switch claiming to be "the single source of truth for the type→adapter mapping" — last seen near internal/api/sources.go:newAdapterForType. Reportedly covers ~21 types and returns nil for the rest (registry/vendor/code-review types such as datadog/splunk/oci_registry/github fall through).
- A separate hand-rolled boot path — last seen near cmd/joe/server.go:connectSourcesDefault — that does NOT call the above, instead doing per-type ListByType(...) + direct New() with a DIFFERENT type set (k8s, git, aws, azure, falco, datadog, splunk, dynatrace, newrelic, github, gitlab).
- A third type switch for refresh-handler routing — last seen near internal/coreagent/refresh.go.
- Query-time operation routing — last seen near internal/access/observe.go — keys on the Go adapter INTERFACE type, not the type string. This one is arguably fine; note it but the goal is not to force it onto the string switch.

- **Added by D-0119 (2026-07-19):** `handlePromoteComponent` is now a THIRD caller of
  `newAdapterForType`, via the `connectAndRegisterAdapter` helper it shares with
  `handleTestComponent`. This does not change the divergence between the API path and the
  boot path — it widens the API-path side. The consolidation this item asks for should fold
  all three call sites onto the one canonical constructor, not just the original two.

## The actual problem

Two construction paths (sources.go vs server.go) with non-identical type coverage means "which types can Joe actually connect" depends on which path ran. The "single source of truth" comment is contradicted by the boot path.

## Acceptance criteria

- One canonical type→adapter construction function. Both former construction paths (the API path and the boot path) route through it.
- Coverage is the UNION of the two former sets, verified against the full AllowedSourceTypes() const list — no valid type returns nil at construction.
- A valid type with no adapter is an explicit, logged, surfaced error, not a silent nil-fallthrough.
- The false "single source of truth" comment is removed or made true.
- Break-test: a valid type present in the const list but (previously) missing from one path now constructs successfully via both entry points.
- Out of scope: the query-time interface switch (observe.go) and the knowledge.Source model's syncer-by-type map (different table, different concept).

## Scoping note

This is the infra store.Source/Component model only. The knowledge.Source model (knowledge_sources table; human/confluence/notion/session) has its own type-dispatch and is unrelated — leave it alone.
