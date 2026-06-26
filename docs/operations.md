# Operations

Operator-facing controls: action safety tiers, emergency shutdown, and RBAC
administration. For the security architecture behind these, see
[security-in-layers.md](security-in-layers.md).

All HTTP examples below assume a service-account bearer token in `$JOE_API_KEY`
(see [configuration.md](configuration.md#authentication)).

## Action safety

Every tool Joe can execute is classified on a binary **Read/Mutate** axis at
registration. Reads (queries, Joe's own graph/model maintenance, notifications
to humans) always run. Mutates (writes to files, infrastructure, deployments,
external PR/MR threads) are **denied by default** and require a per-action
opt-in in the safety policy's `act` section. An unknown tool is treated as a
mutation and denied.

This section covers only what an operator edits. For the full action-safety
model — the classification rules, the write floor, and how the executor gates
every call — [security-in-layers.md](security-in-layers.md) is the authority.

Policy lives in `~/.joe/safety-policy.yaml`. Joe's own tools cannot read or
modify this file (the entire `~/.joe/` directory is excluded from every file
tool at compile time). When the file is absent, Joe uses the most restrictive
default: every mutating action disabled.

```yaml
# ~/.joe/safety-policy.yaml
version: 1

# Mutating actions are denied by default; enable per action (explicit opt-in).
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

1. `joe` records the panic to the single `cluster_panic_state` DB row (id=1) and exits with code 2
2. On restart, `joe` reads that row and boots in **safe mode**, raising the write floor
3. In safe mode, only reads are allowed — every mutating action is blocked
4. Joe logs a warning on every startup until the panic row is cleared

### Check status

```bash
curl http://localhost:7777/api/v1/panic/status \
  -H "Authorization: Bearer $JOE_API_KEY"
# {"safe_mode":true,"triggered_at":"...","trigger_source":"signal","trigger_reason":"..."}
```

### Resume normal operation

Recovery is a host operation, not an HTTP call. The write floor is sealed at
boot and nothing in the running process can lower it, so there is no unlock
endpoint. On the host where Joe runs, clear the panic row, then restart:

```bash
joe unlock --reason "false alarm — incident resolved"
```

`joe unlock` opens the database directly and clears the `cluster_panic_state`
row; it does not contact or signal a running daemon. The cleared state takes
effect on the next start, when the floor is re-resolved from the now-clear row.
A reason is optional and, when given, is recorded to the log.

## Incident regime

`joe incident` declares and resolves an incident regime, which changes how Joe
gates actions during an active incident.

```bash
joe incident status                         # is a regime active, and who declared it
joe incident declare --session <id> --reason "payment outage"
joe incident resolve --reason "mitigated"
```

Declare promotes an existing chat session in place to the incident master, so
`--session <id>` is required (`--kind` defaults to `human`). Declare/resolve
authorize server-side against the `regime-control` zone. Incident history lives
in the append-only audit log (there is no `list` history endpoint).

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

**Assign a component to a zone:**

```bash
curl -X POST http://localhost:7777/api/v1/admin/component-zones \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -d '{"component_id":"k8s-prod","zone_id":"prod-readonly","assigned_by":"alice","reason":"initial setup"}'
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
identity. See [security-in-layers.md](security-in-layers.md) for the full RBAC
model.
