# Backlog — False or unguarded claims in the security authority

Status: done — both threads discharged by session `security-authority-claims` (D-0127)

Split out of [`reference-docs-prune-reconcile`](../reference-docs-prune-reconcile.md) by session
`security-authority-claims`. The two threads below are **claims in the security authority
(`docs/reference/security-in-layers.md`) that are false or unguarded**; the threads left on the
original file are documentation cleanup — a tense-and-subject sweep, a light public-page
revision, and stale citations in two other backlog files. One `INDEX.md` row cannot represent
both at their true priority: the cleanup is `next`, a false safety claim in the document that
other documents defer to is `now`. Splitting is what lets each carry its own.

## 1 — The unconditional-denial property is unpinned

The load-bearing correction made by `reference-docs-prune-reconcile` is that **no registered
Mutate tool's `PolicyKey` resolves to a live `ActPolicy` field**, so `CheckAccess`'s allow branch
is unreachable and every registered mutation is denied regardless of configuration. Nothing pins
this.

`TestCheckAccess_MutateDefaultDeny` pins denial under `DefaultPolicy()`, which is the weaker
claim — it would still pass if a key were grantable but simply defaulted off. The test that
exercised the allow branch (`TestCheckAccess_MutateEnabled`) was deleted with
`publish_doc_update_git` rather than migrated, and the explanatory comment left in
`internal/safety/tier_test.go` is the only record of the property.

A structural test would assert the disjointness directly: for every `ActionMutate` row in
`toolRegistry`, `IsT3Allowed(row.PolicyKey)` is false under a policy with **every** `act`
toggle enabled. That fails the moment someone ships a tool with a live key — which is exactly
the event that should force this doc claim to be revisited. This is the same shape as the gaps
in [`tool-class-break-tests`](../tool-class-break-tests.md).

Note this is a *documentation-integrity* test, not a safety regression test: the property it
pins is an accident of what was deleted, not a designed invariant. It should be written so its
failure reads as "the docs now overstate the denial," not as "safety broke."

## 2 — `security-in-layers.md` overstates the parked graph-write tools in two places

Re-derived in `reference-docs-prune-reconcile-03`. `graph_add_node`, `graph_add_edge`, and
`graph_update_node` are **parked**: their only registration site is commented out at
`internal/coreagent/agent.go:182-184`, no other registry registers them (nothing in
`internal/tools/` or `internal/mcp/`), and the absence is pinned by `TestGraphWriteToolsAreParked`
(`internal/coreagent/registry_graph_write_parked_test.go:33`). Their constructors,
implementations, and `ActionRead` rows (`internal/safety/tier.go:188-190`) all survive. So the
tools exist and are Read-classed, but **no surface can invoke them**.

Three places in the doc state this correctly and need no change: the model-maintenance table row
(`:77`), the paragraph below it (`:80`, which `-02` also converted to present tense), and §3.6
(`:232`, "the parked `graph_add_*`").

Two places present them as live with no parked caveat:

- **§3.1 classification table, Examples column (`:141`)** — lists `graph_add_node` among
  `git_log`, `k8s_get`, `graph_query`, `register_component`, all of which *are* registered. A
  reader takes the whole list as the live Read surface. Either drop it from the examples or mark
  it parked inline.
- **§8.2 protected-configuration table (`:463`)** — the `graph_*` row claims LLM tools can
  **write** those tables (`✅`), attributed to "Model-maintenance tools (Read class)". That is
  false as written: the three graph writers are the parked ones, `graph_query` is read-only, and
  a grep for a graph-writing call in any live tool (`internal/tools/`, non-test) returns nothing.
  No LLM-invokable tool writes graph tables today. The row also contradicts `:232` in the same
  document.

The fix is a present-tense statement of what these tools actually are, consistent across all
five sites. Note the §8.2 correction is the load-bearing one — it currently overstates the
LLM's reach over Joe's own graph, which is the opposite direction from the rest of the doc's
claims and the one a security reader is most likely to rely on.

## Resolution — both threads discharged (D-0127)

**Thread 1.** `TestRegisteredMutatesAreUngrantable`
(`internal/safety/tier_mutate_ungrantable_test.go`) asserts that for every `ActionMutate` row
in `toolRegistry`, `IsT3Allowed` on that row's `PolicyKey` is false and `CheckAccess` denies,
under a policy with **every** `act` toggle enabled. Both sets are derived structurally — the
Mutate set from `toolRegistry`, the toggles by reflection over `ActPolicy` — so neither is a
hand-kept list that the change it guards against would have to edit. The guard was shown to
fail on the property it pins: a temporary `case "github_comment"` arm in `IsT3Allowed` failed
it on both assertions while `TestCheckAccess_MutateDefaultDeny` continued to pass, which is
the coverage gap stated above, demonstrated rather than argued. The test carries a note that
its failure means the docs overstate the denial, not that safety broke.

**Thread 2.** §3.1's Examples column now marks `graph_add_node` parked and cross-references
§3.6. §8.2's `graph_*` row now reads LLM-read `✅`, LLM-write `❌ — no registered tool writes
them`, with the mutating surface named (deterministic refreshers through the delta-reconcile
seam; the D-0117 component-delete cascade), plus a paragraph below the table stating the
mechanism: nothing architecturally forbids a graph-writing tool, the three that exist are
parked and pinned, and the registered graph tools read only. `joe-architecture.md:177` already
stated the park correctly, so the two reference docs agree without editing it.

`docs/project/SITE-CLAIMS.md`'s unconditional-denial entry drops its UNPINNED state per D-0077,
names the new test, and is repointed off `reference-docs-prune-reconcile.md`.
