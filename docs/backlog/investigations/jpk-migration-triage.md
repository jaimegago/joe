# JPK Migration Triage — `JOE_PROJECT_KNOWLEDGE.md`

> **Status: OPEN** — live finding; JPK migration triage not yet fully actioned.

**Session:** `jpk-migration-triage` · **Date:** 2026-06-24 · **Mode:** read-only investigation (nothing modified or moved)

Purpose: before `JOE_PROJECT_KNOWLEDGE.md` (JPK) retires, check every section for content that lives **nowhere** in the three tracked spines — `CLAUDE.md`, `docs/project/DECISIONS.md`, `docs/backlog/` — and classify each as **already-captured**, **unique-migrate-worthy**, or **stale**.

---

## Gitignore status

**JPK is git-ignored intentionally, and has never been committed.**

- **Ignored:** `git check-ignore -v JOE_PROJECT_KNOWLEDGE.md` → `.gitignore:54:/JOE_PROJECT_KNOWLEDGE.md	JOE_PROJECT_KNOWLEDGE.md` — matched by an explicit, root-anchored rule.
- **Intentional, not incidental:** the rule at `.gitignore:54` carries the comment (`.gitignore:53`): *"# Personal project-knowledge doc (pinned as claude.ai Project knowledge, not for publish)"*. It is a deliberate, individually-named, commented entry — not a side effect of a broad glob.
- **Never tracked:** `git ls-files JOE_PROJECT_KNOWLEDGE.md` returns nothing and `git log -- JOE_PROJECT_KNOWLEDGE.md` is empty — the file has no history in the repo; it was authored and curated locally only.
- JPK's own header (`JOE_PROJECT_KNOWLEDGE.md:3-15`) corroborates: it describes itself as "git-ignored and so invisible in clones," pinned as claude.ai Project knowledge, with the in-repo copy as source of truth.

**Conclusion:** intentionally ignored as a personal/local knowledge artifact, deliberately kept out of the published tree.

---

## Compact triage table

| # | Section | Classification | Destination (if unique) |
|---|---------|----------------|-------------------------|
| 1 | Header / preamble (maintenance notes + D-0023→D-0030 changelog) | already-captured | — |
| 2 | §0 One-paragraph orientation | already-captured | — |
| 3 | §1 Current architecture (single binary) | already-captured | — |
| 4 | §1.1 What was removed (REPL, reverse-RPC, joe-core, review-agent) | already-captured | — (one doc-gap noted) |
| 5 | §2 Access paths that remain | already-captured | — |
| 6 | §3 Agentic runtime model | already-captured | — |
| 7 | §4 Auth/identity A–H safety chain | already-captured | — |
| 8 | §4.1 Identity Stages 1–5 | already-captured | — |
| 9 | §4.2 What genuinely remains | already-captured | — |
| 10 | §5 Skills system | already-captured | — |
| 11 | §6 Security architecture | already-captured | — |
| 12 | §6.1 Credential-provider abstraction | already-captured | — |
| 13 | §6.2 Component governance (registration/promotion) | already-captured | — |
| 14 | §6.3 Core Agent autonomous reads governed | already-captured | — |
| 15 | §7a Standard test commands | already-captured | — |
| 16 | §7b OASIS relationship + 9/21 score | unique-migrate-worthy (relationship) / stale (score) | backlog file |
| 17 | §8 Launch positioning & former-employer relationship | unique-migrate-worthy | backlog file (+ CLAUDE.md for Apache-2.0 posture) |
| 18 | §9 Stable invariants | already-captured | — (count drift in CLAUDE.md noted) |
| 19 | §10 Known stale claims to distrust | already-captured | — |
| 20 | §10 item 3 — provider list correction (Claude+Gemini only) | unique-migrate-worthy | CLAUDE.md |
| 21 | §11.0/11.1/11.3 Session subsystem as-built | unique-migrate-worthy | CLAUDE.md (high-level) |
| 22 | §11.2 Orphaned learn-from-sessions extractor | already-captured | — |
| 23 | §11.3 Doc-debt findings (stale comments / phantom reports) | unique-migrate-worthy | backlog file |

---

## Per-section evidence

### 1. Header / preamble — already-captured
The preamble (`JOE_PROJECT_KNOWLEDGE.md:1-91`) is dated meta-commentary plus a changelog of what landed D-0023→D-0030. Every delta bullet maps 1:1 to a logged decision: D-0023 (`docs/project/DECISIONS.md:636`), D-0024 (`:597`), D-0025 (`:535`), D-0026 (`:498`), D-0027 (`:418`), D-0028 (`:234`), D-0029 (`:141`), D-0030 (`:45`). The D-0031 signpost note matches `docs/project/DECISIONS.md:13`. The framing/dating/"this document wins" language is ephemeral and not migratable.

### 2. §0 Orientation — already-captured
Single `joe` binary, HTTP daemon on `:7777`, agentic loop, single LLM adapter, SQLite graph, safety/RBAC layer, binary read/mutate axis — all in `CLAUDE.md` "Project Identity" + "Architectural Invariants". (The "no REPL anymore" and "Claude+Gemini" corrections are tracked under rows 4 and 20.)

### 3. §1 Current architecture — already-captured
"Single `joe` binary; bare `joe` starts the server; subcommands dispatch ahead via `cmd/joe/main.go`; server entrypoint `cmd/joe/server.go`" is verbatim the first bullet of `CLAUDE.md` "Architectural Invariants" and the "Key Architecture" framing. Subcommand list matches `CLAUDE.md:51`.

### 4. §1.1 What was removed — already-captured (one documentation gap)
The *current-state* truth (single binary, in-process accessor, no REPL/no reverse-RPC) is captured in `CLAUDE.md`. The removal of the `joe review` subcommand is already reflected by its **absence** from the `CLAUDE.md:51` subcommand list. The removed-component enumeration itself is historical drift-correction — useful only while stale chats persist, not migrate-worthy.
**Noted gap (not migrate-worthy content, but a real observation):** JPK §1.1 (`:149-152`) flags that **no `D-00xx` entry records the joe-core→single-binary collapse**, and my grep confirms it — `grep -ni "joe-core\|single-binary\|review-agent" docs/project/DECISIONS.md` finds no decision recording either collapse. This is a documentation gap in DECISIONS.md, not lost knowledge; flag to Jaime if a retroactive entry is wanted.

### 5. §2 Access paths — already-captured
Web UI / Slack / MCP / REST access paths and the operator subcommands match `CLAUDE.md` conventions (`joe` subcommands line, MCP env vars, category observability API). The `zone`/`admin`/`review` subcommand removals are covered by D-0016 (`docs/project/DECISIONS.md:1807`) and the CLAUDE.md subcommand list.

### 6. §3 Agentic runtime model — already-captured
Loop in `internal/agentloop`; context management → D-0015 (`docs/project/DECISIONS.md:1882`); write-floor posture prompt → D-0023 (`:636`); Core Agent reads governed → D-0028 (`:234`). `/model` global control-plane semantics is the only soft spot — JPK itself labels it an "undocumented user-facing polish gap," i.e. a known gap, not content to preserve.

### 7. §4 Auth/identity A–H — already-captured
The A–G(+H) safety chain maps directly: D-0004 (`docs/project/DECISIONS.md:3545`), D-0005 (`:3437`), D-0006 (`:3287`), D-0007 (`:3146`), D-0008 (`:2979`), D-0009 (`:2774`), D-0010 (`:2563`), D-0011 (`:2325`), plus follow-ups D-0012 (`:2223`) / D-0013 (`:2095`).

### 8. §4.1 Identity Stages 1–5 — already-captured
Principals registry, full admin REST/UI surface, CLI removal → D-0016 (`docs/project/DECISIONS.md:1807`); the `svc:agent:core` principal and component-governance admin surface → D-0028 (`:234`) / D-0029 (`:141`) / D-0030 (`:45`).

### 9. §4.2 What genuinely remains — already-captured
"Refuses to start without usable identity config / engine-nil unreachable" → D-0027 (`docs/project/DECISIONS.md:418`); cold-start admin bootstrap → D-0011 (`:2325`); `joe zone`/`joe admin` removed → D-0016 (`:1807`).

### 10. §5 Skills system — already-captured
The `joe skills` subcommand is named in `CLAUDE.md:51`. The deeper workflow detail (agentskills.io format, quarantine/`joe skills approve`, `~/.joe/skills/`, `skills.lock.yaml`, `~/.joe/skills-policy.yaml`, and the `github.com/jaimegago/joe-sre-skills` MIT starter repo) is **not** in the two spines but **is** preserved in the tracked `docs/reference/joe-skills-design.md` and `README.md` (confirmed by grep). Nothing is lost on JPK retirement, so this is captured rather than unique.

### 11. §6 Security architecture — already-captured
Binary Read/Mutate axis → D-0020 (`docs/project/DECISIONS.md:854`); write floor → D-0018 (`:1315`); denial precedence floor>incident>RBAC → D-0022 (`:673`); captaincy handshake bind → D-0017 (`:1725`); no-auto-lapse captaincy → D-0024 (`:597`) / D-0025 (`:535`). Mirrored in `CLAUDE.md` "Architectural Invariants" (Action Safety, write floor, denial precedence) and the auto-memory safety section.

### 12. §6.1 Credential-provider abstraction — already-captured
Resolve/Probe/Describe, two-half resolved-credential type, two launch providers, deferred surface → D-0026 (`docs/project/DECISIONS.md:498`) and `docs/project/adr/D-0026-credential-provider-abstraction.md` (cited in-section). Per-type wiring gaps are tracked in `docs/backlog/` (aws/azure/registry-auth credential-provider files present).

### 13. §6.2 Component governance — already-captured
Credential-less registration → D-0029 (`docs/project/DECISIONS.md:141`); promotion as the governed read-only→armed transition + locator-safe read model → D-0030 (`:45`).

### 14. §6.3 Core Agent reads governed — already-captured
`svc:agent:core`, `auto_promote_reads`, migration `024`, dynamic predicate in `PolicyEngine.Decide` → D-0028 (`docs/project/DECISIONS.md:234`).

### 15. §7a Standard test commands — already-captured
`go test/vet/build`, `gofmt`, `-tags=integration`, frontend `npm run lint`/`test` → `CLAUDE.md` "Build / Test / Lint".

### 16. §7b OASIS relationship + 9/21 score — unique (relationship) / stale (score)
- **Relationship is unique to the spines.** OASIS appears in `docs/project/DECISIONS.md` only as two incidental mentions (`:839`, `:873`) — neither describes the harness, the `oasisctl` → `POST /api/v1/tasks` contract, nor the 21-scenario battery. The descriptive relationship lives in JPK and an auxiliary design doc (`docs/reference/joe-skills-design.md`) but **not** in any of the three spines.
- **The 9/21 score is stale** — JPK itself flags it (`JOE_PROJECT_KNOWLEDGE.md:529-532`): it predates the Phase-2 refactor + prompt fix, and there is no re-evaluated score in the repo.
- **Destination:** a **backlog file** — the live deferred work is "re-run OASIS post-Phase-2 and add the OASIS section to the README" (the §8 B4 launch blocker). The stable `POST /api/v1/tasks` contract that OASIS depends on is the only durable current-state nugget and could ride along as a one-line note there or in CLAUDE.md.

### 17. §8 Launch positioning & former-employer relationship — unique-migrate-worthy
Not present in any of the three spines (grep for `apache`/`portfolio`/`build-from-source`/`goreleaser` in `CLAUDE.md` + `docs/project/DECISIONS.md` returns nothing). Sub-claims:
- **Apache-2.0 / personal portfolio / build-from-source positioning** — current-state posture → **CLAUDE.md** (a one-line project-positioning note; `LICENSE` is ground truth for the license itself).
- **former-employer decoupling + history-scrub blocker** (rewrite the 3 commits carrying `a former-employer address`, purge ~71 MB of compiled-binary blobs, `git-filter-repo` Path B) and **launch-blocker B1–B4 status** — deferred pre-publish work → **a backlog file** (e.g. `docs/backlog/history-scrub.md`). Partially captured in the security-findings punchlist (since archived out of the repo) under "history scrub (Stream C)" — so the scrub mechanics are not wholly lost, but the consolidated launch-blocker checklist is.

### 18. §9 Stable invariants — already-captured
Single binary, SQLite graph (no Cayley), binary action classification, runtime-immutable write floor, OTel in middleware, prompts in `internal/prompts/`, category observability API, Core Agent autonomy levels — all mirror `CLAUDE.md` "Architectural Invariants."
**Count drift noted (derivable from the tree, so not migrate-worthy prose):** JPK says **20 edge types** and **27 migrations**; `CLAUDE.md:12` says "19 edge types" and the auto-memory says "20" migrations. Disk confirms JPK: `ls internal/store/migrations/*.up.sql | wc -l` → **27** (last is `027_audit_session_lifecycle_kind`). These are CLAUDE.md/memory staleness to fix at the source, not knowledge to carry forward from JPK.

### 19. §10 Known stale claims — already-captured
A drift-correction catalog; each item's *correction* is a logged decision (two-binary→single D-0008; T1/T2/T3→binary D-0020; panic/floor D-0018; source→component D-0021 `docs/project/DECISIONS.md:811`; identity F/G/H D-0009–D-0011; D-0016; D-0028; D-0029/D-0030). The catalog's value is killing stale chat assertions — moot once JPK retires and the synced spines are authoritative. Item 16 (learn-from-sessions) is tracked under row 22; item 3 is broken out as row 20.

### 20. §10 item 3 — provider-list correction — unique-migrate-worthy → CLAUDE.md
JPK (`JOE_PROJECT_KNOWLEDGE.md:598-600`) states Joe is **Claude + Gemini only** — "OpenAI explicitly rejected, Ollama not wired." This is **current and verified against code**: `internal/llmfactory/factory.go:27-52` handles only `claude`/`gemini`, and `internal/config/validation.go:13-36` validates only Anthropic/Gemini keys (rejecting unknown providers). **`CLAUDE.md` "Project Identity" still says "AI-agnostic (Claude, OpenAI, Ollama)"** — stale and contradicted by the code. The correct claim exists in neither spine, so it is migrate-worthy: **fix the provider list in CLAUDE.md** to "Claude, Gemini."

### 21. §11.0/11.1/11.3 Session subsystem as-built — unique-migrate-worthy → CLAUDE.md
The as-built session model has **essentially no coverage in the two spines**: `grep -ni "agent_sessions\|chat_messages\|session schema\|025_session\|sessionauthz" docs/project/DECISIONS.md` yields only one incidental `agent_sessions` mention (`:2857`), and `CLAUDE.md` does not describe sessions at all. The load-bearing current-state facts — two session types `{default, incident}`, `agent_sessions` + `chat_messages` as the live store (migrations 025–027), promote-in-place incident declaration, the dedicated `internal/sessionauthz` seam, the policy-driven `internal/sessionsweeper`, the `internal/sessionarchive` provider, and the per-user `/api/v1/sessions` vs admin `/api/v1/admin/sessions` split — are verified against the tree (file:line throughout §11.1/§11.3) and absent from the spines.
**Destination:** **CLAUDE.md** — a short "Session subsystem" convention block capturing the high-level invariants. The full as-built spec is preserved in the tracked `docs/reference/DESIGN-CHAT-SESSIONS.md` §12 (the authoritative B001 design) and in the code, so only the orientation-level summary needs to land in the spine.

### 22. §11.2 Orphaned learn-from-sessions extractor — already-captured
The dormant/orphaned/ungoverned status of `internal/knowledge/learning/extractor.go` and its fate-pending-on-B001-retirement is preserved in tracked files: `docs/backlog/learn-from-sessions-fate.md` (the fate decision — a backlog spine member) and `docs/reference/learn-from-sessions-current-state.md` (full current-state). Both exist on disk. Captured.

### 23. §11.3 Doc-debt findings — unique-migrate-worthy → backlog file
JPK (`JOE_PROJECT_KNOWLEDGE.md:833-845`) records two concrete, still-open cleanup items found during its last pass: (a) `internal/api/webui.go:229` and `:769` still cite "migration 009" for CHECKs that migration **025** now owns (stale comments, behavior correct); (b) `docs/reference/DESIGN-CHAT-SESSIONS.md:827-828` claims three `b001-*` inventory reports landed in an investigations directory — **not on disk** (confirmed: no such `b001-*` files exist). These are actionable deferred cleanups that exist in no spine. **Destination:** a **backlog file** (e.g. `docs/backlog/session-doc-debt.md`), or spawn-as-task.

---

## Acceptance check

- ✅ Gitignore status reported with evidence (intentional, never committed).
- ✅ Every JPK section classified exactly once (23 rows; §7/§11 split where genuinely mixed, each sub-row appearing once).
- ✅ Every unique-migrate-worthy item names a destination spine (rows 16, 17, 20, 21, 23).
- ✅ Nothing modified or moved — read-only investigation; this findings file is the only artifact created.
