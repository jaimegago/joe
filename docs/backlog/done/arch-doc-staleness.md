# Arch-doc staleness — full rewrite of joe-architecture.md and security-in-layers.md against the live tree

Status: done — both docs audited and fully rewritten to the as-built system in the `arch-doc-staleness` session; CLAUDE.md's surviving T1/T2/T3 references corrected in the same pass; the superseded `post-joefile-cleanup` item is closed by this rewrite.

## Context

`docs/joe-architecture.md` and `docs/security-in-layers.md` are operator-authored, public-facing-credibility documents. A Phase-1 audit found both were stale at the foundation, not at the margins: the two-binary `joe`/`joecored` split, the two-agent HTTP boundary, the three-tier T1/T2/T3 action model, `.joe/` ingestion, `~/.joe/panic.state`, OpenAI/Ollama providers, pre-rename "source" terminology, and several **claimed-but-unbuilt** safety mechanisms. The operator chose a full end-to-end rewrite over a surgical patch or a separate-session deferral.

No unverified claims were introduced. Mechanisms that exist only as design (circuit breaker, environment-level blocking, credential isolation, the `joe-security` remote mode) are now explicitly framed as not-yet-built.

## STALE claims found and corrected

### Action classification — three-tier → binary (D-0020)
- **Was:** T1 Observe / T2 Record / T3 Act throughout both docs (security `§3.1`, `Part 1/2/5`, `§3.7`; arch "Action Tiers", Review-Agent tools, Implementation Phases). CLAUDE.md line 15 and line 64.
- **Now:** binary `ActionRead` / `ActionMutate`. Reads always run; Mutates denied by default, per-action opt-in via the `act` policy. The Record band is gone — model-maintenance tools are Reads.
- **Verified:** `internal/safety/tier.go:14-27` (axis), `:283-311` (binary `CheckAccess`), `:192-203` (model-maintenance tools registered `ActionRead`); D-0020 (`docs/DECISIONS.md:1407`).

### Read authorization / posture (D-0041, D-0043)
- **Was:** zones presented as the uniform gate for all reads (security `§8.1`); arch doc had no read-posture concept.
- **Now:** install-wide read posture — `team_flat` launch default (any authenticated principal reads any component) vs `zoned` (grant-based full-mode). Governs human-facing transport reads only; the `agent:core` autonomous read surface is governed by `auto_promote_read` + grants, separated at engine construction. Orthogonal to the write floor; flipped by an audited admin act.
- **Verified:** `internal/rbac/policy.go:62-80`; engine sites `cmd/joe/server.go:856` (transport, with-governance) vs `:722` (refresh, promote-only, posture-nil); D-0041 (`docs/DECISIONS.md:160`), D-0043 (`docs/DECISIONS.md:56`).

### RBAC characterization — role-based → zone-scoped
- **Was:** `rbac_policies` labeled "User/group permissions" (security `§8.2`); role/group framing.
- **Now:** zone-scoped access control — zones carry an action ceiling, components are assigned to zones, grants are action-less. No roles, no groups.
- **Verified:** no role/group symbols in `internal/rbac/policy.go` / `zones.go`; action vocabulary `internal/rbac/zones.go:11-32` (`read`/`query`/`mutate`/`delete` + `declare_incident`/`resolve_incident`); default zones seeded `internal/store/migrations/006_rbac.up.sql:31-34`.

### Write floor (D-0018)
- **Was:** "safe mode = T1 (Observe) only", framed via the panic.state file (security `§7.3`).
- **Now:** boot-resolved, runtime-immutable write floor; reasons `observation` (`JOE_MODE`) and `safe_mode` (sticky panic at boot); recovery is a restart.
- **Verified:** `internal/safety/floor.go:22-54`, reasons `:13-20`.

### Denial precedence (D-0022)
- **Was:** absent from both docs.
- **Now:** floor > incident > RBAC, enforced by check order; the incident half is the §C captain gate.
- **Verified:** `internal/tools/executor.go:201-219`; captain gate `internal/captaingate/captaingate.go`; D-0022 (`docs/DECISIONS.md:1226`).

### Panic state — file → DB row (D-0018)
- **Was:** "Write panic state to `~/.joe/panic.state`" and a panic.state YAML (security `§7.2`/`§7.3`); arch Phase-9 checklist "Panic state persistence (~/.joe/panic.state)".
- **Now:** single `cluster_panic_state` DB row (id=1); no panic.state file. Safe mode is the write floor raised at the next boot.
- **Verified:** `internal/store/panic_store.go:13-15`, `internal/safety/panic.go:23-33`.

### Two-binary `joecored` → single `joe` binary
- **Was:** "two binaries from day one" (`joe` Local + `joecored` Core) with an HTTP boundary; `cmd/joecored`, "joe↔joecored", joecored bind, directory tree (both docs, 30+ occurrences in the arch doc).
- **Now:** single `joe` process; both agent roles in-process sharing one tool-executor governance path; subcommands dispatch ahead of the server.
- **Verified:** only `cmd/joe` exists (`ls cmd/`); dispatcher `cmd/joe/main.go`, server `cmd/joe/server.go`; shared gate `internal/captaingate/`.

### `.joe/` ingestion removed (D-0042)
- **Was:** `.joe/` documented as a first-class subsystem (onboarding/refresh flows, Discovery Engine pseudo-code citing `internal/discovery/joefile.go`, `joe_file_cache` schema, clarification example) in the arch doc; residue in the security doc.
- **Now:** removed; the git-refresh path still builds a `git_repo` node from HEAD commit identity. Noted explicitly.
- **Verified:** D-0042 (`docs/DECISIONS.md:118`); migration `029_drop_joe_file_cache`.

### LLM providers — Claude/OpenAI/Ollama → claude + gemini
- **Was:** OpenAI/Ollama adapters and a four-model `config.yaml` (arch LLM-Adapter + Core-Services diagrams, config).
- **Now:** exactly two providers, `claude` and `gemini`; boot validates the active model's key.
- **Verified:** `internal/llmfactory/factory.go:10-30`; `internal/config/validation.go`.

### Terminology — source → component (D-0021)
- **Was:** `sources` entity/table, `/api/v1/sources`, `/api/v1/admin/source-zones`, `list_sources`, `update_source`, `sources` SQL table (both docs).
- **Now:** component entity, `/api/v1/components`, `/api/v1/admin/component-zones`, `list_components`, `register_component`; `graph_nodes.component_id`. (`knowledge_sources` legitimately keep "source".)
- **Verified:** `internal/safety/tier.go:81,201`; API surface `internal/api/server.go` (`/components`, `/component-zones`); D-0021.

### Claimed-but-unbuilt safety mechanisms — reframed as design/roadmap
- **Was:** environment-level operation blocking, mutation circuit breaker, credential isolation enforcement, and a `CanWriteTable`/`writeProtectedTables` guard "hardcoded in internal/safety/invariants.go" (security `§3.6`/`§3.7`/`§3.8`/`§8.2`); a `joe-security` remote hardened mode with its own `security.db` (security `§8.3`); the arch doc's "Deterministic Blast Radius Controls".
- **Now:** the executor's real checks (floor → zone/namespace scope → safety policy → notify) are documented as what's built; the circuit breaker / env-blocking / credential isolation are framed as designed-but-not-built (security `§3.7`, Part 5 "Still open"). The `CanWriteTable` guard is corrected to the actual architectural protection (admin-only REST surface; no LLM raw-SQL write path). The `joe-security` remote mode is marked not-built.
- **Verified:** `internal/tools/executor.go:188-299` (no breaker/env-block/cred-isolation); no `CanWriteTable`/`writeProtectedTables` anywhere (`grep`); no `cmd/joe-security`/`internal/securitysvc` (`ls`). Self-protection blocks `joe`/`kill`/`pkill`/`killall` (not `joecored`) — `internal/safety/invariants.go:110-139`.

## CLAUDE.md (incidental, corrected in the same pass)
- Line 15 T1/T2/T3 → binary Read/Mutate axis citing D-0020.
- Line 64 "safe mode blocks T2/T3 tools" → "safe mode raises the write floor, which blocks the Mutate class."

## Note
These are documentation corrections, not new decisions — no `docs/DECISIONS.md` entries were added. Where a correction reflects a recorded decision (D-0018/D-0020/D-0022/D-0041/D-0043, plus D-0021/D-0042/D-0019), the decision is cited in the doc.
