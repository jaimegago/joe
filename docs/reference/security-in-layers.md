# Security in Layers — Joe

This document captures Joe's security posture, known gaps, remediation plan, and — critically — the **Action Safety Framework** that governs what Joe is allowed to change and under what conditions.

Joe runs as a single `joe` binary: bare `joe` starts the server (HTTP API on `:7777`, Core Agent, adapters, graph); subcommands (`joe mcp`, `joe slack`, `joe panic`, `joe unlock`, …) dispatch ahead of it. There is no separate daemon. Where this document and [`docs/project/DECISIONS.md`](../project/DECISIONS.md) conflict, the decision log is the source of truth.

This is a living document — update it as fixes land.

---

## Core Safety Principles

These principles fall into two categories: **invariants** that hold at every stage of Joe's trust progression and are hardcoded into the binary, and **policy structure** that defines how humans express trust through configuration. Both are non-negotiable in their respective scopes — invariants cannot be overridden by configuration, and policy structure cannot be bypassed by LLM reasoning or prompt injection.

### Invariants (hardcoded, true at every trust stage)

1. **Joe must always announce mutations.** Even when authorized, Joe notifies the human before and after any managed-system mutation. The tool executor enforces a blocking pre-execution notification (cancellable) and a post-execution summary for every mutating action — this is compiled in, not an LLM instruction.

2. **Safety config is outside Joe's reach.** Joe cannot read, write, or influence its own safety configuration. The guarantee is **architectural**: the server process ships no local file tool (Part 2), so no LLM-invokable tool can reach `~/.joe/` (its config, DB, and the `safety-policy.yaml` / `skills-policy.yaml` names) at all. The guarantee rests entirely on the absence of any file tool; a guard of that shape would have to accompany any reintroduced file tool.

3. **The write floor is boot-resolved and runtime-immutable.** Joe resolves a read-only "write floor" once at boot (observation mode or a sticky safe-mode/panic state). Nothing in the running binary can lower it — recovery is a restart, never a live down-transition (`internal/safety/floor.go`, D-0018).

### Policy structure (how humans express trust)

4. **Humans authorize every mutating action.** Joe never executes a managed-system mutation that a human has not explicitly opted into via the safety policy's `act` section. Within an authorized action, Joe may decide *which specific call* fits the situation, but enabling the action is always a human act. **This is the policy shape, not a currently-exercisable mechanism** — as of D-0113 no registered tool carries a policy key the `act` section can grant, so today the opt-in cannot be extended to anything (§3.2).

5. **Default is deny.** Every mutating capability starts disabled. Humans opt in per action, not opt out. An unknown/unregistered tool is classified as a mutation and denied by default. Today the deny is *unconditional* for every registered mutating tool, because none of them has a grantable policy key (§3.2).

6. **Allowlists, not blocklists.** We enumerate what's permitted, never what's forbidden.

---

## Part 1: Current State

### What We Do Right

| Area | Detail |
|------|--------|
| SQL injection | All datastore-query tools use parameterized statements (`?`/`$n` placeholders) via `database/sql` |
| No local mutation surface | The server process ships **no** local file or arbitrary-command tool (Part 2). The only mutating tools it registers are the code-review paths |
| Default bind | The `joe` server binds to `localhost:7777` by default |
| API keys | LLM keys loaded from environment variables, not config files |
| URL encoding | Client uses `url.PathEscape` / `url.QueryEscape` for all HTTP calls |
| Adapters are read-only | K8s, Git, AWS, Azure, observability, datastore, GitOps adapters expose only read/list/describe operations. The only mutating adapter path is code review (GitHub/GitLab comment + request-changes) |
| Action classification | Every tool is classified on a **binary Read/Mutate axis** at registration; the executor gate checks the write floor and the safety policy before every `Execute()` (`internal/safety/tier.go`, `internal/tools/executor.go`) |
| Write floor | Boot-resolved, runtime-immutable read-only floor (observation mode or sticky safe mode); denies every mutate independent of policy or RBAC (`internal/safety/floor.go`, D-0018) |
| Safety policy | Compiled `DefaultPolicy()` (no on-disk file); default-deny for mutating actions, modulated per request only by the task `safety_tier`. No registered tool's policy key resolves to a live `act` field, so the deny is unconditional (§3.2, `internal/safety/policy.go`) |
| Self-protection invariants | Structural, not code-enforced: Joe registers no tool that writes a local file or runs a shell command, so it has no way to reach `~/.joe/` or terminate a process |
| Mutation notification contract | Blocking pre-execution notification with cancel; post-execution summary, for every mutating action (`internal/safety/notifier.go`) |
| API authentication | Edge auth middleware on all `/api/v1/` routes (Bearer service-account tokens; OIDC where configured) (`internal/auth/middleware.go`, `internal/api/authconfig.go`) |
| Identity model | Every authenticated caller resolves to a **principal** — session cookie for humans, service-account key for machines — set in request context by the edge-auth middleware and carried into every authorization decision and audit row (`internal/rbac/identity.go`, `internal/rbac/principals.go`) |
| Request size limits | `http.MaxBytesReader` wraps all request bodies (`internal/api/middleware.go`) |
| RBAC | Zone-scoped access control: zones carry an action ceiling, components are assigned to zones, grants map principals to zones (`internal/rbac/`) |
| Emergency shutdown | Panic/safe-mode persisted to the `cluster_panic_state` DB row; safe mode raises the write floor on the next boot (`internal/store/panic_store.go`, `internal/safety/panic.go`) |
| Secure home resolution | `paths.JoeDirPath()` uses `os/user.Current()` (via `secure_unix.go`/`secure_windows.go`), bypassing the `HOME` env var to prevent path manipulation |

---

## Part 2: Mutation Audit

Every place Joe can change state, by axis. The organizing question is the one the action classification asks: **does this operation mutate the managed system** (live infrastructure + the code/config that governs it)?

### Local file/command tools — none registered

Joe registers **no** local file or arbitrary-command tool on any surface. `internal/tools/` holds exactly two trees: `core/` (tools that reach managed systems through the typed API client) and `shared/` (Go-native read-only diagnostics). `NewCoreRegistry` registers only those two sets (`internal/tools/default.go:25-46`, see the doc comment at `:26-28`), and no constructor for `read_file` / `write_file` / `run_command` / `ask_user` / `local_git_*` exists anywhere in the tree.

The consequence is a bounded attack surface: the server process has **no** tool that writes an arbitrary local file or runs an arbitrary shell command, and therefore no LLM-invokable path to `~/.joe/` or to a process-control verb. The only mutating tools Joe ships are the code-review paths below.

> **These names are unknown to the classifier.** `read_file`, `write_file`, `run_command`, `ask_user`, `local_git_status`, and `local_git_diff` carry no classification entry in `internal/safety/tier.go` and no policy entry in `internal/safety/policy.go`. `ClassifyTool` returns the unknown-tool default for them — **Mutate, deny-by-default** — so even a call fabricated under one of these names is denied before it can dispatch.

### Joe's own model maintenance — classified as Read (not a managed-system mutation)

Per D-0018/D-0019/D-0020, a "write" is an operation that mutates the *managed system*. The tools below only record observed state into Joe's own graph/store — the managed system is in the same state after they run — so on the action axis they are **reads**, not mutations. They are always allowed and never frozen by the write floor or the incident gate (Joe must keep its own model current even in safe mode).

| Tool | Writes to | Classification |
|------|-----------|----------------|
| `graph_add_node`, `graph_add_edge`, `graph_update_node` | Joe's infrastructure graph | Read (idempotent UPSERTs) — **parked**, not registered on `agent:core` (D-0081 pattern) |
| `register_component` | Joe's component store | Read (non-idempotent insert; carries a per-run durability key) |

These tools carry Read rows in `internal/safety/tier.go`. The classification is the load-bearing claim: it is *why* the LLM may maintain Joe's model autonomously without a mutation gate, while still being unable to touch live infrastructure. The three `graph_*` tools retain their classification rows and implementations but are **parked out of the `agent:core` registry** — they bypass the delta-reconcile seam, and being Read-classed they would pass the write floor in observation mode (pinned by `TestGraphWriteToolsAreParked`).

> **D-0020 note.** `register_component` generates row identity server-side and is not idempotent on retry. It declares `NeedsDurability`, so the durable executor persists a per-run idempotency key and dedups a crash-resume replay. This is independent of the Read/Mutate axis — "does this need crash-resume" is a different question than "does this mutate the managed system."

### Managed-system mutations

| Tool | What it changes | `PolicyKey` declared | Resolves to a live `act` field? | Notification |
|------|-----------------|----------------------|-------------------------------|--------------|
| `github_comment` / `gitlab_comment` | External PR/MR thread | `github_comment` / `gitlab_comment` | **No** — denied unconditionally | Before + after |
| `github_request_changes` | External GitHub review (gates merge) | `github_request_changes` | **No** — denied unconditionally | Before + after |

These are the **only** mutating tools the binary registers. They live under `internal/tools/core/` (`github_comment.go`, `gitlab_comment.go`, `github_request_changes.go`) and are wired in `internal/tools/default.go`. Unknown tools default to Mutate and are denied.

**Each of these is denied regardless of configuration.** `ActPolicy` and `IsT3Allowed` (`internal/safety/policy.go`) recognize only `k8s_write`, `pagerduty_ack`, `alertmanager_silence`, and `git_push`; none of the three declared policy keys above is among them, so `IsT3Allowed` falls through to its `default: return false` and the allow branch of `CheckAccess` is never taken. There is no configuration that enables these tools — see §3.2 and `docs/backlog/act-policy-vestigial.md`.

**Enforcement path — a single in-process seam.** These tools reach managed systems through one in-process path: tool executor → in-process core client → RBAC accessor → vendor adapter. There is **no HTTP route that mutates a managed system**, so the agentic tool path is the sole managed-system mutation surface. The two enforcement seams sit at distinct layers and do not overlap:

- The **write floor is checked only in the executor** (`internal/tools/executor.go:215` — floor up + `ActionMutate` → deny). The accessor carries **no** floor check (the `internal/access/` package references no write floor), so the executor is the only place the floor gates a mutation.
- **RBAC and the append-only audit row are decided and written only by the accessor** (`internal/access/access.go:159`, `permit` — "the single enforcement chokepoint"; the audit insert at `:190`). The accessor is the **sole** RBAC gate on this path, and the transport carries no enforcement leg at all: the middleware chain runs CORS → rate limit → metrics → edge auth → session headers → request size limit → mux, with **no per-request RBAC enforcement middleware** in it (`cmd/joe/server.go:1090-1097`). Edge auth resolves the caller principal into request context; the authorization decision itself happens downstream, in the accessor.

The one policy engine both paths share is constructed at the **composition root** and injected, never built downstream: `rbac.NewPolicyEngineWithGovernance(rbacRepo, promoteReadsRepo, readPostureRepo)` at `cmd/joe/server.go:1051` is passed into `api.New`, which hands that same governance-wired engine to both the guarded accessor and the regime handler (`internal/api/server.go:82-91`). There is no `api.New`-internal engine construction. A static guard, `TestGuard_PolicyEngineConstructedOnlyAtCompositionRoot` (`internal/rbac/engine_construction_guard_test.go:34`), fails the build if any `rbac.NewPolicyEngine*` constructor is called outside `cmd/joe` and `_test.go` files — so the accessor cannot silently come to enforce with a differently-wired engine than the transport governs with.

The VCS tools route through the accessor in-process (`internal/api/inproc_client.go`).

### API endpoints that mutate Joe's own state

| Endpoint | Method | Mutation | Authorization |
|----------|--------|----------|---------------|
| `/api/v1/components` | POST | Registers a component **inert** — credential-less, no adapter connected | Admin-gated + audited |
| `/api/v1/components/{id}` | DELETE | Removes a component (+ its credential reference) and unregisters any resident adapter | Admin-gated + audited |
| `/api/v1/components/{id}/promote` | POST | Arms a component (readonly → armed): writes a credential **reference**, performs no Connect/Probe | Admin-gated + audited |
| `/api/v1/admin/*` | various | Zones, policies, read-posture, read-promotions, sessions | Admin-gated + audited |

All `/api/v1/` routes sit behind edge auth middleware; admin routes additionally require the admin gate and write an append-only audit row. Request bodies are limited by `MaxRequestBody` middleware.

### Component lifecycle: inert registration → governed promotion

Creating a component does **not** connect to or register an adapter. `POST /api/v1/components` lands the component **inert** — credential-less by construction (credential-bearing config fields are *rejected* at registration, not silently stripped), assigned to no zone (so it resolves to the read-only `unassigned` zone), with no adapter connected and no credential present (`internal/api/components.go:192-199,247-252`). There is no eager `Connect` probe: a credential-less record cannot authenticate, and connecting at registration would be the attacker-controllable network-call / env-dereference vector the inert landing closes.

Credential entry is owned by a **single governed transition** — the read-only-to-armed promotion (D-0030): `POST /api/v1/components/{id}/promote` → `handlePromoteComponent` (`internal/api/components.go:647-778`). Promotion is admin-gated and audited, writes a credential **reference** into the component's config — an env-var indirection, the in-cluster pod-mounted token, or the Entra client-secret reference (`client_secret_env_var`), never an inline secret — in one fail-closed transaction, and performs **no** credential resolution (no `Connect`, `Resolve`, or `Probe`; whether the reference actually works is a separate explicit admin probe). Re-promoting an armed component overwrites its reference as another gated, audited event, so a credential change is itself governed rather than a delete-and-recreate. A component type with no wired credential provider can never be armed (the first validation after the component loads).

### Adapters

| Adapter family | Can mutate external systems? | Detail |
|----------------|------------------------------|--------|
| K8s, AWS, Azure, datastore, observability, GitOps, networking, security/runtime, registries | **No** | Read/list/describe/query only |
| Git | **No** | `ReadFile`, `ListFiles`, `Log`, `Diff` (can `Pull`, never commit/push) |
| GitHub / GitLab | **Yes** | Comment + request-changes — the only mutating adapter verbs; their tools are denied unconditionally today (§3.2) |

---

## Part 3: Action Safety Framework

This is how Joe handles mutations. It is implemented as a hardcoded enforcement layer, not as LLM instructions or soft guidelines.

### 3.1 Action classification — a binary Read/Mutate axis

Every tool is classified into one of **two** classes (`internal/safety/tier.go`):

| Class | Description | Default | Examples |
|-------|-------------|---------|----------|
| **Read** (`ActionRead`) | Does **not** mutate the managed system. Includes component queries, Joe's own graph/model maintenance, and notifications to humans. | Always allowed, no policy check | `git_log`, `k8s_get`, `graph_query`, `graph_add_node`, `register_component` |
| **Mutate** (`ActionMutate`) | Mutates the managed system (external PR/MR threads; infrastructure/deployments as adapters gain mutate verbs). | Denied. The policy shape provides a per-action opt-in, but no registered tool's key resolves to a live `act` field, so today the denial is unconditional (§3.2) | `github_comment`, `gitlab_comment`, `github_request_changes` |

Per **D-0020** (see also D-0018/D-0019), severity-of-mutation is deliberately *not* encoded as a classification tier — a static blast-radius taxonomy is hard to get right and hard to evaluate on a non-deterministic LLM. Blast-radius safety lives elsewhere (tools, skills, the per-zone/per-capability graduation ladder); the classification carries only the action axis. Operations that mutate Joe's own internal state are Reads on this axis, because they do not change the managed system.

Classification is hardcoded per tool at registration time. The LLM cannot change a tool's class. **Unknown tools are classified Mutate and denied by default.**

### 3.2 Policy (compiled default — no file)

There is **no on-disk safety-policy file** in this build. The runtime policy is
constructed at boot from `DefaultPolicy()` (`internal/safety/policy.go`) and
modulated per request by the task `safety_tier` (`observe` / `record` / `act`),
which can only *restrict* a task below the default, never widen it.
`DefaultPolicy()` is the single source of policy truth: there is no runtime
decode of `SafetyPolicy` anywhere in the binary, so no on-disk file can widen it.

`DefaultPolicy()` denies every managed-system mutation.

**The opt-in seam is structurally intact but currently reachable by no registered tool.** `ActPolicy` still carries per-action toggles and `IsT3Allowed` still switches on them, so the mechanism by which an operator grants a single mutating action is present and unchanged in shape. What is absent is any tool that can be granted: `IsT3Allowed` recognizes exactly `k8s_write`, `pagerduty_ack`, `alertmanager_silence`, and `git_push`, while the three registered Mutate tools declare the keys `github_comment`, `gitlab_comment`, and `github_request_changes`. The two sets are disjoint. Every lookup therefore falls to `default: return false`, the allow branch of `CheckAccess` is unreachable, and **every registered Mutate is denied unconditionally, regardless of how the policy is configured**.

This state of the seam is the standing consequence of **D-0113**. It becomes exercisable again only when full mode ships a tool carrying a live policy key — tracked in [`docs/backlog/act-policy-vestigial.md`](../backlog/act-policy-vestigial.md). Until then, the four surviving `act` fields gate nothing that exists.

Joe also cannot reach `~/.joe/`: the server ships no LLM-invokable file tool
(Part 2), so no code path can open that directory. The guarantee rests entirely
on the absence of any file tool.

> **Inert struct fields.** The `SafetyPolicy` struct *carries* a `record:` section (`graph_mutations`, `source_registration`, `onboarding_facts`, `autonomous_refresh`) that gates nothing: the model-maintenance tools it names are classified Read, so the executor's `CheckAccess` consults only the `act` keys. The four `act` fields are inert in the same sense — no registered tool's `PolicyKey` reaches them (above). The net default-deny guarantee is therefore absolute: every registered mutating tool is denied, with no configuration able to change it (D-0018/D-0019/D-0020, D-0113).

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
   else deny. (Reads always pass.) No registered tool's key resolves to a live
   act field today, so in practice every Mutate reaching this step is denied.
6. Mutate only: blocking pre-execution notification (human can cancel).
7. Execute.
8. Mutate only: post-execution notification (summary of result).
```

The **incident** half of the precedence sits one layer up, in the §C captain-session gate (`internal/captaingate/`), which checks the same floor before its own gate (see §3.4).

**2. Self-protection (structural, not a runtime guard).** Joe registers no tool that writes a local file or runs a shell command, so nothing can reach `~/.joe/` (its config, DB, and the `safety-policy.yaml` / `skills-policy.yaml` names) or terminate a process (`joe`, `kill`, `pkill`, `killall`). The guarantee is structural rather than enforced by a runtime path/command allowlist: with no file or command tool registered on any surface, there is no LLM-invokable path that would need guarding. If such a tool were ever introduced, a compiled-in guard of that shape would have to accompany it.

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
- The posture governs **human-facing transport reads only.** The autonomous `agent:core` read surface (background refresh) is a **separate axis**, governed solely by `auto_promote_read` (per-component-type promotion) plus grants. The two are separated at engine construction: the transport policy engine carries the read-posture resolver (`NewPolicyEngineWithGovernance`); the refresh engine does not (`NewPolicyEngineWithPromote`, posture seam nil). Flipping the posture cannot change what `agent:core` can read (D-0043). The refresh does not bypass the access seam to reach adapters: since A001-COREGOV CC-05 it resolves every component's adapter through `access.ResolveAdapter` under the `agent:core` principal at `ActionRead` (`internal/coreagent/refresh.go:194-216`, `internal/access/access.go:196-231`), so an ungranted/unpromoted component is denied before its adapter — and thus its credential — is resolved, and the refresh read is audited like any other adapter access. The seam **fails closed** if unwired: the background refresh refuses to start, and `resolveAdapter` denies rather than reading the raw registry (CC-08, `internal/coreagent/refresh.go:106-118`). The access seam is in fact the sole governed path to any adapter — a build-failing structural test (`TestInvariant_NoUngovernedAdapterOrGraphAccess`, `internal/api/access_guard_test.go`) forbids any package outside a narrow allowlist (`internal/access`, `internal/coreagent`, `cmd/joe`) from resolving an adapter directly.
- The posture is read **live per request** (no boot cache). Flipping it is an **admin-gated, audited operator act** on `GET`/`POST /api/v1/admin/read-posture`.

### 3.6 Protected security configuration

**Critical invariant:** LLM tools cannot modify Joe's security configuration. This is enforced **architecturally**, not by a table-name guard inside the tool path:

- The security tables — `security_zones`, `component_zone_assignments`, `rbac_policies`, `read_posture`, `auto_promote_reads`, the admin-principal set — are mutated **only** through the admin REST surface (`/api/v1/admin/*`), which is admin-gated and writes an append-only audit row.
- The LLM's tools have **no raw-SQL write path** to those tables. Joe's model-maintenance tools (`register_component`, and the parked `graph_add_*`) write only to operational tables (graph, components) via their typed repositories.
- The safety policy and the `~/.joe/` directory are self-protected from the file tools (§3.3).

So an LLM — even under prompt injection — has no tool that reaches the security configuration; changing it requires an authenticated admin call on a surface no LLM tool can invoke.

### 3.7 Roadmap mutations (not yet built)

The executor today enforces the floor, zone/namespace scope, the safety policy, and the notification contract. The following deterministic controls are **designed but not yet implemented**; they are tracked in Part 5 and must not be assumed active:

- **Environment-level operation blocking** — deterministic pattern-matching that blocks namespace/cluster-scoped destructive operations (e.g. `kubectl delete namespace`, `--all` selectors, `terraform destroy`) regardless of policy flags, unless the specific environment is allow-listed.
- **Mutation circuit breaker** — a rolling-window rate limiter on mutating actions that trips after a threshold and suspends further mutations until a human resets it.
- **Credential isolation enforcement** — validation that Joe never uses caller-provided infrastructure credentials, only its own pre-scoped service account.

These become relevant as Joe gains infrastructure-mutating adapters (Part 6). Until they land, the live guarantees are: managed-system mutation is denied — unconditionally, since no registered mutating tool has a grantable policy key (§3.2) — floor-blocked in observation/safe mode, scope-checked against zones, and announced before and after.

---

## Part 4: Gaps by Layer (non-mutation security)

### Layer 1: Network boundary (`joe` HTTP API)

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
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
| Prompt injection | CRITICAL | Open | User/infra text flows unsanitized into LLM context. **Mitigated** by the architecture: even a fully-injected LLM has no tool that mutates the managed system — every registered Mutate is denied unconditionally today (§3.2) — and no tool that reaches security config; but the input channel itself is unsanitized |

**Key files:** `internal/tools/default.go` (registration; omits the local set), `internal/tools/core/` (the live tool surface)

### Layer 4: Authorization

| Gap | Severity | Status | Detail |
| --- | -------- | ------ | ------ |
| Audit logging | — | **Present** | Admin acts and infra access write append-only audit rows (`internal/audit/`) |

---

## Part 5: Implementation Roadmap

### Action Safety Framework — COMPLETE

- Binary Read/Mutate classification with a hardcoded registry: `internal/safety/tier.go`
- Compiled default safety policy (no on-disk file): `internal/safety/policy.go` (`DefaultPolicy`)
- Executor gate (floor → scope → policy → notify) before every `Execute()`: `internal/tools/executor.go`
- Default-deny for unknown tools (classified Mutate)
- Self-protection is structural: no file/command tool is registered, so there is nothing to guard
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
- Response security headers; TLS-by-default for non-localhost binds

---

## Part 6: Infrastructure Healing — From Observer to Operator (design direction)

The Action Safety Framework is not just a wall against mutations — it's a **gate** that humans open incrementally as trust builds. Joe is designed to evolve from a read-only observer into an active infrastructure healer, under explicit human control. Today Joe ships as a read-first copilot; the healing actions below are the forward direction, each to be gated by a per-action `act` policy key, the write floor, zone scope, and (in incident regime) the captain gate.

**This progression describes a design direction, not a mechanism an operator can exercise today.** The opt-in seam exists in the policy shape, but as of D-0113 no registered tool carries a key it can grant (§3.2), so there is currently no stage-3 action a human could enable even if they wanted to. Stages 3 and 4 below become reachable when full mode ships tools carrying live policy keys.

This is how the Core Safety Principles compose: invariants 1–3 hold at every stage of the progression, while policy structure 4–6 is the shape humans will use to move Joe along it.

### 6.1 Trust progression

```
Stage 1: Observe                 Stage 2: Read-only infra tools
─────────────────                ──────────────────────────────
Joe reads everything,            Human registers more components. Each core
changes nothing.                 tool is read-only by construction (it exposes
"Why is payment-api slow?"       no mutate verb), so Joe queries infra
→ Joe explains the cause.        directly, still mutating nothing.

Stage 3: Supervised healing      Stage 4: Autonomous healing
  (not yet reachable)              (not yet reachable)
───────────────────────────      ────────────────────────────
Human enables specific           Human enables broader mutating actions.
mutating actions (e.g.           Joe heals known patterns without prompting,
k8s_scale). Joe announces        still announcing before and after. Audit
before acting; the human         log captures everything; the circuit breaker
can cancel.                      (§3.7) bounds runaway sequences.
```

Joe ships at stage 2. Stages 3 and 4 require tools carrying live `act` policy keys, of which there are currently none (§3.2).

At every stage Joe decides *which specific action* to take within the classes humans have authorized. The class boundary is always human-authored; the situational judgment within it is Joe's (principle 4).

### 6.2 Healing actions by adapter (planned)

When humans choose to enable healing, each action is individually toggled — there is no "enable all mutations" switch. Examples of planned mutating actions, each with its own `act` policy key: K8s scale/restart/cordon/apply; Alertmanager silence; PagerDuty ack/resolve; Git commit/push; Helm upgrade. The code-review mutations (Part 2) are the only mutating tools implemented so far, and they are **not** enabled — they declare policy keys no `act` field resolves, so they are denied unconditionally (§3.2). No mutating action is enableable today.

### 6.3 Graph-driven healing

Joe's healing is informed by the infrastructure graph, not blind command execution. Before acting, Joe traverses relationships to understand blast radius — e.g. recognizing that scaling a service won't help when the real cause is a saturated database it depends on. This is the difference between Joe and a runbook executor: Joe reasons about *why* something is broken and whether a proposed fix will actually help.

### 6.4 Healing safety guarantees

Even with healing enabled, the hardcoded invariants remain: every mutating action announces before and after; each mutating action is a separate adapter-backed tool with its own narrow verb (there is no arbitrary-command tool through which extra verbs could be smuggled in); self-protection invariants never change; per-action granularity means each mutation is a separate trust decision; and the write floor blocks every mutation in observation or safe mode regardless of which actions are enabled. The environment-level block and circuit breaker (§3.7) are the planned additions that bound blast radius and runaway sequences.

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

> Observation mode raises the same floor with reason `observation` — a calm, intended read-only resting posture (not panic) — and is Joe's **day-one boot default** when `JOE_MODE` is unset (or set to `observation`; `full` is refused as not-yet-implemented, D-0073). A present panic state wins over observation.

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
| `graph_*` | ✅ | ✅ | Model-maintenance tools (Read class) |
| `security_zones` | — | ❌ **never** | Admin API only (audited) |
| `component_zone_assignments` | — | ❌ **never** | Admin API only (audited) |
| `rbac_policies` | — | ❌ **never** | Admin API only (audited) |
| `read_posture`, `auto_promote_reads` | — | ❌ **never** | Admin API only (audited) |
| audit rows | — | append-only | Audit layer |

> There is intentionally **no** `CanWriteTable`/`writeProtectedTables` guard inside the tool executor, and such a guard is not the mechanism protecting these tables. Protection comes from the tool surface itself: the LLM's tools write only operational data through typed repositories, and security configuration is a different surface (admin REST, admin-gated, audited).

### 8.3 Deployment

Joe runs as a **single `joe` process** with one SQLite database. Security enforcement (write floor, safety policy, zone RBAC, read posture, incident gate) is in-process. The admin REST surface is admin-gated and audited.

> There is **no** split-process hardened deployment mode: no separate security process, no second `security.db`, and no `cmd/joe-security` or `internal/securitysvc` in the tree. Security enforcement is in-process, as described above. If a future hardened split is pursued, it gets its own decision entry.

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
- [`docs/project/DECISIONS.md`](../project/DECISIONS.md) — normative decision log (D-0018 write floor / panic; D-0020 binary axis; D-0022 denial precedence; D-0041/D-0043 read posture)
- [`docs/reference/joe-architecture.md`](joe-architecture.md) — system architecture
