# Security in Layers — Joe

This document captures Joe's security posture, known gaps, remediation plan, and — critically — the **Action Safety Framework** that governs what Joe is allowed to change and under what conditions.

Joe runs as a single `joe` binary: bare `joe` starts the server (HTTP API on `:7777`, Core Agent, adapters, graph); subcommands (`joe mcp`, `joe slack`, `joe panic`, `joe unlock`, …) dispatch ahead of it. There is no separate daemon — the earlier two-binary `joecored` split is retired. Where this document and [`docs/DECISIONS.md`](DECISIONS.md) conflict, the decision log is the source of truth.

This is a living document — update it as fixes land.

---

## Core Safety Principles

These principles fall into two categories: **invariants** that hold at every stage of Joe's trust progression and are hardcoded into the binary, and **policy structure** that defines how humans express trust through configuration. Both are non-negotiable in their respective scopes — invariants cannot be overridden by configuration, and policy structure cannot be bypassed by LLM reasoning or prompt injection.

### Invariants (hardcoded, true at every trust stage)

1. **Joe must always announce mutations.** Even when authorized, Joe notifies the human before and after any managed-system mutation. The tool executor enforces a blocking pre-execution notification (cancellable) and a post-execution summary for every mutating action — this is compiled in, not an LLM instruction.

2. **Safety config is outside Joe's reach.** Joe cannot read, write, or influence its own safety configuration. The safety policy (`~/.joe/safety-policy.yaml`) and the entire `~/.joe/` directory are excluded from every file tool at compile time (`internal/safety/invariants.go`).

3. **The write floor is boot-resolved and runtime-immutable.** Joe resolves a read-only "write floor" once at boot (observation mode or a sticky safe-mode/panic state). Nothing in the running binary can lower it — recovery is a restart, never a live down-transition (`internal/safety/floor.go`, D-0018).

### Policy structure (how humans express trust)

4. **Humans authorize every mutating action.** Joe never executes a managed-system mutation that a human has not explicitly opted into via the safety policy's `act` section. Within an authorized action, Joe may decide *which specific call* fits the situation, but enabling the action is always a human act.

5. **Default is deny.** Every mutating capability starts disabled. Humans opt in per action, not opt out. An unknown/unregistered tool is classified as a mutation and denied by default.

6. **Allowlists, not blocklists.** We enumerate what's permitted, never what's forbidden.

---

## Part 1: Current State

### What We Do Right

| Area | Detail |
|------|--------|
| SQL injection | All queries use parameterized statements (`?`/`$n` placeholders) via `database/sql` |
| Command injection | `run_command` uses `exec.CommandContext` (no shell), plus an allowlist of permitted commands |
| Default bind | The `joe` server binds to `localhost:7777` by default |
| API keys | LLM keys loaded from environment variables, not config files |
| Output limits | `run_command` truncates stdout/stderr; `read_file` caps file size |
| URL encoding | Client uses `url.PathEscape` / `url.QueryEscape` for all HTTP calls |
| Adapters are read-only | K8s, Git, AWS, Azure, observability, datastore, GitOps adapters expose only read/list/describe operations. The only mutating adapters are the doc-publish path (Git commit/push, Confluence, Notion) and the code-review path (GitHub/GitLab comment + request-changes) |
| Action classification | Every tool is classified on a **binary Read/Mutate axis** at registration; the executor gate checks the write floor and the safety policy before every `Execute()` (`internal/safety/tier.go`, `internal/tools/executor.go`) |
| Write floor | Boot-resolved, runtime-immutable read-only floor (observation mode or sticky safe mode); denies every mutate independent of policy or RBAC (`internal/safety/floor.go`, D-0018) |
| Safety policy | Loaded once at startup from `~/.joe/safety-policy.yaml`; immutable at runtime; default-deny for mutating actions (`internal/safety/policy.go`) |
| Self-protection invariants | Joe cannot read/write `~/.joe/`, cannot run `joe`/`kill`/`pkill`/`killall` — hardcoded, no override (`internal/safety/invariants.go`) |
| Path sandboxing | `write_file` enforces `allowed_directories` from the safety policy; symlink-aware, case-insensitive on macOS/Windows |
| Subcommand allowlists | kubectl/helm/argocd restricted to read-only subcommands; compiled-in, not configurable by LLM (`internal/tools/local/runcmd/subcommands.go`) |
| Mutation notification contract | Blocking pre-execution notification with cancel; post-execution summary, for every mutating action (`internal/safety/notifier.go`) |
| API authentication | Edge auth middleware on all `/api/v1/` routes (Bearer service-account tokens; OIDC where configured) (`internal/api/middleware.go`) |
| Request size limits | `http.MaxBytesReader` wraps all request bodies (`internal/api/middleware.go`) |
| RBAC | Zone-scoped access control: zones carry an action ceiling, components are assigned to zones, grants map principals to zones (`internal/rbac/`) |
| Emergency shutdown | Panic/safe-mode persisted to the `cluster_panic_state` DB row; safe mode raises the write floor on the next boot (`internal/store/panic_store.go`, `internal/safety/panic.go`) |
| Secure home resolution | `paths.JoeDirPath()` uses `os/user.Current()` (via `secure_unix.go`/`secure_windows.go`), bypassing the `HOME` env var to prevent path manipulation |

---

## Part 2: Mutation Audit

Every place Joe can change state, by axis. The organizing question is the one the action classification asks: **does this operation mutate the managed system** (live infrastructure + the code/config that governs it)?

### Local tools (run in the `joe` process, user-facing task loop)

| Tool | Mutates managed system? | What it changes | Authorization | Notification |
|------|-------------------------|-----------------|---------------|--------------|
| `write_file` | **YES (Mutate)** | Files within `allowed_directories` only | `act.write_file.enabled` + path sandbox | Before (blocking) + after |
| `run_command` | **YES (Mutate)** | Depends on allowlist + subcommand validation | `act.run_command.enabled` + command allowlist + subcommand allowlist | Before (blocking) + after |
| `read_file` | No (Read) | — | Always allowed (blocks `~/.joe/`) | — |
| `local_git_status` / `local_git_diff` | No (Read) | — | Always allowed | — |
| `ask_user` | No (Read) | — | Always allowed | — |

**`write_file` detail** (`internal/tools/local/writefile/writefile.go`): disabled by default; blocks `~/.joe/` (hardcoded invariant, symlink-aware, case-insensitive); when `allowed_directories` is set, writes are restricted to those dirs; blocking pre-execution notification with cancel.

**`run_command` detail** (`internal/tools/local/runcmd/runcmd.go`): default allowlist is read-only only (`ls, cat, head, tail, grep, find, wc`); kubectl/helm/argocd are **excluded** from the default and must be added explicitly in `safety-policy.yaml`. When enabled, mutation-capable commands enforce compiled-in subcommand allowlists (kubectl: get/describe/logs/top/explain/…; helm: list/status/get/history/…; argocd read-only verbs). `joe`, `kill`, `pkill`, `killall` are blocked by hardcoded invariant.

### Joe's own model maintenance — classified as Read (not a managed-system mutation)

Per D-0018/D-0019/D-0020, a "write" is an operation that mutates the *managed system*. The tools below only record observed state into Joe's own graph/store/knowledge — the managed system is in the same state after they run — so on the action axis they are **reads**, not mutations. They are always allowed and never frozen by the write floor or the incident gate (Joe must keep its own model current even in safe mode).

| Tool | Writes to | Classification |
|------|-----------|----------------|
| `graph_add_node`, `graph_add_edge`, `graph_update_node` | Joe's knowledge graph | Read (idempotent UPSERTs) |
| `register_component` | Joe's component store | Read (non-idempotent insert; carries a per-run durability key) |
| `save_onboarding_fact` | Joe's facts store | Read (durability key) |
| `save_knowledge_entry`, `generate_doc_draft` | Joe's knowledge / proposal store | Read (durability key) |
| `detect_doc_drift` | — (read-only comparison) | Read |

These tools are registered Read in `internal/safety/tier.go`. The classification is the load-bearing claim: it is *why* the LLM may maintain Joe's graph autonomously without a mutation gate, while still being unable to touch live infrastructure.

> **D-0020 note.** Some of these inserts (e.g. `register_component`, `save_knowledge_entry`) generate row identity server-side and are not idempotent on retry. They declare `NeedsDurability`, so the durable executor persists a per-run idempotency key and dedups a crash-resume replay. This is independent of the Read/Mutate axis — "does this need crash-resume" is a different question than "does this mutate the managed system."

### Managed-system mutations

| Tool | What it changes | Policy key (in `act`) | Notification |
|------|-----------------|-----------------------|--------------|
| `write_file` | Local files (sandboxed) | `write_file` | Before + after |
| `run_command` | Local + remote infra (restricted) | `run_command` | Before + after |
| `publish_doc_update_confluence` / `_notion` / `_git` | External docs (Confluence page, Notion page, Git repo) | `confluence_publish` / `notion_publish` / `git_push` | Before + after |
| `github_comment` / `gitlab_comment` | External PR/MR thread | `github_comment` / `gitlab_comment` | Before + after |
| `github_request_changes` | External GitHub review (gates merge) | `github_request_changes` | Before + after |

All managed-system mutations are denied unless their `act` policy key is enabled. Unknown tools default to Mutate and are denied.

### API endpoints that mutate Joe's own state

| Endpoint | Method | Mutation | Authorization |
|----------|--------|----------|---------------|
| `/api/v1/components` | POST | Creates a component + registers its adapter | Edge auth |
| `/api/v1/components/{id}` | DELETE | Removes a component + disconnects its adapter | Edge auth |
| `/api/v1/components/{id}/promote` | POST | Arms a component (readonly → armed) | Edge auth |
| `/api/v1/clarifications/{id}/answer` | POST | Marks answered + applies stored graph ops | Edge auth |
| `/api/v1/onboarding` | POST | Triggers LLM → graph mutations | Edge auth |
| `/api/v1/refresh` | POST | Triggers graph reconciliation | Edge auth |
| `/api/v1/admin/*` | various | Zones, policies, read-posture, read-promotions, sessions | Admin-gated + audited |

All `/api/v1/` routes sit behind edge auth middleware; admin routes additionally require the admin gate and write an append-only audit row. Request bodies are limited by `MaxRequestBody` middleware.

### Adapters

| Adapter family | Can mutate external systems? | Detail |
|----------------|------------------------------|--------|
| K8s, AWS, Azure, datastore, observability, GitOps, networking, security/runtime, registries | **No** | Read/list/describe/query only |
| Git (read path) | **No** | `ReadFile`, `ListFiles`, `Log`, `Diff` (can `Pull`, never commit/push) |
| Git (doc-publish path) | **Yes** | `CommitAndPush`, gated behind `act.git_push` and the doc-proposal approval flow |
| GitHub / GitLab | **Yes** | Comment + request-changes, gated behind their `act` keys |

---

## Part 3: Action Safety Framework

This is how Joe handles mutations. It is implemented as a hardcoded enforcement layer, not as LLM instructions or soft guidelines.

### 3.1 Action classification — a binary Read/Mutate axis

Every tool is classified into one of **two** classes (`internal/safety/tier.go`):

| Class | Description | Default | Examples |
|-------|-------------|---------|----------|
| **Read** (`ActionRead`) | Does **not** mutate the managed system. Includes component queries, Joe's own graph/model maintenance, and notifications to humans. | Always allowed, no policy check | `read_file`, `git_log`, `k8s_get`, `graph_query`, `graph_add_node`, `register_component` |
| **Mutate** (`ActionMutate`) | Mutates the managed system (files, infrastructure, deployments, external PR/MR threads, published docs). | Denied by default; per-action opt-in via the `act` policy | `write_file`, `run_command`, `publish_doc_update_*`, `github_comment`, `github_request_changes` |

This **replaces the former three-tier scheme** (Observe/Record/Act, T1/T2/T3). Per **D-0020** (collapse to a binary axis; see also D-0018/D-0019), severity-of-mutation is deliberately *not* encoded as a classification tier — a static blast-radius taxonomy is hard to get right and hard to evaluate on a non-deterministic LLM. Blast-radius safety lives elsewhere (tools, skills, the per-zone/per-capability graduation ladder); the classification carries only the action axis. The old "Record" band (internal-state mutations) is gone: those operations are now Reads, because they do not change the managed system.

Classification is hardcoded per tool at registration time. The LLM cannot change a tool's class. **Unknown tools are classified Mutate and denied by default.**

### 3.2 Policy file

The safety policy lives in a file Joe **cannot access**:

```
~/.joe/safety-policy.yaml       # Human-editable only
```

This path is excluded from `read_file`/`write_file` at compile time, is not readable by any LLM-invokable tool, and is loaded **once** at startup (never re-read at runtime by the agent).

```yaml
# ~/.joe/safety-policy.yaml
version: 1

# act: managed-system mutations — each is denied unless explicitly enabled.
act:
  write_file:
    enabled: false
    allowed_directories: []      # e.g., ["/tmp/joe-workspace"]

  run_command:
    enabled: true
    allowed_commands:            # Overrides the compiled-in read-only allowlist
      - ls
      - cat
      - head
      - tail
      - grep
      - find
      - wc
    # kubectl, helm, argocd deliberately excluded — add them here to accept the risk.

  # Doc-publish and code-review mutations (default false):
  git_push:            { enabled: false }
  confluence_publish:  { enabled: false }
  notion_publish:      { enabled: false }
  github_comment:      { enabled: false }
  github_request_changes: { enabled: false }
```

> **`record:` is a retained compatibility shim.** The policy struct still parses a `record:` section (`graph_mutations`, `source_registration`, `onboarding_facts`, `autonomous_refresh`) for backward compatibility with existing policy files. It is **inert**: since Joe's model-maintenance tools are now classified Read, the executor's `CheckAccess` consults only the `act` section. The `record` keys no longer gate anything (D-0018/D-0019/D-0020).

### 3.3 Hardcoded enforcement points

These checks are compiled into the binary and cannot be bypassed by configuration, LLM reasoning, or prompt injection.

**1. Tool executor gate** (`internal/tools/executor.go`, `Execute`). For every tool call, in order:

```
1. Classify the tool (Read or Mutate).
2. Write floor (D-0018): if the floor is up AND the tool is Mutate → deny.
   Checked FIRST among the denials (precedence floor > incident > RBAC).
3. Zone/component scope: if the call targets a component_id outside the
   caller's authorized zones → deny.
4. Namespace scope: if a K8s call targets a namespace outside scope → deny.
5. Safety policy CheckAccess: a Mutate tool requires its act.<key>.enabled == true,
   else deny. (Reads always pass.)
6. Mutate only: blocking pre-execution notification (human can cancel).
7. Execute.
8. Mutate only: post-execution notification (summary of result).
```

The **incident** half of the precedence sits one layer up, in the §C captain-session gate (`internal/captaingate/`), which checks the same floor before its own gate (see §3.4).

**2. Self-protection invariants** (`internal/safety/invariants.go`, hardcoded, no config override):

| Invariant | Enforcement |
|-----------|-------------|
| Joe cannot read/write its safety policy | `~/.joe/safety-policy.yaml` excluded from all file tools |
| Joe cannot read/write its skills policy | `~/.joe/skills-policy.yaml` excluded |
| Joe cannot write to `~/.joe/` | The whole directory is excluded (symlink-aware, case-insensitive) |
| Joe cannot run the `joe` binary | `joe` blocked from `run_command` |
| Joe cannot kill processes | `kill`, `pkill`, `killall` blocked |

These are constants in source, not configurable.

**3. Notification contract** (hardcoded for every mutating action, `internal/safety/notifier.go`):

```
BEFORE (blocking, cancellable): "[Joe] About to <action>. Proceeding... (cancel to abort)"
AFTER:                          "[Joe] Done: <action summary>"
```

The pre-execution notification is enforced by the executor before calling `Execute()` — it is not a soft guideline for the LLM.

### 3.4 Denial precedence: floor > incident > RBAC (D-0022)

When more than one gate would deny a mutation, the executor surfaces the reason ordered by **how readily the caller can resolve it**:

1. **Write floor** — the least readily fixable (recovery is a restart, or clearing a panic state). Checked first.
2. **Incident gate** — the §C captain-session gate: in incident regime, only the incident's captain session may attempt a mutation; non-captain sessions are refused (deny-only — the gate never *grants* authority). Enforced in `internal/captaingate/`.
3. **RBAC** — zone scope / safety policy. The accessor's RBAC check runs unchanged *after* the incident gate (gate-then-accessor).

Enforcement short-circuits at the first failing check, so for a mutation that trips both the floor and a zone violation, only the floor error is produced.

### 3.5 Read authorization: the install-wide read posture (D-0041, D-0043)

Reads are governed by a single persisted, install-wide **read posture** with two values (`internal/readposture/`, `internal/rbac/policy.go`):

- **`team_flat`** — the **launch default**. Any authenticated principal may read any component, regardless of grant. This is the team-public read model already settled for chat sessions (privacy between teammates is a non-goal; the spine is integrity and accountability, not secrecy). A fresh install and an upgraded install both inherit `team_flat` until an operator deliberately opts in to `zoned`.
- **`zoned`** — the grant-based read decision (the **full-mode read path**): the zone+grant behaviour described in §8, byte-identical to before the posture existed.

Key properties:

- The `team_flat` admit sits **after** the zone-allows-action gate. It widens **WHO** may perform a read the zone already permits; it never changes **WHICH** actions a zone allows. It fires for the read action only and requires a non-empty (authenticated) principal set.
- The posture is **orthogonal to the write floor.** A `team_flat` install with the floor up still denies every mutate — read posture has no input to the executor's floor check.
- The posture governs **human-facing transport reads only.** The autonomous `agent:core` read surface (background refresh) is a **separate axis**, governed solely by `auto_promote_read` (per-component-type promotion) plus grants. The two are separated at engine construction: the transport policy engine carries the read-posture resolver (`NewPolicyEngineWithGovernance`); the refresh engine does not (`NewPolicyEngineWithPromote`, posture seam nil). Flipping the posture cannot change what `agent:core` can read (D-0043).
- The posture is read **live per request** (no boot cache). Flipping it is an **admin-gated, audited operator act** on `GET`/`POST /api/v1/admin/read-posture`.

### 3.6 Protected security configuration

**Critical invariant:** LLM tools cannot modify Joe's security configuration. This is enforced **architecturally**, not by a table-name guard inside the tool path:

- The security tables — `security_zones`, `component_zone_assignments`, `rbac_policies`, `read_posture`, `auto_promote_reads`, the admin-principal set — are mutated **only** through the admin REST surface (`/api/v1/admin/*`), which is admin-gated and writes an append-only audit row.
- The LLM's tools have **no raw-SQL write path** to those tables. Joe's model-maintenance tools (`graph_add_*`, `register_component`, `save_*`) write only to operational tables (graph, components, facts, knowledge) via their typed repositories.
- The safety policy and the `~/.joe/` directory are self-protected from the file tools (§3.3).

So an LLM — even under prompt injection — has no tool that reaches the security configuration; changing it requires an authenticated admin call on a surface no LLM tool can invoke.

### 3.7 Roadmap mutations (not yet built)

The executor today enforces the floor, zone/namespace scope, the safety policy, and the notification contract. The following deterministic controls are **designed but not yet implemented**; they are tracked in Part 5 and must not be assumed active:

- **Environment-level operation blocking** — deterministic pattern-matching that blocks namespace/cluster-scoped destructive operations (e.g. `kubectl delete namespace`, `--all` selectors, `terraform destroy`) regardless of policy flags, unless the specific environment is allow-listed.
- **Mutation circuit breaker** — a rolling-window rate limiter on mutating actions that trips after a threshold and suspends further mutations until a human resets it.
- **Credential isolation enforcement** — validation that Joe never uses caller-provided infrastructure credentials, only its own pre-scoped service account.

These become relevant as Joe gains infrastructure-mutating adapters (Part 6). Until they land, the live guarantees are: managed-system mutation is denied by default, gated per action, floor-blocked in observation/safe mode, scope-checked against zones, and announced before and after.

---

## Part 4: Gaps by Layer (non-mutation security)

### Layer 1: Network boundary (`joe` HTTP API)

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
| ~~No authentication~~ | ~~CRITICAL~~ | **FIXED** | Edge auth middleware on all `/api/v1/` routes (`internal/api/middleware.go`) |
| ~~No request size limits~~ | ~~HIGH~~ | **FIXED** | `http.MaxBytesReader` wraps all request bodies |
| Rate limiting | — | **Configurable** | Rate-limit middleware (`server.rate_limit_rps`/`burst`) |
| Plaintext HTTP | HIGH | Partially addressed | TLS is configurable (`server.tls_*`); plaintext is the default for localhost binds |
| CORS / security headers | MEDIUM | Partial | CORS middleware present; some response security headers still missing |

**Key files:** `cmd/joe/server.go`, `internal/api/server.go`, `internal/api/middleware.go`

### Layer 2: Credential storage

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
| Credentials in DB | CRITICAL | Partially addressed | Component connection config stored in SQLite; credential encryption-at-rest is in progress (Phase 6.5) |
| No credential rotation | MEDIUM | Open | Static credentials never refreshed or expired |
| World-readable config | MEDIUM | Open | `~/.joe/config.yaml` uses the default umask |

### Layer 3: LLM tool sandbox

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
| ~~No path sandboxing on `write_file`~~ | ~~CRITICAL~~ | **FIXED** | `write_file` enforces `allowed_directories` + blocks `~/.joe/` |
| No path sandboxing on `read_file` | HIGH | Open | The LLM can read any file the process user can access (except `~/.joe/`) |
| Prompt injection | CRITICAL | Open | User/infra text flows unsanitized into LLM context. **Mitigated** by the architecture: even a fully-injected LLM has no tool that mutates the managed system without a human-enabled `act` key, and no tool that reaches security config — but the input channel itself is unsanitized |

**Key files:** `internal/tools/local/readfile/`, `internal/tools/local/writefile/`, `internal/safety/invariants.go`

### Layer 4: Authorization

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
| ~~No per-component access control~~ | ~~HIGH~~ | **FIXED** | Zone-scoped RBAC + middleware enforce per-component access (`internal/rbac/`) |
| ~~No user identity model~~ | ~~HIGH~~ | **FIXED** | API-key → principal mapping; OIDC where configured (`internal/rbac/`, `internal/api/authconfig.go`) |
| Audit logging | — | **Present** | Admin acts and infra access write append-only audit rows (`internal/audit/`) |

---

## Part 5: Implementation Roadmap

### Action Safety Framework — COMPLETE

- Binary Read/Mutate classification with a hardcoded registry: `internal/safety/tier.go`
- Safety policy loader from `~/.joe/safety-policy.yaml`: `internal/safety/policy.go`
- Executor gate (floor → scope → policy → notify) before every `Execute()`: `internal/tools/executor.go`
- Default-deny for unknown tools (classified Mutate)
- Self-protection invariants + path sandboxing: `internal/safety/invariants.go`, `internal/tools/local/writefile/`
- `run_command` subcommand validation: `internal/tools/local/runcmd/subcommands.go`
- Mutation notification contract: `internal/safety/notifier.go`
- Edge auth + request size limits: `internal/api/middleware.go`

### Security architecture — COMPLETE

- Write floor (observation + safe mode), boot-resolved and immutable: `internal/safety/floor.go` (D-0018)
- Emergency shutdown / panic mode, persisted to the `cluster_panic_state` DB row: `internal/safety/panic.go`, `internal/store/panic_store.go`
- Zone-scoped RBAC, admin API, audit: `internal/rbac/`, `internal/api/admin.go`, `internal/audit/`
- Install-wide read posture (`team_flat` launch default / `zoned` full-mode): `internal/readposture/`, `internal/rbac/policy.go` (D-0041, D-0043)
- Incident regime + §C captain-session gate: `internal/captaingate/`, `internal/sessiongate/`

### Still open

- **Credential encryption at rest** and rotation
- **Environment-level operation blocking** — deterministic block of namespace/cluster-scoped destructive operations (§3.7)
- **Mutation circuit breaker** — rolling-window rate limiter on mutating actions with manual reset (§3.7)
- **Credential isolation enforcement** — verify Joe never uses caller-provided infra credentials (§3.7)
- **`read_file` path sandboxing**
- Response security headers; TLS-by-default for non-localhost binds

---

## Part 6: Infrastructure Healing — From Observer to Operator (design direction)

The Action Safety Framework is not just a wall against mutations — it's a **gate** that humans open incrementally as trust builds. Joe is designed to evolve from a read-only observer into an active infrastructure healer, under explicit human control. Today Joe ships as a read-first copilot; the healing actions below are the forward direction, each gated by a per-action `act` policy key, the write floor, zone scope, and (in incident regime) the captain gate.

This is how the Core Safety Principles compose: invariants 1–3 hold at every stage of the progression, while policy structure 4–6 is what humans use to move Joe along it.

### 6.1 Trust progression

```
Stage 1: Observe                 Stage 2: Read-only infra tools
─────────────────                ──────────────────────────────
Joe reads everything,            Human enables kubectl/helm/argocd in
changes nothing.                 safety-policy.yaml. Subcommand allowlists
"Why is payment-api slow?"       restrict to get/describe/logs. Joe queries
→ Joe explains the cause.        infra directly, still mutating nothing.

Stage 3: Supervised healing      Stage 4: Autonomous healing
───────────────────────────      ────────────────────────────
Human enables specific           Human enables broader mutating actions.
mutating actions (e.g.           Joe heals known patterns without prompting,
k8s_scale). Joe announces        still announcing before and after. Audit
before acting; the human         log captures everything; the circuit breaker
can cancel.                      (§3.7) bounds runaway sequences.
```

At every stage Joe decides *which specific action* to take within the classes humans have authorized. The class boundary is always human-authored; the situational judgment within it is Joe's (principle 4).

### 6.2 Healing actions by adapter (planned)

When humans choose to enable healing, each action is individually toggled — there is no "enable all mutations" switch. Examples of planned mutating actions, each with its own `act` policy key: K8s scale/restart/cordon/apply; Alertmanager silence; PagerDuty ack/resolve; Git commit/push; Helm upgrade. The doc-publish and code-review mutations (Part 2) are the first such actions already implemented.

### 6.3 Graph-driven healing

Joe's healing is informed by the knowledge graph, not blind command execution. Before acting, Joe traverses relationships to understand blast radius — e.g. recognizing that scaling a service won't help when the real cause is a saturated database it depends on. This is the difference between Joe and a runbook executor: Joe reasons about *why* something is broken and whether a proposed fix will actually help.

### 6.4 Healing safety guarantees

Even with healing enabled, the hardcoded invariants remain: every mutating action announces before and after; subcommand allowlists are compiled in (enabling `kubectl` does not unlock `kubectl delete`); self-protection invariants never change; per-action granularity means each mutation is a separate trust decision; and the write floor blocks every mutation in observation or safe mode regardless of which actions are enabled. The environment-level block and circuit breaker (§3.7) are the planned additions that bound blast radius and runaway sequences.

---

## Part 7: Emergency Shutdown (Panic Mode)

Joe has a kill switch. When something goes wrong — runaway automation, unexpected behavior, or human error — operators can immediately halt all mutating operations.

### 7.1 Panic triggers

| Method | Command / Endpoint | Use case |
|--------|-------------------|----------|
| CLI command | `joe panic` | Operator at a shell |
| API endpoint | `POST /api/v1/panic` | Automation, external monitoring |
| Signal | `SIGUSR1` to the `joe` server process | Unix-native, works when the API is unreachable |

### 7.2 Panic state — a DB row, not a file

Panic state has exactly one home: the **`cluster_panic_state` DB row** (single row, id=1, created by migration 008; `internal/store/panic_store.go`). There is **no `~/.joe/panic.state` file** (D-0018 consolidation). Panic entry writes the row via `SetPanicked`; boot reads it via `IsPanicked`.

When panic is triggered, Joe records the panic state (trigger source, reason, timestamp) to that row and stops accepting mutating work. Because the state is persisted, a restart sees it.

### 7.3 Safe mode = the write floor, raised at boot

There is no separate "safe mode" runtime toggle. When boot finds a panic state present in the DB row, `ResolveWriteFloor` raises the **write floor** with reason `safe_mode` (`internal/safety/floor.go`). The floor:

- denies every managed-system mutation (the Mutate set), independent of policy or RBAC;
- is **runtime-immutable** — nothing in the running binary lowers it;
- is recovered only by clearing the panic state and **restarting** (recovery is a restart, never a live down-transition).

Reads continue normally in safe mode — Joe stays a useful read-only copilot while locked.

> The separate `JOE_MODE=observation` env var raises the same floor with reason `observation` — a calm, intended read-only resting posture (not panic). A present panic state wins over the observation env var.

### 7.4 Unlock procedure

To clear the panic state (so the next boot resolves the floor down):

```bash
joe unlock --reason "investigated panic, false alarm - operator error"
```

Unlock is CLI-only — there is no `POST /api/v1/unlock` HTTP endpoint; recovery is a local operator action.

Unlock requires an authenticated caller and a mandatory `reason` (audited). Clearing the panic state plus a restart returns Joe to normal operation.

### 7.5 API endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/panic` | POST | Trigger emergency shutdown (record panic state) |
| `/api/v1/panic/status` | GET | Report panic / floor state |
| `joe unlock` (CLI) | — | Clear panic state (requires reason) — CLI-only, no HTTP endpoint |
| `/api/v1/mutate-status` | GET | Report the boot-resolved write floor and its reason |

### 7.6 Safety guarantees

1. **Panic state is durable** — persisted to the DB row, enforced as the write floor on the next boot.
2. **Recovery is explicit** — no automatic unlock; a human must clear the state and provide a reason.
3. **The floor cannot be lowered at runtime** — the lowering operation does not exist in the binary; recovery is a restart.
4. **Reads survive** — safe mode blocks mutations only; Joe stays observable.

---

## Part 8: Security Zones and Protected Configuration

### 8.1 Security zones (the `zoned` full-mode read/write model)

Zones decouple RBAC policy from individual components. Admins define zones with an **action ceiling**, then assign components to zones. Grants map principals to zones. There are **no roles and no groups** — this is zone-scoped access control, and grants are action-less (the action ceiling lives on the zone, not on the grant).

The action vocabulary (`internal/rbac/zones.go`): `read`, `query`, `mutate`, `delete`, plus the componentless capabilities `declare_incident` / `resolve_incident`.

Default zones seeded by migration 006:

| Zone | Allowed actions |
|------|-----------------|
| `prod-readonly` | `read`, `query` |
| `prod-write` | `read`, `query`, `mutate` |
| `dev-full` | `read`, `query`, `mutate`, `delete` |
| `unassigned` | `read` (default for new components — most restrictive) |

**Flow:**
1. Joe registers a component → `components` table.
2. No zone assignment found → defaults to `unassigned` (read-only).
3. An admin assigns a zone via the admin API → `component_zone_assignments` table.
4. Subsequent requests respect the assigned zone.

**Permission evaluation (mutate example, under `zoned`):**
```
Tool: a mutating tool on grafana/xyz-prod
Component zone: prod-readonly  → ceiling [read, query]
Required action: mutate
Result: DENIED (zone does not permit mutate)
```

Under the launch-default `team_flat` posture, *read* decisions short-circuit to allow for any authenticated principal (§3.5); *mutate* decisions are unaffected and still evaluate against the zone ceiling, the safety policy, and the write floor.

> **Launch note (read-posture-latch).** The zone/policy/component-zone admin surface is the **full-mode (`zoned`) era** model. At launch the install ships `team_flat`, and the zone-administration UI is de-emphasized; zones become operative when an operator opts in to `zoned`. See `docs/backlog/read-posture-latch.md` and D-0041/D-0043.

### 8.2 Protected configuration

**Critical invariant:** LLM tools cannot modify security configuration. As described in §3.6, this is enforced **architecturally** — the security tables are reachable only through the admin REST surface, and no LLM-invokable tool has a raw-SQL write path to them.

| Table | LLM tool can read | LLM tool can write | Mutated via |
|-------|-------------------|--------------------|-------------|
| `components` | ✅ (queries) | ✅ (register_component) | Component tools + admin |
| `graph_*`, `knowledge_*`, facts | ✅ | ✅ | Model-maintenance tools (Read class) |
| `security_zones` | — | ❌ **never** | Admin API only (audited) |
| `component_zone_assignments` | — | ❌ **never** | Admin API only (audited) |
| `rbac_policies` | — | ❌ **never** | Admin API only (audited) |
| `read_posture`, `auto_promote_reads` | — | ❌ **never** | Admin API only (audited) |
| audit rows | — | append-only | Audit layer |

> There is intentionally **no** `CanWriteTable`/`writeProtectedTables` guard inside the tool executor. Earlier drafts of this document described one; it was never the mechanism. Protection comes from the tool surface itself: the LLM's tools write only operational data through typed repositories, and security configuration is a different surface (admin REST, admin-gated, audited).

### 8.3 Deployment

Joe runs as a **single `joe` process** with one SQLite database. Security enforcement (write floor, safety policy, zone RBAC, read posture, incident gate) is in-process. The admin REST surface is admin-gated and audited.

> An earlier draft described an optional second `joe-security` process with its own `security.db` for hardened/remote deployments. That mode is **not built** — there is no `cmd/joe-security` or `internal/securitysvc` in the tree. If a future hardened split is pursued, it gets its own decision entry.

### 8.4 Admin API

The admin surface is separate from the LLM-accessible API and requires admin authentication. Every write is audited.

```
GET/POST/PATCH/DELETE /api/v1/admin/zones              # Zones (action ceilings)
GET/POST/DELETE       /api/v1/admin/component-zones    # Component → zone assignments
GET                   /api/v1/admin/unassigned         # Components with no zone
GET/POST/DELETE       /api/v1/admin/policies           # Principal → zone grants
GET/POST              /api/v1/admin/read-posture        # team_flat | zoned (D-0041)
GET/POST              /api/v1/admin/read-promotions     # Per-type autonomous-read promotion
GET/POST/DELETE       /api/v1/admin/admins              # Admin principals
GET/POST              /api/v1/admin/principals/...       # Principal enable/disable
GET                   /api/v1/admin/credential-status    # Credential authz/connectivity (D-0026)
GET/POST              /api/v1/admin/sessions/...          # Cross-tenant session governance
```

---

## References

- OWASP Top 10: https://owasp.org/www-project-top-ten/
- Go secure coding: https://owasp.org/www-project-go-secure-coding-practices-guide/
- [`docs/DECISIONS.md`](DECISIONS.md) — normative decision log (D-0018 write floor / panic; D-0020 binary axis; D-0022 denial precedence; D-0041/D-0043 read posture)
- [`docs/joe-architecture.md`](joe-architecture.md) — system architecture
- [`docs/JOE_SECURITY.md`](JOE_SECURITY.md) — security architecture overview
- [`docs/JOE_RBAC_IMPLEMENTATION.md`](JOE_RBAC_IMPLEMENTATION.md) — RBAC middleware spec
