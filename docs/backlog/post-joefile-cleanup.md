Backlog — Deeper register_source/D-0021 doc prose drift (beyond the tool-name correction)
Status: open

Surfaced by the `post-joefile-cleanup` sweep. Part B of that sweep was scoped to a
**name-only** correction of the renamed-away `register_source` → `register_component`
tool in the human docs (landed: [docs/security-in-layers.md](../security-in-layers.md)
and [docs/joe-architecture.md](../joe-architecture.md)). While correcting the names,
the surrounding prose was found to also describe **behavior that no longer matches
the live tool**. That semantic drift was deliberately NOT rewritten in-place (per the
sweep's "correct the name, do not silently rewrite the semantics" rule) and is parked
here for a separate decision.

## The drift (name corrected, behavior still stale)

1. **Tier/mutability misclassification — `docs/security-in-layers.md`.**
   The "Core Agent Tools" table (~line 98) lists the tool with **`Can Mutate? = YES`**
   and the Action-Safety tier table (~line 151) places it under **`T2 — Record`**
   (a mutate-class action). The live tool is classified **`ActionRead`** with
   `NeedsDurability: true` ([internal/safety/tier.go:201](../../internal/safety/tier.go:201)),
   and a governance break-test pins that it must NEVER be reclassified to Mutate
   ([internal/coreagent/register_component_governance_test.go:95](../../internal/coreagent/register_component_governance_test.go:95)).
   The doc's "YES / T2" framing contradicts the as-built classification (recording a
   discovered component to Joe's own store is a Read that merely declares durability,
   not a managed-system mutation — see the comment at
   [internal/coreagent/agent.go:488](../../internal/coreagent/agent.go:488)).

2. **Stale parameter signature + sibling-tool block — `docs/joe-architecture.md`.**
   The "Source Tools" ASCII block (~line 855) shows
   `register_component(type, url, name, env, ...) → store source`, but the live tool's
   required parameters are `name, type, config`
   ([internal/coreagent/agent.go:422](../../internal/coreagent/agent.go:422)) — there is
   no `url`/`env`. The same block still names `update_source` and `list_sources`
   (the latter renamed to `list_components` under D-0021) and points at a
   `internal/tools/sources/` path; this is broader D-0021 rename drift, not just the
   one tool.

3. **Adjacent `.joe/`-removal drift (related, out of this sweep's Part B).**
   `docs/security-in-layers.md` line 101 still says the LLM calls these tools "during
   onboarding and **.joe/ file processing**" and line 108 documents a "`.joe/` file
   processing" mutation path with `joefile_service.go`. The `.joe/` ingestion path was
   deleted in the `joefile-removal` session (commit `0c9e741`), so this prose is also
   stale. Noted here for completeness since it sits in the same table.

## Open work (the decision)

Decide whether `docs/security-in-layers.md` and `docs/joe-architecture.md` should be
brought fully in line with the as-built register_component tool and the D-0021 rename
(re-tier it to a Read/T1-with-durability framing, fix the parameter signature, rename
the sibling `list_sources`→`list_components` and the `internal/tools/sources/` pointer,
and scrub the residual `.joe/` references) — or whether these layered-security/architecture
docs are treated as point-in-time design records that are allowed to lag the code. The
sweep corrected only the tool **name**; the semantics above remain a deliberate, recorded
deferral.

## Evidence

Live tool: [internal/coreagent/agent.go:401-441](../../internal/coreagent/agent.go:401)
(name `register_component`, params `name/type/config`); classification
[internal/safety/tier.go:201](../../internal/safety/tier.go:201) and the
no-reclassify break-test
[internal/coreagent/register_component_governance_test.go](../../internal/coreagent/register_component_governance_test.go).
The source→component rename is **D-0021**; the `.joe/` removal is the `joefile-removal`
session (commit `0c9e741`). The name-only correction is the `post-joefile-cleanup`
sweep (this session).
