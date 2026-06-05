# Operations

Operator-facing controls: action safety tiers, emergency shutdown, and RBAC
administration. For the security architecture behind these, see
[security-in-layers.md](security-in-layers.md) and [JOE_SECURITY.md](JOE_SECURITY.md).

All HTTP examples below assume a service-account bearer token in `$JOE_API_KEY`
(see [configuration.md](configuration.md#authentication)).

## Action safety tiers

Every tool Joe can execute is classified into one of three tiers. The tier
determines confirmation behavior and safe-mode restrictions.

| Tier | Name    | Examples                                        | Safe Mode  |
| ---- | ------- | ----------------------------------------------- | ---------- |
| T1   | Observe | `read_file`, `k8s_get`, `graph_query`           | ✅ Allowed |
| T2   | Record  | `write_file`, `graph_add_node`                  | ❌ Blocked |
| T3   | Act     | `run_command` (mutations), `kubectl apply`      | ❌ Blocked |

Tier behavior is configured in `~/.joe/safety-policy.yaml`. Joe's own tools
cannot read or modify this file. When the file is absent, Joe uses the most
restrictive default: all T2 enabled (internal state is safe), all T3 disabled.

```yaml
# ~/.joe/safety-policy.yaml
version: 1

# T2 — internal state mutations
record:
  graph_mutations: true
  source_registration: true
  onboarding_facts: true
  autonomous_refresh: true

# T3 — external system mutations (disabled by default; explicit opt-in)
act:
  write_file:
    enabled: false
    allowed_directories:        # empty = unrestricted (when enabled)
      - /tmp/joe-workspace
      - /home/me/projects
  run_command:
    enabled: false
    allowed_commands:           # allowlist by command
      - kubectl get
      - kubectl logs
      - kubectl describe
      - helm list
  k8s_write:
    enabled: false
  pagerduty_ack:
    enabled: false
  alertmanager_silence:
    enabled: false
  git_push:
    enabled: false
```

## Emergency shutdown (panic mode)

Joe has a kill switch for runaway operations. Four ways to trigger it:

**From the CLI:**

```bash
joe panic --reason "runaway mutation detected"
```

**Via HTTP API:**

```bash
curl -X POST http://localhost:7777/api/v1/panic \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"reason": "runaway mutation detected"}'
```

**Via Unix signal** (to the running `joe` process):

```bash
kill -USR1 <joe-pid>
```

**From the Web UI** chat or controls.

### What happens

1. `joe` writes `~/.joe/panic.state` and exits with code 2
2. On restart, `joe` reads `panic.state` and boots in **safe mode**
3. In safe mode, only T1 (read-only) tools are allowed — no writes or mutations
4. Joe logs a warning on every startup until safe mode is cleared

### Check status

```bash
curl http://localhost:7777/api/v1/panic/status \
  -H "Authorization: Bearer $JOE_API_KEY"
# {"safe_mode":true,"triggered_at":"...","trigger_source":"signal","trigger_reason":"..."}
```

### Resume normal operation

A reason is required for the audit log:

```bash
# CLI
joe unlock --reason "false alarm — incident resolved"

# HTTP
curl -X POST http://localhost:7777/api/v1/unlock \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"reason": "false alarm — incident resolved"}'
```

## Incident regime

`joe incident` declares and resolves an incident regime, which changes how Joe
gates actions during an active incident.

```bash
joe incident status                         # is a regime active, and who declared it
joe incident declare --reason "payment outage"
joe incident resolve --reason "mitigated"
```

Declare/resolve authorize server-side against the `regime-control` zone. Incident
history lives in the append-only audit log (there is no `list` history endpoint).

## RBAC administration

Sources are assigned to zones; principals are granted access to zones. Manage all
of this from the Web UI admin panel or the admin API below — the single audited
writer to RBAC state. All admin endpoints require Bearer auth.

### Default zones

| Zone            | Allowed Actions                      |
| --------------- | ------------------------------------ |
| `prod-readonly` | read, query                          |
| `prod-write`    | read, query, mutate                  |
| `dev-full`      | read, query, mutate, delete          |
| `unassigned`    | read (default for new sources)       |

### Admin API

**List zones:**

```bash
curl http://localhost:7777/api/v1/admin/zones \
  -H "Authorization: Bearer $JOE_API_KEY"
```

**Create a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/zones \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"id":"staging","name":"Staging","allowed_actions":["read","query","mutate"]}'
```

**Assign a source to a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/source-zones \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"source_id":"k8s-prod","zone_id":"prod-readonly","assigned_by":"alice","reason":"initial setup"}'
```

**Grant a principal access to a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/policies \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"principal":"svc:ci","zone_id":"prod-readonly"}'
```

**List unassigned sources:**

```bash
curl http://localhost:7777/api/v1/admin/unassigned \
  -H "Authorization: Bearer $JOE_API_KEY"
```

The `principal` is a service-account principal (`svc:<name>`) or an OIDC user
identity. See [JOE_RBAC_IMPLEMENTATION.md](JOE_RBAC_IMPLEMENTATION.md) for the
full RBAC spec.
