# Joe Security Architecture

## Overview

Joe implements defense-in-depth security with three independent layers:

1. **RBAC Layer**: Controls what humans can request (pre-LLM)
2. **Safety Layer**: Controls what LLMs can execute (post-LLM)
3. **Security Service**: Manages security configuration outside Joe's reach

All layers are required. RBAC prevents unauthorized requests from reaching the LLM. Safety controls prevent accidents even for authorized users. The Security Service ensures Joe cannot modify its own security rules.

## Security Layers

```
User Request
    │
    ▼
┌─────────────────────────────────────────┐
│ LAYER 1: RBAC (Human Authorization)    │
│ - Who are you?                          │
│ - What zones can you access?            │
│ - What actions in each zone?            │
└───────────┬─────────────────────────────┘
            │ DENY → "Access denied"
            │
            │ ALLOW
            ▼
┌─────────────────────────────────────────┐
│ LLM Reasoning                           │
│ - Analyzes request                      │
│ - Picks tools + target sources          │
└───────────┬─────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────┐
│ LAYER 2: Safety (LLM Controls)         │
│ - T1/T2/T3 classification               │
│ - Dry-run + human approval              │
│ - Circuit breaker                       │
│ - Audit logging                         │
└───────────┬─────────────────────────────┘
            │
            ▼
       Tool Execution
```

---

## Security Zones

Security zones solve the problem of managing permissions across dynamic infrastructure sources. Instead of writing policies against specific source IDs (which are created dynamically), policies reference **zones** — pre-defined security boundaries.

### Why Zones?

```
┌──────────────────┐      ┌──────────────────┐      ┌──────────────────┐
│ Policies         │      │ Security Zones   │      │ Sources          │
│ (static, git)    │─────►│ (admin-managed)  │◄─────│ (dynamic, DB)    │
│                  │      │                  │      │                  │
│ "sre-team can    │      │ "prod-readonly"  │      │ grafana/xyz-prod │
│  write to        │      │ "prod-write"     │      │   zone: prod-ro  │
│  dev-full zone"  │      │ "dev-full"       │      │                  │
│                  │      │                  │      │ k8s/bar-test     │
│                  │      │                  │      │   zone: dev-full │
└──────────────────┘      └──────────────────┘      └──────────────────┘
```

### Zone Definitions

```yaml
# Managed by joe-security (NOT accessible to LLM)
security_zones:
  prod-readonly:
    description: "Production systems - read only"
    actions: [Read, Query]
    
  prod-write:
    description: "Production systems - with write access"
    actions: [Read, Query, Mutate]
    constraints:
      require_approval: [Mutate]
      
  prod-critical:
    description: "Critical production - restricted"
    actions: [Read, Query]
    constraints:
      require_approval: [Query]
      
  dev-full:
    description: "Development - full access"
    actions: [Read, Query, Mutate, Delete]
    
  unassigned:
    description: "Default for new sources"
    actions: [Read]  # Most restrictive
```

### Source → Zone Assignment Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│  1. Joe registers new source (via LLM tool)                         │
│                                                                      │
│  INSERT INTO sources (id, type, config) VALUES (...)                │
│  → Allowed ✅ (sources table is NOT protected)                      │
│                                                                      │
│  Zone lookup → No row found → defaults to 'unassigned' (Read only) │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  2. Joe notifies admin                                              │
│                                                                      │
│  "New source registered: grafana/xyz-experiment                     │
│   Status: unassigned (read-only by default)                         │
│   Assign a security zone via joe-security admin API."               │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  3. Admin assigns zone (via joe-security, NOT via LLM)              │
│                                                                      │
│  joe-security assign-zone grafana/xyz-experiment dev-full           │
│    --reason "approved for dev experimentation"                      │
│                                                                      │
│  → Writes to protected table (joe-security only)                   │
│  → Logged to audit_log                                              │
└─────────────────────────────────────────────────────────────────────┘
         │
         ▼
┌─────────────────────────────────────────────────────────────────────┐
│  4. Joe now respects new zone                                       │
│                                                                      │
│  Zone lookup → Returns 'dev-full'                                   │
│  Actions allowed: [Read, Query, Mutate, Delete]                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Policy → Zone → Principal

```yaml
policies:
  - principal: "junior-engineers"
    zones: [prod-readonly, dev-full]
    
  - principal: "senior-engineers"
    zones: [prod-readonly, prod-write, dev-full]
    
  - principal: "sre-team"
    zones: [prod-readonly, prod-write, prod-critical, dev-full]
```

### Permission Evaluation Example

```
User: "create a dashboard in grafana for payment-svc"
         │
         ▼
    Tool: grafana_create_dashboard
    Source: grafana/xyz-prod
         │
         ▼
    Lookup: grafana/xyz-prod → zone: prod-readonly
         │
         ▼
    Policy Check:
    - User: jaime@company.com
    - Groups: [sre-team]
    - Zone: prod-readonly allows: [Read, Query]
    - Action requested: Mutate
    - Mutate not in [Read, Query] → DENY
         │
         ▼
    "Access denied: grafana/xyz-prod is in zone 'prod-readonly'.
     To create dashboards, use a source in 'dev-full' or 'prod-write'."
```

---

## Pluggable Security Architecture

| Mode | joecored talks to | Security data | Use case |
|------|-------------------|---------------|----------|
| `embedded` | Local protected tables | Same DB, hardcoded protection | Development, small teams |
| `remote` | joe-security service | Separate process + DB | Production, high security |

### Mode 1: Embedded (Single Process)

```
┌─────────────────────────────────────────────────────────────────────┐
│  joecored (single binary, single DB)                                │
│                                                                      │
│  LLM Tools can write to:                                           │
│    ✅ sources, sessions, graph, knowledge                          │
│                                                                      │
│  LLM Tools CANNOT write to (hardcoded):                            │
│    ❌ security_zones                                                │
│    ❌ source_zone_assignments                                       │
│    ❌ rbac_policies                                                 │
│    ❌ audit_log (append-only)                                       │
│                                                                      │
│  Admin API (separate auth, not LLM-accessible):                    │
│    POST /api/v1/admin/zones                                        │
│    POST /api/v1/admin/source-zones                                 │
│    POST /api/v1/admin/policies                                     │
└─────────────────────────────────────────────────────────────────────┘
```

**Protection**: Hardcoded table checks in tool executor:

```go
// internal/safety/invariants.go (compiled)
var writeProtectedTables = map[string]bool{
    "security_zones":          true,
    "source_zone_assignments": true,
    "rbac_policies":           true,
}
var appendOnlyTables = map[string]bool{
    "audit_log": true,
}
```

### Mode 2: Remote (Separate Process)

```
┌───────────────────────────┐      ┌───────────────────────────┐
│  joecored                 │      │  joe-security             │
│                           │      │                           │
│  main.db (read-write)     │      │  security.db (read-write) │
│  - sources                │      │  - zones                  │
│  - sessions               │ gRPC │  - assignments            │
│  - graph        ─────────────────│  - policies               │
│  - knowledge              │      │  - audit_log              │
│                           │      │                           │
│  No security tables       │      │  Admin API (no LLM code)  │
└───────────────────────────┘      └───────────────────────────┘
```

**Why more secure:**
- joecored has NO write access to security data
- Even RCE in joecored cannot modify zone assignments
- joe-security has no LLM code — smaller attack surface

### Configuration

```yaml
# ~/.joe/config.yaml

# Embedded mode
security:
  mode: embedded
  admin_token_env: "JOE_ADMIN_TOKEN"

# Remote mode
security:
  mode: remote
  endpoint: "localhost:7778"         # Sidecar
  # Or: "joe-security.svc:7778"      # K8s service
  tls:
    enabled: true
    ca_cert: "/etc/joe/ca.crt"
```

### The Interface

```go
// internal/security/interface.go

type SecurityPolicy interface {
    GetSourceZone(ctx context.Context, sourceID string) (Zone, error)
    ListZones(ctx context.Context) ([]Zone, error)
    IsAllowed(ctx context.Context, principal, sourceID string, action Action) (bool, error)
    ListUnassignedSources(ctx context.Context) ([]string, error)
}

type SecurityAdmin interface {
    CreateZone(ctx context.Context, zone Zone) error
    AssignSourceZone(ctx context.Context, sourceID, zoneID, assignedBy, reason string) error
    SetPolicy(ctx context.Context, principal string, zones []string) error
}
```

---

## Deployment Options

### Development: Embedded

```bash
joecored --config config.yaml
# security.mode: embedded
```

### Production: Sidecar

```yaml
spec:
  containers:
    - name: joecored
      env:
        - name: JOE_SECURITY_MODE
          value: "remote"
        - name: JOE_SECURITY_ENDPOINT
          value: "localhost:7778"
          
    - name: joe-security
      command: ["joe-security"]
```

### High Security: Separate Service

```yaml
# joe-security in separate namespace
apiVersion: apps/v1
kind: Deployment
metadata:
  name: joe-security
  namespace: security-system
```

---

## Safety Layer (Post-LLM)

Independent of RBAC. Even if RBAC allows an action, Safety controls still apply.

### Risk Tiers

| Tier | Name | Examples | Behavior |
|------|------|----------|----------|
| T1 | Observe | Get pod status, query metrics | Execute immediately |
| T2 | Record | Add note, create dashboard | Execute with logging |
| T3 | Act | Delete pod, scale deployment | Dry-run + 3s countdown |

### Emergency Shutdown

- Triggers: `/panic`, `joe panic`, `POST /api/v1/panic`, `SIGUSR1`
- Cancels in-flight operations
- Restarts in safe mode (T1 only)
- Requires `joe unlock --reason "..."` to resume

---

## Protected Data Model

```sql
-- PROTECTED TABLES (LLM cannot write)

CREATE TABLE security_zones (
    id          TEXT PRIMARY KEY,
    description TEXT,
    actions     TEXT NOT NULL,
    constraints TEXT
);

CREATE TABLE source_zone_assignments (
    source_id   TEXT PRIMARY KEY,
    zone_id     TEXT NOT NULL REFERENCES security_zones(id),
    assigned_by TEXT NOT NULL,
    assigned_at TIMESTAMP NOT NULL,
    reason      TEXT
);

CREATE TABLE rbac_policies (
    id          TEXT PRIMARY KEY,
    principal   TEXT NOT NULL,
    zones       TEXT NOT NULL,
    created_by  TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL
);

-- APPEND-ONLY (LLM can INSERT only)
CREATE TABLE audit_log (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    actor       TEXT NOT NULL,
    action      TEXT NOT NULL,
    target      TEXT,
    details     TEXT
);
```

---

## Threat Model

### Threats Mitigated

| Threat | Mitigation |
|--------|------------|
| Prompt injection modifies zones | Protected tables / separate service |
| LLM escalates permissions | Zone assignments require admin |
| Unauthorized source access | Sources default to unassigned (read-only) |
| Accidental destructive action | T3: dry-run + countdown |
| Runaway mutations | Circuit breaker |
| Audit log tampering | Append-only |

### Embedded vs Remote Mode

| Threat | Embedded | Remote |
|--------|----------|--------|
| Bug bypasses table check | ❌ Vulnerable | ✅ Protected |
| RCE in joecored | ❌ Full DB access | ✅ Can't write security.db |
| Supply chain attack | ❌ Vulnerable | ✅ joe-security has no LLM deps |

---

## CLI Reference

### joe-security CLI

```bash
# List zones
joe-security zones list

# Create zone
joe-security zones create staging-write \
  --actions Read,Query,Mutate

# Assign source to zone
joe-security assign-zone grafana/staging staging-write \
  --reason "approved"

# List unassigned sources
joe-security sources unassigned

# View audit log
joe-security audit --since 24h
```

---

## FAQ

**Q: Can Joe modify its own security zones?**
A: No. Protected by hardcoded checks (embedded) or separate process (remote).

**Q: What happens when Joe registers a new source?**
A: Defaults to `unassigned` zone (read-only). Admin must assign zone.

**Q: What if joe-security is down?**
A: joecored fails closed — all requests denied. Intentional for security.
