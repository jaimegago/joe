# Security in Layers — Joe

This document captures Joe's security posture, known gaps, remediation plan, and — critically — the **Action Safety Framework** that governs what Joe is allowed to change and under what conditions.

This is a living document — update it as fixes land.

---

## Core Safety Principles

These are non-negotiable and must be hardcoded (not bypassable by LLM or configuration that Joe itself can reach):

1. **Humans own all mutation decisions.** Joe must never change infrastructure, files, or configuration without explicit human authorization.
2. **Joe must always announce changes.** Even when authorized, Joe must notify the human before and after any mutation. Silent mutations are a bug.
3. **Safety config is outside Joe's reach.** Joe must not be able to read, write, or influence its own safety configuration. The policy files live outside Joe's tool sandbox.
4. **Default is deny.** Every mutation capability starts disabled. Humans opt in per-action-class, not opt out.
5. **Allowlists, not blocklists.** We enumerate what's permitted, never what's forbidden.

---

## Part 1: Current State (as of Phase 5.5)

### What We Do Right

| Area | Detail |
|------|--------|
| SQL injection | All queries use parameterized statements (`?` placeholders) via `database/sql` |
| Command injection | `run_command` uses `exec.CommandContext` (no shell), plus allowlist of permitted commands |
| Default bind | joecored binds to `localhost:7777` by default |
| API keys | LLM keys loaded from environment variables, not config files |
| Output limits | `run_command` truncates stdout/stderr to 256 KB; `read_file` caps at 1 MB |
| URL encoding | Client uses `url.PathEscape` / `url.QueryEscape` for all HTTP calls |
| Adapters are read-only | K8s, Git, AWS adapters expose only read/list/describe operations — no create/delete/update |
| Core tools are read-only | All tools that call joecored via HTTP are query-only |
| Action tier enforcement | Every tool classified T1/T2/T3; executor gate checks policy before every `Execute()` (`internal/safety/tier.go`, `internal/tools/executor.go`) |
| Safety policy | Loaded once at startup from `~/.joe/safety-policy.yaml`; immutable at runtime; default-deny for T3 (`internal/safety/policy.go`) |
| Self-protection invariants | Joe cannot read/write `~/.joe/`, cannot run joe/joecored/kill — hardcoded, no override (`internal/safety/invariants.go`) |
| Path sandboxing | `write_file` enforces `allowed_directories` from safety policy; symlink-aware, case-insensitive on macOS/Windows |
| Subcommand allowlists | kubectl/helm/argocd restricted to read-only subcommands; compiled-in, not configurable by LLM (`internal/tools/local/runcmd/subcommands.go`) |
| T3 notification contract | Blocking 3-second countdown before T3 actions with Ctrl+C cancel; post-execution summary for T2/T3 (`internal/repl/notifier.go`) |
| API authentication | Bearer token middleware on all `/api/v1/` routes; configurable via `server.api_key` or `JOE_API_KEY` env (`internal/api/middleware.go`) |
| Request size limits | `http.MaxBytesReader` wraps all request bodies (default 1 MB) (`internal/api/middleware.go`) |
| Secure home resolution | `paths.JoeDirPath()` uses `os/user.Current()` (via `secure_unix.go`/`secure_windows.go`), bypasses `HOME` env var to prevent path manipulation |

---

## Part 2: Full Mutation Audit

Every component that can change state, classified by blast radius.

### Local Tools (joe binary — User Agent side)

| Tool | Can Mutate? | What It Changes | Authorization | Notification | Blast Radius |
|------|-------------|-----------------|---------------|--------------|--------------|
| `write_file` | **YES** | Files within `allowed_directories` only | T3 policy gate + path sandbox | T3: before (blocking) + after | **Local filesystem (sandboxed)** |
| `run_command` | **YES** | Depends on allowlist + subcommand validation | T3 policy gate + command allowlist + subcommand allowlist | T3: before (blocking) + after | **Local + remote infra (restricted)** |
| `read_file` | No | — | T1 (always allowed) | — | Read-only (blocks `~/.joe/`) |
| `local_git_status` | No | — | T1 | — | Read-only |
| `local_git_diff` | No | — | T1 | — | Read-only |
| `echo` | No | — | T1 | — | Harmless |
| `ask_user` | No | — | T1 | — | Harmless |

**`write_file` detail** (`internal/tools/local/writefile/writefile.go`):

- T3 action — disabled by default in safety policy.
- Self-protection: blocks `~/.joe/` (hardcoded invariant, symlink-aware, case-insensitive).
- Path sandboxing: if `allowed_directories` configured in policy, writes restricted to those dirs only.
- T3 notification: 3-second blocking countdown in REPL before execution, Ctrl+C to cancel.

**`run_command` detail** (`internal/tools/local/runcmd/runcmd.go`):

- Default allowlist (read-only only): `ls, cat, head, tail, grep, find, wc`.
- kubectl/helm/argocd **excluded** from default allowlist — must be explicitly added in `safety-policy.yaml`.
- When enabled, mutation-capable commands enforce **compiled-in subcommand allowlists** (`internal/tools/local/runcmd/subcommands.go`):
  - kubectl: get, describe, logs, top, explain, api-resources, api-versions, version, cluster-info, config, auth
  - helm: list, status, get, history, show, search, version, env, repo, template, dependency
  - argocd: app list/get/diff/manifests/logs/history/resources, cluster/repo/version (read-only)
- Self-protection: `joe`, `joecored`, `kill`, `pkill`, `killall` blocked (hardcoded invariant).
- Flag-aware parsing: leading flags (e.g., `-n default`) are skipped to find the actual subcommand.

### Core Agent Tools (joecored — autonomous)

| Tool | Can Mutate? | What It Changes | Authorization | Notification |
|------|-------------|-----------------|---------------|--------------|
| `graph_add_node` | **YES** | Graph store | None | None |
| `graph_add_edge` | **YES** | Graph store | None | None |
| `graph_update_node` | **YES** | Graph store | None | None |
| `register_source` | **YES** | Source store | None | None |
| `save_onboarding_fact` | **YES** | Fact store | None | None |

All defined in `internal/coreagent/agent.go:119-499`. The LLM calls these freely during onboarding and .joe/ file processing.

### Graph Mutations (indirect paths)

| Path | Trigger | What It Changes | Authorization | Notification |
|------|---------|-----------------|---------------|--------------|
| Refresh loop | Timer (5 min) | Adds/removes nodes and edges to match infra state | None (autonomous) | None |
| `.joe/` file processing | Git refresh | LLM interprets YAML → graph mutations | None | None |
| Clarification answers | Human answers question | Applies stored graph operations | Implicit (human answered) | None |
| Onboarding | Human triggers via API | LLM generates graph mutations | None | None |
| Graph delta reconciliation | Refresh | Deletes stale nodes/edges | None (autonomous) | None |

Key files: `internal/coreagent/graphdelta.go:118-145`, `internal/coreagent/joefile_service.go`, `internal/coreagent/git_refresh.go`

### API Endpoints That Mutate State

| Endpoint | Method | Mutation | Authorization | Notification |
|----------|--------|----------|---------------|--------------|
| `/api/v1/sources` | POST | Creates source + registers adapter | Bearer token (if configured) | None |
| `/api/v1/sources/{id}` | DELETE | Removes source + disconnects adapter | Bearer token (if configured) | None |
| `/api/v1/clarifications/{id}/answer` | POST | Marks answered + applies graph ops | Bearer token (if configured) | None |
| `/api/v1/clarifications/{id}/dismiss` | POST | Marks dismissed | Bearer token (if configured) | None |
| `/api/v1/onboarding` | POST | Triggers LLM → graph mutations | Bearer token (if configured) | None |
| `/api/v1/refresh` | POST | Triggers full graph reconciliation | Bearer token (if configured) | None |

All endpoints are behind `BearerAuth` middleware when `server.api_key` is set. Request bodies limited to 1 MB via `MaxRequestBody` middleware.

### Adapters (joecored backend)

| Adapter | Can Mutate External Systems? | Detail |
|---------|------------------------------|--------|
| K8s | **No** | Only `ListResources`, `GetResource`, `GetPodLogs` |
| Git | **No** | Only `ReadFile`, `ListFiles`, `Log`, `Diff`. Can `Pull` but not commit/push |
| AWS | **No** | Only `Describe*` and `List*` API calls |

Adapters are currently safe. But future adapters (Phase 6+) for Prometheus, PagerDuty, Alertmanager could introduce mutations (create silence, acknowledge incident, etc.).

---

## Part 3: Action Safety Framework

This is the design for how Joe handles mutations. It must be implemented as a hardcoded enforcement layer, not as LLM instructions or soft guidelines.

### 3.1 Action Classification

Every tool and API action is classified into one of three tiers:

| Tier | Label | Description | Default | Example |
|------|-------|-------------|---------|---------|
| **T1** | **Observe** | Read-only. Cannot change any state. | Allowed | `read_file`, `git_log`, `k8s_get`, `graph_query` |
| **T2** | **Record** | Changes Joe's internal state (graph, facts, sources). Does not touch external systems. | Requires opt-in | `graph_add_node`, `register_source`, `save_onboarding_fact` |
| **T3** | **Act** | Changes external systems (files, infrastructure, deployments). | Denied by default, per-action opt-in | `write_file`, `run_command(kubectl)`, future: `k8s_scale`, `pagerduty_ack` |

Classification is hardcoded per tool at registration time. The LLM cannot change a tool's tier.

### 3.2 Policy File

Policy lives in a file that Joe **cannot access**:

```
~/.joe/safety-policy.yaml       # Human-editable only
```

This path must be:
- **Excluded** from `read_file` and `write_file` allowed directories
- **Not readable** by any tool the LLM can invoke
- **Loaded once** at startup by joe/joecored, not re-readable at runtime by the agent

```yaml
# ~/.joe/safety-policy.yaml
version: 1

# T2: Internal state mutations
record:
  graph_mutations: true          # Allow graph add/update/delete
  source_registration: true      # Allow registering new sources
  onboarding_facts: true         # Allow saving onboarding facts
  autonomous_refresh: true       # Allow background refresh to mutate graph

# T3: External system mutations — each must be explicitly enabled
act:
  write_file:
    enabled: false
    allowed_directories: []      # e.g., ["/tmp/joe-workspace"]

  run_command:
    enabled: true
    allowed_commands:             # Override the compiled-in allowlist
      - ls
      - cat
      - head
      - tail
      - grep
      - find
      - wc
    # kubectl, helm, argocd deliberately excluded from default
    # To enable: add them here and accept the risk

  # Future adapters
  k8s_write:
    enabled: false               # scale, apply, delete
  pagerduty_ack:
    enabled: false
  alertmanager_silence:
    enabled: false
  git_push:
    enabled: false
```

### 3.3 Hardcoded Enforcement Points

These checks are compiled into the binary. They cannot be bypassed by configuration, LLM reasoning, or prompt injection.

**1. Tool executor gate** (`internal/tools/` — both local and core):

```
Before every tool.Execute():
  1. Look up tool's tier (T1/T2/T3)
  2. If T1: proceed
  3. If T2: check policy.record.<category> == true, else reject
  4. If T3: check policy.act.<tool>.enabled == true, else reject
  5. If T3 and enabled: notify human BEFORE execution
  6. After execution: notify human of result
```

**2. Self-protection invariants** (hardcoded, no config override):

| Invariant | Enforcement |
|-----------|-------------|
| Joe cannot read its own safety policy | `~/.joe/safety-policy.yaml` excluded from all file tool paths at compile time |
| Joe cannot write to `~/.joe/` | `~/.joe/` excluded from `write_file` allowed directories at compile time |
| Joe cannot modify its own config | `~/.joe/config.yaml` excluded from `write_file` at compile time |
| Joe cannot run joe/joecored commands | `joe` and `joecored` excluded from `run_command` allowlist at compile time |
| Joe cannot kill its own process | `kill`, `pkill`, `killall` excluded from `run_command` at compile time |

These are **not configurable**. They are constants in the source code.

**3. Notification contract** (hardcoded for all T2 and T3 actions):

```
For T3 (Act) actions:
  BEFORE: "[Joe] I'm about to <action>. Proceeding in 3s... (Ctrl+C to cancel)"
  AFTER:  "[Joe] Done: <action summary with details>"

For T2 (Record) actions:
  AFTER:  "[Joe] Updated graph: added node service/payment-api" (in session log)
```

T3 notifications are **blocking** — the human sees them in the REPL and can interrupt. This is not a soft guideline for the LLM; it's enforced by the tool executor before calling `Execute()`.

### 3.4 run_command Argument Validation

Even when a command is in the allowlist, certain argument patterns must be blocked for mutation-capable commands. This applies if/when a human enables `kubectl`, `helm`, or `argocd`:

```yaml
# Hardcoded argument blocklist for kubectl (when enabled)
kubectl:
  blocked_subcommands:
    - delete
    - apply
    - patch
    - scale
    - drain
    - cordon
    - taint
    - edit
    - replace
    - rollout
  allowed_subcommands:          # Allowlist mode — only these work
    - get
    - describe
    - logs
    - top
    - explain
    - api-resources
    - version
```

The default for mutation-capable commands is **subcommand allowlist mode**: only explicitly permitted subcommands are allowed, everything else is denied.

### 3.5 Future Adapter Mutations (Phase 6+)

When we add adapters that can mutate external systems, each mutation method must:

1. Be registered as a T3 action
2. Have a corresponding `policy.act.<adapter>_<action>.enabled` flag
3. Default to `false`
4. Enforce the notification contract
5. Be individually configurable (not blanket "enable all PagerDuty actions")

Examples of future T3 actions:

| Adapter | Action | Policy Key |
|---------|--------|------------|
| K8s | Scale deployment | `act.k8s_scale` |
| K8s | Apply manifest | `act.k8s_apply` |
| PagerDuty | Acknowledge incident | `act.pagerduty_ack` |
| PagerDuty | Resolve incident | `act.pagerduty_resolve` |
| Alertmanager | Create silence | `act.alertmanager_silence` |
| Git | Commit | `act.git_commit` |
| Git | Push | `act.git_push` |

### 3.6 Environment-Level Operation Blocking

Operations that affect an entire namespace or cluster are categorically more dangerous than single-resource mutations. A single `kubectl delete namespace payments` has unbounded blast radius compared to `kubectl delete pod payment-svc-7f8b9c-x2k4`.

**Deterministic rule (hardcoded, not LLM-instructed):**

Any operation that targets an entire namespace, cluster, or environment is **always blocked** unless the specific environment is explicitly allow-listed in the safety policy. This applies regardless of the user's RBAC permissions or the action's T3 policy flag.

**Detection heuristics (compiled-in):**

| Pattern | Example | Action |
|---------|---------|--------|
| Namespace-scoped delete without resource name | `kubectl delete namespace payments` | **BLOCKED** |
| Wildcard resource selectors | `kubectl delete pods --all -n payments` | **BLOCKED** |
| Environment recreation | `terraform destroy` on a workspace | **BLOCKED** |
| Bulk resource deletion | Operation affecting >N resources (configurable threshold) | **BLOCKED** unless allow-listed |

**Policy configuration:**

```yaml
# In ~/.joe/safety-policy.yaml
environment_operations:
  # Maximum resources a single operation can affect
  max_blast_radius: 10

  # Environments where environment-level operations are allowed (empty = none)
  # Even when allow-listed, T3 notification contract still applies
  allow_environment_ops:
    - dev        # Only dev environments can be fully recreated
    # staging and prod are never allow-listed for bulk operations
```

**Key distinction from `MaxResourcesAffected`:** The blast radius limit (`max_blast_radius`) is a hard cap on any single tool execution. Environment-level blocking goes further — it pattern-matches on the *intent* of the operation (targeting a whole namespace/cluster) and blocks it even if the current resource count is below the threshold.

### 3.7 Mutation Rate Limiting (Circuit Breaker)

An LLM in a reasoning loop can chain multiple destructive actions faster than a human can review them. Even with T3 notifications, a runaway sequence of mutations (e.g., scale down service A → delete configmap B → restart deployment C) can cause cascading damage before the human processes the first notification.

**Deterministic rule (hardcoded):**

A circuit breaker tracks mutation frequency. If more than N mutations occur within M minutes, all further mutations are suspended until a human explicitly resets the breaker. This is independent of and in addition to the per-action T3 approval flow.

**Behavior:**

```
Mutation 1: kubectl scale deployment/payment-svc --replicas=5
  → T3 notification → human approves → executes → counter: 1

Mutation 2: kubectl rollout restart deployment/payment-svc
  → T3 notification → human approves → executes → counter: 2

Mutation 3: kubectl delete configmap payment-config
  → T3 notification → human approves → executes → counter: 3

Mutation 4: (within window)
  → CIRCUIT BREAKER TRIPPED
  → "⚠ Safety: 3 mutations executed in 2 minutes.
     Further mutations suspended. Review recent changes
     and type 'joe safety reset' to continue."
```

**Policy configuration:**

```yaml
# In ~/.joe/safety-policy.yaml
circuit_breaker:
  max_mutations: 5           # Max T3 mutations within the window
  window_minutes: 10         # Rolling window duration
  cooldown_minutes: 5        # Minimum wait after trip before reset
  auto_reset: false          # Require manual reset (recommended)
```

**Implementation notes:**
- Counter tracks T3 (Act) actions only, not T2 (Record)
- Counter is per-session in joe local, global in joecored
- The breaker trips *before* the Nth+1 action executes, not after
- `joe safety status` shows current counter and window
- `joe safety reset` resets the counter (requires interactive confirmation)

### 3.8 Credential Isolation (No Permission Inheritance)

AI agents must never inherit the calling user's infrastructure credentials directly. If a user has cluster-admin on a production cluster and Joe inherits that credential, Joe can do anything the user can — including operations that Joe's safety policy is designed to prevent.

**Deterministic rule (architectural constraint):**

Joe's service account credentials are separate from and more restrictive than the user's credentials. Joe never proxies the user's kubeconfig, cloud credentials, or git tokens for infrastructure operations. Instead:

1. **joecored uses its own service account** with pre-scoped permissions
2. **RBAC maps user intent to Joe's capabilities**, not to the user's permissions
3. **Joe's credentials are configured by operators**, not inherited at runtime

**What this means in practice:**

```
WRONG (Kiro model):
  User has cluster-admin → AI inherits cluster-admin → AI can delete anything

RIGHT (Joe model):
  User has cluster-admin (their own kubectl still works)
  User talks to Joe → Joe uses joe-svc-account (read-only + scoped mutations)
  Joe's RBAC checks: "Can this user ask Joe to do X?"
  Joe's Safety checks: "Is X permitted by policy?"
  Joe's credentials: "Can Joe's service account actually execute X?"
```

**Three independent permission boundaries:**

| Boundary | Controls | Enforcement |
|----------|----------|-------------|
| User's RBAC | What the user can *ask* Joe to do | Middleware (pre-LLM) |
| Safety Policy | What Joe is *allowed* to do | Executor gate (post-LLM) |
| Joe's Service Account | What Joe *can* do at the infrastructure level | Cloud/K8s IAM |

All three must allow an operation for it to proceed. This is defense in depth — even if RBAC is misconfigured, Joe's service account permissions limit the blast radius. Even if the service account is over-provisioned, the safety policy blocks unauthorized mutations.

**Configuration guidance:**

Joe's service account should follow least-privilege:
- K8s: `ClusterRole` with only the verbs Joe needs (get, list, watch by default; scale, patch only for enabled healing actions)
- AWS: IAM role with only Describe/List permissions (no Create/Delete/Modify unless explicitly needed)
- Git: Read-only deploy key (no push unless `act.git_push` is enabled and a separate push-capable key is configured)

---

## Part 4: Gaps by Layer (non-mutation security)

### Layer 1: Network Boundary (joecored HTTP API)

| Gap | Severity | Status | Detail |
| ----- | -------- | ------ | ------ |
| ~~No authentication~~ | ~~CRITICAL~~ | **FIXED** | Bearer token middleware on all `/api/v1/` routes (`internal/api/middleware.go`) |
| No rate limiting | HIGH | Open | No middleware to prevent DoS or brute-force |
| ~~No request size limits~~ | ~~HIGH~~ | **FIXED** | `http.MaxBytesReader` wraps all request bodies, default 1 MB (`internal/api/middleware.go`) |
| Plaintext HTTP | HIGH | Open | joe↔joecored traffic unencrypted; credentials in transit exposed |
| No CORS / security headers | MEDIUM | Open | Missing `X-Content-Type-Options`, `X-Frame-Options`, etc. |

**Key files:** `cmd/joecored/main.go`, `internal/api/server.go`, `internal/api/middleware.go`

### Layer 2: Credential Storage

| Gap | Severity | Status | Detail |
| ----- | -------- | ------ | ------ |
| Plaintext credentials in DB | CRITICAL | Open | `sources.config` JSON column stores AWS keys, Git tokens unencrypted |
| SSH keys with empty passphrase | HIGH | Open | `git.go:158` loads SSH keys with `""` passphrase |
| No credential rotation | MEDIUM | Open | Static credentials never refreshed or expired |
| World-readable config | MEDIUM | Open | `~/.joe/config.yaml` uses default umask (typically 0644) |

**Key files:** `internal/adapters/git/git.go:169`, `internal/adapters/aws/aws.go:256-257`

### Layer 3: LLM Tool Sandbox

| Gap | Severity | Status | Detail |
| ----- | -------- | ------ | ------ |
| ~~No path sandboxing on `write_file`~~ | ~~CRITICAL~~ | **FIXED** | `write_file` enforces `allowed_directories` from safety policy + blocks `~/.joe/` |
| No path sandboxing on `read_file` | HIGH | Open | LLM can read any file the process user can access (except `~/.joe/`) |
| Prompt injection | CRITICAL | Open | User input flows unsanitized into LLM context, which then calls tools |

**Key files:** `internal/tools/local/readfile/readfile.go`, `internal/tools/local/writefile/writefile.go`, `internal/safety/invariants.go`

### Layer 4: Authorization

| Gap | Severity | Status | Detail |
| ----- | -------- | ------ | ------ |
| No per-source access control | HIGH | Open | Any client can query any infrastructure source |
| No user identity model | HIGH | Open | No concept of who is making a request |
| No audit logging | MEDIUM | Open | No record of who did what, when |

---

## Part 5: Implementation Roadmap

### Phase 5.5: Action Safety Framework — COMPLETE

All six steps implemented and tested. Coverage: safety 95.4%, middleware 100%, tools 97.1%.

**1. Action Safety Framework — core enforcement** ✅

- Action tier classification (T1/T2/T3) with hardcoded registry: `internal/safety/tier.go`
- Safety policy loader from `~/.joe/safety-policy.yaml`: `internal/safety/policy.go`
- Executor gate checks tier + policy before every `Execute()`: `internal/tools/executor.go`
- Default-deny for unknown tools (classified as T3)

**2. Self-protection invariants + path sandboxing** ✅

- `~/.joe/` blocked for all read/write operations (hardcoded, symlink-aware, case-insensitive): `internal/safety/invariants.go`
- `write_file` enforces `allowed_directories` from safety policy: `internal/tools/local/writefile/writefile.go`
- `..` traversal normalized via `filepath.Clean`, symlinks resolved via `filepath.EvalSymlinks`
- Secure home directory resolution bypasses `HOME` env var: `internal/paths/secure_unix.go`, `internal/paths/secure_windows.go`

**3. run_command subcommand validation** ✅

- kubectl/helm/argocd removed from default allowlist (read-only commands only)
- Compiled-in subcommand allowlists for mutation-capable commands: `internal/tools/local/runcmd/subcommands.go`
- Flag-aware parsing skips leading flags to find actual subcommand
- `joe`, `joecored`, `kill`, `pkill`, `killall` blocked by hardcoded invariant

**4. T3 notification contract** ✅

- `ActionNotifier` interface with `NotifyBefore`/`NotifyAfter`: `internal/safety/notifier.go`
- REPL notifier with 3-second blocking countdown + Ctrl+C cancel: `internal/repl/notifier.go`
- Executor calls `NotifyBefore` for T3 (blocking), `NotifyAfter` for T2 and T3
- `NoopNotifier` for tests, `LogNotifier` for joecored

**5. API key authentication** ✅

- `BearerAuth` middleware on all `/api/v1/` routes: `internal/api/middleware.go`
- Token from `config.yaml` `server.api_key` or `JOE_API_KEY` env var
- Client sends `Authorization: Bearer <token>` on all requests: `internal/client/client.go`
- Graceful degradation: auth disabled when no key configured (logs warning)

**6. Request size limits** ✅

- `MaxRequestBody` middleware wraps all request bodies with `http.MaxBytesReader`: `internal/api/middleware.go`
- Default limit: 1 MB (`DefaultMaxRequestBytes`)
- `Chain()` helper composes middleware in order

### Do in Phase 6

**7. Credential encryption at rest**
**8. TLS support for joe↔joecored**
**9. Rate limiting middleware**
**10. Security headers**
**11. Mutation audit log** — structured log of all T2/T3 actions with timestamp, tool, args, result
**12. Environment-level operation blocking** — deterministic detection and blocking of namespace/cluster-scoped destructive operations (§3.6)
**13. Mutation circuit breaker** — rolling window rate limiter on T3 actions with manual reset (§3.7)
**14. Credential isolation enforcement** — validate that joecored never uses user-provided credentials for infrastructure operations; service account separation (§3.8)

### Do in Phase 9 (multi-user)

**12. RBAC / per-source authorization**
**13. Per-user safety policies**
**14. Credential rotation**

---

## Part 6: Infrastructure Healing — From Observer to Operator

The Action Safety Framework is not just a wall against mutations — it's a **gate** that humans open incrementally as trust builds. Joe is designed to evolve from a read-only observer into an active infrastructure healer, under explicit human control.

### 6.1 Trust Progression

Joe's relationship with infrastructure follows a natural progression:

```
Stage 1: Observe (T1)          Stage 2: Read-Only Tools (T3 read)
─────────────────────          ─────────────────────────────────
Joe reads everything,          Human enables kubectl/helm/argocd
changes nothing.               in safety-policy.yaml. Subcommand
"Why is payment-api slow?"     allowlists restrict to get/describe/
→ Joe explains the cause.      logs. Joe queries infra directly.

Stage 3: Supervised Healing    Stage 4: Autonomous Healing
───────────────────────────    ────────────────────────────
Human enables specific T3      Human enables broader T3 actions.
mutation actions (e.g.,        Joe heals known patterns without
k8s_scale). Joe announces      prompting, still announces before
before acting, human can       and after. Audit log captures
Ctrl+C to cancel.              everything.
```

### 6.2 Healing Actions by Adapter

When humans choose to enable healing, each action is individually toggled. There is no "enable all mutations" switch.

| Adapter | Healing Action | Policy Key | What Joe Does |
| ------- | -------------- | ---------- | ------------- |
| **K8s** | Scale deployment | `act.k8s_scale` | Increase replicas when CPU/memory pressure detected |
| **K8s** | Restart rollout | `act.k8s_restart` | `kubectl rollout restart` for stuck deployments |
| **K8s** | Cordon node | `act.k8s_cordon` | Isolate unhealthy node, let pods reschedule |
| **K8s** | Apply manifest | `act.k8s_apply` | Apply known-good config from git |
| **Alertmanager** | Create silence | `act.alertmanager_silence` | Silence alert while fixing root cause |
| **PagerDuty** | Acknowledge | `act.pagerduty_ack` | Acknowledge incident while investigating |
| **PagerDuty** | Resolve | `act.pagerduty_resolve` | Resolve incident after confirming fix |
| **Git** | Commit | `act.git_commit` | Commit config changes to repo |
| **Git** | Push | `act.git_push` | Push fix to trigger GitOps pipeline |
| **Helm** | Upgrade | `act.helm_upgrade` | Upgrade release with new values |

### 6.3 Graph-Driven Healing

Joe's healing is not blind command execution — it's informed by the knowledge graph. Before acting, Joe traverses relationships to understand blast radius:

```
Example: "payment-api is slow"

Joe's graph traversal:
  payment-api (service)
    ──runs_on──► payment-deploy (deployment, 2 replicas, 95% CPU)
      ──runs_on──► node-ip-10-0-1-42 (node, healthy)
    ──depends_on──► payment-db (RDS, 100% CPU)
      ──metrics_in──► prometheus (high query latency)
    ──paged_via──► pagerduty (incident #4521, unacked)

Joe's reasoning:
  "payment-api is slow because payment-db RDS is at 100% CPU.
   Scaling payment-api replicas won't help — it'll add more
   load to the already-saturated database. The root cause is
   likely the missing index from last week's migration.

   What I can do now:
   1. Acknowledge PagerDuty incident #4521 (act.pagerduty_ack)
   2. Scale payment-db read replicas (if act.aws_rds_scale enabled)

   What requires human action:
   1. Add the missing database index
   2. Review the migration that removed it"
```

This is the key difference between Joe and a simple runbook executor: Joe understands *why* something is broken and can reason about whether a proposed fix will actually help or make things worse.

### 6.4 Healing Safety Guarantees

Even with healing enabled, the hardcoded safety invariants remain:

1. **T3 notification contract is always enforced** — Joe announces before and after every mutation. For REPL users, this is a blocking 3-second window with Ctrl+C to cancel.

2. **Subcommand allowlists are compiled in** — Enabling `kubectl` in the policy doesn't unlock `kubectl delete`. Mutation subcommands require their own dedicated policy keys (e.g., `act.k8s_scale`).

3. **Self-protection invariants never change** — Joe cannot modify its own config, safety policy, or process, regardless of what healing actions are enabled.

4. **Audit log captures everything** — Every T2 and T3 action is logged with timestamp, tool, arguments, and result. This is the record of what Joe did and why.

5. **Per-action granularity** — A team can enable `k8s_scale` and `alertmanager_silence` while keeping `k8s_apply` and `k8s_delete` disabled. Each mutation is a separate trust decision.

6. **Environment-level operations are always blocked** — Operations targeting entire namespaces, clusters, or environments (e.g., `kubectl delete namespace`, `terraform destroy`) are rejected by deterministic pattern matching regardless of policy flags, unless the specific environment is explicitly allow-listed in `environment_operations.allow_environment_ops` (§3.6).

7. **Mutation circuit breaker prevents runaway sequences** — A rolling-window rate limiter on T3 actions trips after a configurable threshold (default: 5 mutations in 10 minutes), suspending all further mutations until a human explicitly resets it. This catches LLM reasoning loops that chain destructive actions faster than humans can review (§3.7).

8. **Joe never inherits user credentials** — Joe's infrastructure access uses its own service account with pre-scoped permissions, never the calling user's kubeconfig or cloud credentials. Even if a user has cluster-admin, Joe's operations are bounded by its service account's IAM/RBAC (§3.8).

### 6.5 Example: Enabling Healing in safety-policy.yaml

```yaml
# ~/.joe/safety-policy.yaml — a team that trusts Joe with scaling and alerting
version: 1

record:
  graph_mutations: true
  source_registration: true
  onboarding_facts: true
  autonomous_refresh: true

act:
  write_file:
    enabled: false

  run_command:
    enabled: true
    allowed_commands:
      - ls
      - cat
      - head
      - tail
      - grep
      - find
      - wc
      - kubectl              # Read-only subcommands enforced by compiled-in allowlist

  # Healing actions this team has opted into:
  k8s_scale:
    enabled: true            # Joe can scale deployments
  k8s_restart:
    enabled: true            # Joe can rollout restart
  alertmanager_silence:
    enabled: true            # Joe can silence alerts while fixing
  pagerduty_ack:
    enabled: true            # Joe can ack incidents

  # Healing actions this team has NOT opted into:
  k8s_apply:
    enabled: false           # No applying manifests
  k8s_cordon:
    enabled: false           # No node isolation
  pagerduty_resolve:
    enabled: false           # Human resolves incidents
  git_push:
    enabled: false           # No pushing to repos
  helm_upgrade:
    enabled: false           # No helm upgrades
```

---

## Part 7: Emergency Shutdown (Panic Mode)

Joe needs a kill switch. When something goes wrong—runaway automation, unexpected behavior, or human error—operators must be able to immediately halt all Joe operations.

### 7.1 Panic Triggers

Multiple ways to trigger emergency stop:

| Method | Command / Endpoint | Use Case |
|--------|-------------------|----------|
| REPL command | `/panic` | User in active session sees problem |
| CLI command | `joe panic` | Operator not in active session |
| API endpoint | `POST /api/v1/panic` | Automation, external monitoring |
| Signal | `SIGUSR1` to joecored | Unix-native, works when API unreachable |
| Dead man's switch | Heartbeat timeout | joecored loses contact with control plane |

### 7.2 Panic Sequence

When panic is triggered:

```
1. IMMEDIATE (< 100ms)
   ├── Stop accepting new requests (HTTP 503)
   ├── Set global panic flag (atomic bool)
   └── Log panic event with trigger source

2. IN-FLIGHT OPERATIONS (< 1s)
   ├── Cancel all running tool contexts
   ├── Abort LLM streaming responses
   └── Interrupt background refresh loop

3. ROLLBACK (best effort)
   ├── If mid-transaction, attempt rollback
   └── Record incomplete operations to audit log

4. NOTIFY (< 5s)
   ├── Log panic completion with state dump
   ├── Send alert via configured channels (Slack, PagerDuty)
   └── Write panic state to ~/.joe/panic.state

5. EXIT
   └── Process exits with code 2 (panic exit)
       Orchestrator (systemd, k8s) will restart in safe mode
```

### 7.3 Safe Mode

After a panic, joecored restarts in **safe mode**:

```yaml
# ~/.joe/panic.state (written on panic, read on startup)
triggered_at: "2025-02-21T14:32:01Z"
trigger_source: "api"  # repl | cli | api | signal | heartbeat
trigger_user: "jaime@company.com"
reason: "runaway scaling detected"
incomplete_operations:
  - tool: k8s_scale
    args: {deployment: payment-svc, replicas: 50}
    status: cancelled
```

**Safe mode restrictions:**
- T1 (Observe) operations only — all mutations disabled
- Background refresh paused
- Core Agent autonomous operations suspended
- Explicit unlock required to resume normal operation

### 7.4 Unlock Procedure

To exit safe mode:

```bash
# CLI unlock with reason (logged to audit)
joe unlock --reason "investigated panic, false alarm - operator error"

# Or via API
curl -X POST http://localhost:7777/api/v1/unlock \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"reason": "investigated panic, root cause identified and fixed"}'
```

Unlock requirements:
- Authenticated user with appropriate RBAC permissions
- Reason field mandatory (audit trail)
- Unlock event logged with user identity and reason
- Incomplete operations from panic shown to user before unlock

### 7.5 Implementation

**Panic package:** `internal/safety/panic.go`

```
internal/safety/
├── panic.go           # Panic trigger, state management
├── panic_state.go     # State file read/write
├── safemode.go        # Safe mode enforcement
└── unlock.go          # Unlock handler
```

**Key components:**

1. **Global panic flag** — Atomic bool checked by:
   - HTTP middleware (reject requests if panicked)
   - Tool executor (cancel if panicked)
   - Core Agent (stop refresh if panicked)

2. **Context cancellation** — All operations use contexts derived from a root panic context. Triggering panic cancels the root context, propagating to all in-flight operations.

3. **State persistence** — Panic state written to disk before exit, read on startup to detect if previous run panicked.

4. **Signal handler** — SIGUSR1 registered at startup, triggers panic sequence.

### 7.6 API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/v1/panic` | POST | Trigger emergency shutdown |
| `/api/v1/panic/status` | GET | Check if in safe mode |
| `/api/v1/unlock` | POST | Exit safe mode (requires reason) |

**Panic request:**
```json
POST /api/v1/panic
{
  "reason": "runaway scaling detected"  // optional but recommended
}
```

**Status response:**
```json
GET /api/v1/panic/status
{
  "safe_mode": true,
  "triggered_at": "2025-02-21T14:32:01Z",
  "trigger_source": "api",
  "trigger_user": "jaime@company.com",
  "reason": "runaway scaling detected",
  "incomplete_operations": [...]
}
```

### 7.7 REPL Integration

```
> /panic
⚠️  EMERGENCY SHUTDOWN

This will immediately:
- Cancel all in-flight operations
- Stop accepting new requests  
- Exit joecored (will restart in safe mode)

Type 'CONFIRM PANIC' to proceed, or press Enter to cancel: CONFIRM PANIC

🛑 Panic triggered. joecored shutting down...
   Cancelled 2 in-flight operations
   State saved to ~/.joe/panic.state
   
Reconnect after restart. Joe will be in safe mode (read-only).
Use 'joe unlock --reason "..."' to resume normal operation.
```

### 7.8 Safety Guarantees

1. **Panic always works** — No dependencies on LLM, external services, or database. Pure in-memory flag + signal handling.

2. **Panic is fast** — Target <1s from trigger to process exit.

3. **Panic is audited** — Trigger event, incomplete operations, and unlock all logged.

4. **Panic survives restart** — State persisted to disk, safe mode enforced on next startup.

5. **Panic requires explicit recovery** — No automatic unlock. Human must acknowledge and provide reason.

6. **Panic is idempotent** — Multiple panic triggers are safe (no-op if already panicking).

---

## Risk Matrix (updated post-Phase 5.5)

```
           Impact
           HIGH ┃ Cred Storage  │ Prompt Inj.   │ No TLS
                ┃ (Layer 2)     │ (Layer 3)     │ (Layer 1)
                ┃               │               │
           MED  ┃ No RBAC       │ read_file     │ No Rate Limit
                ┃ (Layer 4)     │ no sandbox    │ (Layer 1)
                ┃               │ (Layer 3)     │
           LOW  ┃ No Audit      │ Sec Headers   │
                ┃ (Layer 4)     │ (Layer 1)     │
                ┗━━━━━━━━━━━━━━━┿━━━━━━━━━━━━━━━┿━━━━━━━━━━━━━━━
                  LOW             MEDIUM          HIGH
                                Likelihood

Resolved in Phase 5.5:
  - No Auth (Layer 1) → Bearer token middleware
  - No Request Size Limits (Layer 1) → MaxBytesReader 1 MB
  - File Tools unsandboxed (Layer 3) → write_file allowed_directories + self-protection
  - run_command unrestricted (Layer 3) → subcommand allowlists + self-protection
```

---

## References

- OWASP Top 10: https://owasp.org/www-project-top-ten/
- Go secure coding: https://owasp.org/www-project-go-secure-coding-practices-guide/
- `docs/joe-architecture.md` — system architecture
- `docs/next-steps-plan.md` — implementation phases
