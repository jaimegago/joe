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

## Part 1: Current State (as of Phase 5)

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

---

## Part 2: Full Mutation Audit

Every component that can change state, classified by blast radius.

### Local Tools (joe binary — User Agent side)

| Tool | Can Mutate? | What It Changes | Authorization | Notification | Blast Radius |
|------|-------------|-----------------|---------------|--------------|--------------|
| `write_file` | **YES** | Any file the process user can write | None | None | **Local filesystem** |
| `run_command` | **YES** | Depends on allowlist | Allowlist only | None | **Local + remote infra** |
| `read_file` | No | — | — | — | Read-only |
| `local_git_status` | No | — | — | — | Read-only |
| `local_git_diff` | No | — | — | — | Read-only |
| `echo` | No | — | — | — | Harmless |
| `ask_user` | No | — | — | — | Harmless |

**`write_file` detail** (`internal/tools/local/writefile/writefile.go:44-81`):
- Creates files anywhere, creates parent dirs with `os.MkdirAll`, overwrites silently.
- No path validation, no sandbox, no confirmation.

**`run_command` detail** (`internal/tools/local/runcmd/runcmd.go`):
- Allowlist defined in `internal/tools/default.go:33-36`:
  ```
  ls, cat, head, tail, grep, find, wc, kubectl, helm, argocd
  ```
- **Problem:** `kubectl`, `helm`, `argocd` are not read-only. They can:
  - `kubectl delete pod`, `kubectl scale`, `kubectl apply`
  - `helm install`, `helm upgrade`, `helm delete`
  - `argocd app sync`, `argocd app rollback`
- Arguments are not validated — only the base command is allowlisted.

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
| `/api/v1/sources` | POST | Creates source + registers adapter | None | None |
| `/api/v1/sources/{id}` | DELETE | Removes source + disconnects adapter | None | None |
| `/api/v1/clarifications/{id}/answer` | POST | Marks answered + applies graph ops | None | None |
| `/api/v1/clarifications/{id}/dismiss` | POST | Marks dismissed | None | None |
| `/api/v1/onboarding` | POST | Triggers LLM → graph mutations | None | None |
| `/api/v1/refresh` | POST | Triggers full graph reconciliation | None | None |

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

---

## Part 4: Gaps by Layer (non-mutation security)

### Layer 1: Network Boundary (joecored HTTP API)

| Gap | Severity | Detail |
|-----|----------|--------|
| No authentication | CRITICAL | API at `:7777` has zero auth — any network client has full control |
| No rate limiting | HIGH | No middleware to prevent DoS or brute-force |
| No request size limits | HIGH | Unbounded request bodies → memory exhaustion |
| Plaintext HTTP | HIGH | joe↔joecored traffic unencrypted; credentials in transit exposed |
| No CORS / security headers | MEDIUM | Missing `X-Content-Type-Options`, `X-Frame-Options`, etc. |

**Key files:** `cmd/joecored/main.go`, `internal/api/server.go`

### Layer 2: Credential Storage

| Gap | Severity | Detail |
|-----|----------|--------|
| Plaintext credentials in DB | CRITICAL | `sources.config` JSON column stores AWS keys, Git tokens unencrypted |
| SSH keys with empty passphrase | HIGH | `git.go:158` loads SSH keys with `""` passphrase |
| No credential rotation | MEDIUM | Static credentials never refreshed or expired |
| World-readable config | MEDIUM | `~/.joe/config.yaml` uses default umask (typically 0644) |

**Key files:** `internal/adapters/git/git.go:169`, `internal/adapters/aws/aws.go:256-257`

### Layer 3: LLM Tool Sandbox

| Gap | Severity | Detail |
|-----|----------|--------|
| No path sandboxing on `read_file` | CRITICAL | LLM can read any file the process user can access |
| No path sandboxing on `write_file` | CRITICAL | LLM can write to any writable location |
| Prompt injection | CRITICAL | User input flows unsanitized into LLM context, which then calls tools |

**Key files:** `internal/tools/local/readfile/readfile.go`, `internal/tools/local/writefile/writefile.go`, `internal/tools/local/pathutil.go`

### Layer 4: Authorization

| Gap | Severity | Detail |
|-----|----------|--------|
| No per-source access control | HIGH | Any client can query any infrastructure source |
| No user identity model | HIGH | No concept of who is making a request |
| No audit logging | MEDIUM | No record of who did what, when |

---

## Part 5: Implementation Roadmap

### Do Now (before Phase 6)

**1. Action Safety Framework — core enforcement**
- Implement action tier classification (T1/T2/T3)
- Load `safety-policy.yaml` at startup
- Add gate check in tool executor before every `Execute()` call
- Hardcode self-protection invariants (Joe can't touch `~/.joe/`)
- Files: new `internal/safety/`, changes to `internal/tools/`, `internal/useragent/`, `internal/coreagent/`

**2. Path sandboxing for file tools**
- Add `allowed_directories` from safety policy
- Reject paths containing `..` after normalization
- Reject symlinks that resolve outside allowed directories
- Hardcode exclusion of `~/.joe/` directory
- Files: `internal/tools/local/pathutil.go`, `readfile/readfile.go`, `writefile/writefile.go`

**3. run_command subcommand validation**
- Split allowlist into read-only commands and mutation-capable commands
- Add subcommand allowlist for `kubectl`, `helm`, `argocd`
- Default: only read-only subcommands permitted
- Files: `internal/tools/local/runcmd/runcmd.go`, `internal/tools/default.go`

**4. T3 notification contract**
- Implement pre-execution notification in REPL (blocking, cancellable)
- Implement post-execution summary
- Files: `internal/repl/`, `internal/useragent/`

**5. API key authentication**
- `Authorization: Bearer <token>` middleware on all `/api/v1/` routes
- Token from `config.yaml` under `server.api_key`
- Files: new `internal/api/middleware.go`, `cmd/joecored/main.go`

**6. Request size limits**
- `http.MaxBytesReader` wrapper (default 1 MB)
- Files: `internal/api/middleware.go`

### Do in Phase 6

**7. Credential encryption at rest**
**8. TLS support for joe↔joecored**
**9. Rate limiting middleware**
**10. Security headers**
**11. Mutation audit log** — structured log of all T2/T3 actions with timestamp, tool, args, result

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

## Risk Matrix

```
           Impact
           HIGH ┃ Cred Storage  │ File Tools    │ No Auth
                ┃ (Layer 2)     │ (Layer 3)     │ (Layer 1)
                ┃               │               │
           MED  ┃ No RBAC       │ Prompt Inj.   │ No Rate Limit
                ┃ (Layer 4)     │ (Layer 3)     │ (Layer 1)
                ┃               │               │
           LOW  ┃ No Audit      │ Sec Headers   │ No TLS (local)
                ┃ (Layer 4)     │ (Layer 1)     │ (Layer 1)
                ┗━━━━━━━━━━━━━━━┿━━━━━━━━━━━━━━━┿━━━━━━━━━━━━━━━
                  LOW             MEDIUM          HIGH
                                Likelihood
```

---

## References

- OWASP Top 10: https://owasp.org/www-project-top-ten/
- Go secure coding: https://owasp.org/www-project-go-secure-coding-practices-guide/
- `docs/joe-architecture.md` — system architecture
- `docs/next-steps-plan.md` — implementation phases
