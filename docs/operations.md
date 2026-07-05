# Operations

Operator-facing controls: action safety tiers, emergency shutdown, and RBAC
administration. For the security architecture behind these, see
[security-in-layers.md](reference/security-in-layers.md).

All HTTP examples below assume a service-account bearer token in `$JOE_API_KEY`
(see [configuration.md](configuration.md#authentication)).

## Action safety

Every tool Joe can execute is classified on a binary **Read/Mutate** axis at
registration. Reads (queries, Joe's own graph/model maintenance, notifications
to humans) always run. Mutates (writes to files, infrastructure, deployments,
external PR/MR threads) are **denied by default**. An unknown tool is treated as
a mutation and denied.

The mutation gate is **compiled in — there is no operator-editable safety-policy
file** in this build. Gating comes from three compiled-in layers: the binary
Read/Mutate classification, the default policy (`internal/safety/policy.go`,
`DefaultPolicy` — all managed-system mutations off), and the boot-resolved write
floor, which denies the entire Mutate class in observation mode or sticky safe
mode regardless of anything else. The only per-request control is the task
`safety_tier` (`observe` / `record` / `act`), which can further *restrict* a
given task but never grants more than the compiled-in default.

For the full model — classification rules, the write floor, and the executor
gate order (floor → scope → policy → notify) —
[security-in-layers.md](reference/security-in-layers.md) is the authority.

> Joe still protects `~/.joe/` structurally: the server ships no filesystem-write
> tool, and the compile-time self-protection path exclusion
> (`internal/safety/invariants.go`) excludes the whole `~/.joe/` directory from
> any file tool that might ever be reintroduced.

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
identity. See [security-in-layers.md](reference/security-in-layers.md) for the full RBAC
model.
