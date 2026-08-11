# Adapter construction is fragmented across divergent type-keyed paths

Status: done — one canonical constructor, `internal/adapters/factory.New`, with both former construction paths routed through it and coverage the union of their two type sets; merged as `d84ee19` (jaimegago/joe#27), thread `adapter-dispatch-consolidation`
Priority: now
Severity: latent correctness bug — type→adapter coverage gaps, not a safety issue

This item previously depended on the source→component rename; that rename landed
2026-06-08 (D-0021, migration 023), so this item is unblocked.

## Symptom

The type→adapter decision is duplicated across multiple sites with divergent type coverage, and one site's comment claiming to be the canonical mapping is false. A source/component whose type is handled by one path but not another can construct in one context and fall through to nil in another.

## Leads to verify (re-derive exact file:line from live code; do not trust these)

- A construction switch claiming to be "the single source of truth for the type→adapter mapping" — `newAdapterForType`, internal/api/components.go:132-183 (the citation formerly read internal/api/sources.go, a pre-D-0021 path). Returns nil for every type it does not name (registry/vendor/code-review types such as datadog/splunk/oci_registry/github fall through).
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

## Re-prioritized to now (component-audit-filing)

The fragmentation is no longer latent. A verified two-map split creates live dead windows for
registrable types, so the coverage gap this item describes is reachable by an operator following
the documented registration path, not merely a shape defect in the code.

`newAdapterForType` ([internal/api/components.go:132-183](../../internal/api/components.go#L132-L183))
is invoked at promotion through `connectAndRegisterAdapter`
([internal/api/components.go:199-216](../../internal/api/components.go#L199-L216)), which returns
nil when the constructor returns nil. It omits github, gitlab, datadog, and the four registry
types, so a promoted github or gitlab component gets a nil no-op and has no live adapter until
the next restart.

`connectSourcesDefault` ([cmd/joe/server.go:1157-1313](../../cmd/joe/server.go#L1157-L1313))
runs at boot and constructs only kubernetes, git, aws, azure, falco, datadog, splunk, dynatrace,
newrelic, github, and gitlab. It omits prometheus, mimir, loki, tempo, jaeger, alertmanager,
pagerduty, grafana, argocd, terraform, envoy, and all datastores, so components of those types
lose their adapter on every server restart and nothing reconstructs it until an admin manually
runs Test Connection per component.

Counted against the registrable set
([store.AllowedComponentTypes](../../internal/store/constants.go#L109-L131)), that is thirteen
registrable types carrying a dead window, in two shapes: **eleven restart-shaped** — prometheus,
mimir, loki, tempo, jaeger, alertmanager, pagerduty, grafana, argocd, terraform, envoy (present
in the runtime map, absent from the boot pass) — plus **two promotion-shaped** — github, gitlab
(present in the boot pass, absent from the runtime map). Splunk, dynatrace, and newrelic are
absent from `newAdapterForType` as well and present in the boot pass, so they share the
promotion-shaped window with github and gitlab; counted that way the promotion-shaped set is
five and the total sixteen. The composition is stated as thirteen-plus-three rather than as a
bare number precisely so this passage cannot drift stale against either map: whichever count a
reader prefers, the membership is spelled out and checkable against the two function bodies
cited above.

## Closed

Landed as `d84ee19` on `main` via `jaimegago/joe#27`. The body above is kept as
the historical statement of the problem; this section records what the fix
actually covered, because the acceptance criteria and the leads are not the same
list.

**Delivered.** `internal/adapters/factory.New(componentType) (adapters.Adapter, error)`
is the canonical constructor. `newAdapterForType` is deleted, and all three live
call sites route through the new one: `connectAndRegisterAdapter` (promotion
activation), `handleTestComponent` (Test Connection), and `connectSourcesDefault`
(the boot pass). The boot pass iterates the stored components instead of a
hand-maintained type list, so both shapes of dead window this item measured —
the promotion-shaped one for splunk/dynatrace/newrelic/github/gitlab and the
restart-shaped one for prometheus through envoy — are closed. A type with no
adapter is now `factory.ErrNoAdapter` rather than a silent nil: the promotion
path returns it, and Test Connection stopped answering `ok:true` with "has no
connection to test". Coverage is pinned by a guard that reads
`store.AllowedComponentTypes()` itself, so a new registrable type added without a
factory case fails at the seam.

**Deliberately not covered, and still open elsewhere:**

- The **artifact-registry types** (`oci_registry`, `dockerhub`, `artifactory`,
  `ecr`) stay out of the union. They are unregistrable under D-0058 precisely
  because they have no construction path, and wiring them needs a credential path
  first — still `docs/backlog/trim-deadonarrival-component-types.md`.
- The **query-time interface switch** (`internal/access/observe.go`) and the
  `knowledge.Source` syncer map were out of scope by this item's own criteria and
  were not touched.
- The **refresh type switch** (`internal/coreagent/refresh.go`), listed above as a
  lead, was left alone on inspection: it type-asserts an already-registered
  adapter and builds nothing, so it is routing rather than construction and folding
  it onto the constructor would not have been meaningful.

**Two findings this work surfaced rather than fixed**, both carried in
`threads/adapter-dispatch-consolidation.md`:

- The consolidation necessarily retired the identifier `newAdapterForType` that
  `TestPromote_NoResolution` and `TestCreateComponent_NoConnectProbe` both forbid
  by literal name. Both tests stay green and neither was edited, but that clause
  in each now matches nothing that could exist. It belongs to
  `docs/backlog/structural-guard-vacuity.md` as a third instance, and a different
  shape from the two already filed there.
- `make test-unit` iterates `go list ./internal/...` and never reaches `cmd/`, so
  no `cmd/joe` test runs in CI — including this work's boot-path break-test and
  the pre-existing files under that directory. `make vet` still compiles them.
  Unfiled as of this closure.
