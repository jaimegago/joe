Backlog — Residual `joe-architecture.md` architectural drift (D-0021 rename + removed `.joe/` ingestion)
Status: done — closed by the `arch-doc-staleness` session, which took "the decision" below (reconcile fully) and rewrote `docs/joe-architecture.md` end-to-end: the "Source Tools" block and `update_source` are gone (now `register_component`/`list_components`), the `.joe/` ingestion subsystem and `joe_file_cache` schema are scrubbed, and the Phase-3 historical checklist no longer names `sources`/`list_sources`/`joe_file_cache`. See [done/arch-doc-staleness.md](arch-doc-staleness.md).

Originally surfaced by the `post-joefile-cleanup` sweep and narrowed by the
`post-joefile-cleanup-02` documentation-truth sweep. That sweep corrected every
**verifiable, cleanly-mapped fact** of the deferred drift in both target docs:

- **RESOLVED — tier/mutability misclassification** ([docs/security-in-layers.md](../../reference/security-in-layers.md)).
  The "Core Agent Tools" table now lists `register_component` with `Can Mutate? = No`
  and the Action-Safety tier table places it under **T1 (read-class, records to Joe's
  own store, no managed-system mutation)** — matching the live `ActionRead` classification
  ([internal/safety/tier.go:201](../../internal/safety/tier.go:201)) and the no-reclassify
  break-test ([internal/coreagent/register_component_governance_test.go:95](../../internal/coreagent/register_component_governance_test.go:95)).
- **RESOLVED — parameter signature** ([docs/joe-architecture.md](../../reference/joe-architecture.md)).
  The "Source Tools" block now shows `register_component(name, type, config)`, matching the
  live required parameters ([internal/coreagent/agent.go:422](../../internal/coreagent/agent.go:422)).
- **RESOLVED — `list_sources` rename** ([docs/joe-architecture.md](../../reference/joe-architecture.md)).
  Renamed to `list_components` in the "Source Tools" block, matching the live tool
  ([internal/tools/core/listcomponents.go:26](../../internal/tools/core/listcomponents.go:26)).
- **RESOLVED — `.joe/` residue in the security doc** ([docs/security-in-layers.md](../../reference/security-in-layers.md)).
  Dropped the ".joe/ file processing" onboarding phrase, the ".joe/ file processing" graph-mutation
  table row, and the `joefile_service.go` key-file pointer (the `.joe/` ingestion path was deleted
  in the `joefile-removal` session, commit `0c9e741`).

## What remains open (deeper, flagged — not a name/signature/residue fix)

These were deliberately **not** rewritten because each requires reworking an architectural
description that no longer matches the system, not a mechanical correction — parked for a
separate decision (the same call the original backlog framed: reconcile fully, or treat these
as point-in-time design records allowed to lag the code).

1. **Removed/never-registered tool + nonexistent package path — `docs/joe-architecture.md`
   "Source Tools" block (~line 855).** The block header still points at
   `internal/tools/sources/` (no such package — the live tools are split across
   `internal/coreagent/` for `register_component` and `internal/tools/core/` for
   `list_components`), and it still lists `update_source(id, ...)`, a tool with **no live
   counterpart** (D-0021 renamed only `register_source`→`register_component` and
   `list_sources`→`list_components`; there is no `update_source`/`update_component` LLM tool
   anywhere in the live tree). Correcting these is a structural rewrite of the block, not a
   rename to a known live name.

2. **Pervasive `.joe/` ingestion architecture — `docs/joe-architecture.md`.** Beyond the
   localized security-doc residue (now scrubbed), `joe-architecture.md` documents the removed
   `.joe/` ingestion path as a **first-class subsystem** across many sections: onboarding /
   refresh flowcharts (~lines 57, 147, 499, 515, 522), a clarification example (~line 582), a
   `.joe/` Processing pseudo-code block citing `internal/discovery/joefile.go` (~lines 927,
   936–948), the `joe_file_cache` DB-schema rows (~lines 1699–1702), a `discovery.go # .joe/
   processing` file-tree comment (~line 2022), and the Phase-3 completion checklist listing the
   `sources` table + `joe_file_cache` (~line 2297). The `.joe/` path was deleted in the
   `joefile-removal` session (commit `0c9e741`, plus migration 029 dropping the cache), so all
   of this is stale — but scrubbing it coherently means rewriting onboarding flows, the
   discovery architecture, and the DB-schema section, which is out of scope for a residue scrub.

3. **`list_sources` in the historical Phase-3 checklist — `docs/joe-architecture.md`
   (~line 2301).** A stale `list_sources` name sits inside a point-in-time "Phase 3 ✅ COMPLETE"
   record, adjacent to the likewise-stale `sources` table and `joe_file_cache` entries. Left as
   part of the historical-record question in (2) rather than edited in isolation.

## The decision

Decide whether `docs/joe-architecture.md` should be brought fully in line with the as-built
system (restructure the "Source Tools" block to the real two-tool, two-package layout and drop
`update_source`; scrub the `.joe/` ingestion subsystem documentation and the `joe_file_cache`
schema; rename the Phase-3 checklist `list_sources`) — or whether this architecture doc is
treated as a point-in-time design record allowed to lag the code.

## Evidence

Live source-family tools: `register_component`
([internal/coreagent/agent.go:414](../../internal/coreagent/agent.go:414), params
`name/type/config` at [:422](../../internal/coreagent/agent.go:422)) and `list_components`
([internal/tools/core/listcomponents.go:26](../../internal/tools/core/listcomponents.go:26)) —
no `update_*` source tool exists. Classification:
[internal/safety/tier.go:201](../../internal/safety/tier.go:201) (`ActionRead` +
`NeedsDurability`) with the no-reclassify break-test
[internal/coreagent/register_component_governance_test.go](../../internal/coreagent/register_component_governance_test.go).
The source→component rename is **D-0021**; the `.joe/` removal is the `joefile-removal` session
(commit `0c9e741`, migration 029). The name/signature/classification/security-residue corrections
are the `post-joefile-cleanup-02` sweep.
