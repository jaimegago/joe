# Joe Security Architecture

## Overview

Joe implements defense-in-depth security with two independent layers:

1. **RBAC Layer**: Controls what humans can request (pre-LLM)
2. **Safety Layer**: Controls what LLMs can execute (post-LLM)

Both layers are required. RBAC prevents unauthorized requests from reaching the LLM. Safety controls prevent accidents even for authorized users.

## Security Layers

```
User Request
    │
    ▼
┌─────────────────────────────────────────┐
│ LAYER 1: RBAC (Human Authorization)    │
│ - Who are you?                          │
│ - What can you request?                 │
│ - Env/namespace/resource scope          │
└───────────┬─────────────────────────────┘
            │ DENY → "Access denied"
            │
            │ ALLOW
            ▼
┌─────────────────────────────────────────┐
│ LLM Reasoning                           │
│ - Analyzes request                      │
│ - Picks tools                           │
└───────────┬─────────────────────────────┘
            │
            ▼
┌─────────────────────────────────────────┐
│ LAYER 2: Safety (LLM Controls)         │
│ - Dry-run by default                    │
│ - Risk assessment                       │
│ - Approval workflows                    │
│ - Audit logging                         │
└───────────┬─────────────────────────────┘
            │
            ▼
       Tool Execution
```

## RBAC Layer (Pre-LLM)

**Purpose**: Control what users can ask Joe to do based on their identity and role.

**Examples**:
- Junior engineer can query dev/staging logs, but not prod
- Senior engineer can modify dev/staging, read prod
- SRE can do everything, but destructive prod ops require approval
- Security team can read secrets and audit logs, but not modify anything

**Components**:

### Authentication (Who are you?)

Supports multiple identity providers via adapters:
- **LDAP/Active Directory**: Corporate directory integration
- **Azure Entra ID**: Azure SSO
- **AWS IAM**: AWS identity federation
- **GCP Identity**: Google Cloud identity
- **OIDC/SAML**: Generic SSO providers
- **API Keys**: For automation/service accounts
- **mTLS**: Certificate-based auth
- **Local Dev**: No auth (development only)

**Token Flow**:
1. User authenticates via chosen provider
2. Joe receives identity token
3. Token cached in `~/.joe/token` (CLI) or browser session (Web UI)
4. Token validated on each request (cached for TTL)
5. Groups/roles extracted from identity

### Authorization (What can you do?)

**Permission Model**:

Each user/group gets permissions defining:
- **Environments**: `[dev, staging, prod, all]`
- **Namespaces**: `[payments, platform, monitoring, all]`
- **Resources**: `[pods, deployments, secrets, logs, all]`
- **Actions**: `[Read, Query, Mutate, Delete]`

**Action Types**:
- `Read`: View current state (pod status, deployment config)
- `Query`: Execute read-only queries (logs, metrics, graph traversal)
- `Mutate`: Modify configuration (scale deployment, update config)
- `Delete`: Remove resources (delete pod, remove PVC)

**Policy Examples**:

```yaml
# Junior Engineers
- environments: ["dev", "staging"]
  namespaces: ["*"]
  resources: ["*"]
  actions: [Read, Query]

# Senior Engineers  
- environments: ["dev", "staging"]
  namespaces: ["*"]
  resources: ["*"]
  actions: [Read, Query, Mutate]
- environments: ["prod"]
  namespaces: ["*"]
  resources: ["pods", "deployments"]
  actions: [Read, Query]

# SRE Team
- environments: ["*"]
  namespaces: ["*"]
  resources: ["*"]
  actions: [Read, Query, Mutate, Delete]
  constraints:
    - require-approval: [Delete]
      for-environments: [prod]

# Security Team
- environments: ["*"]
  namespaces: ["*"]
  resources: ["secrets", "audit-logs"]
  actions: [Read]
```

### Enforcement Point

**Pre-LLM Check**:
```
User: "show prod payment service logs"
         │
         ▼
    Policy Check:
    - User: jaime@company.com
    - Groups: [sre, platform]
    - Request: {env: prod, namespace: payments, 
                resource: logs, action: Query}
    - Policies: Load for [sre, platform] groups
    - Evaluation: Match found → ALLOW
         │
         ▼
    Request forwarded to LLM
```

**Denial Example**:
```
User: "delete prod payment database"
         │
         ▼
    Policy Check:
    - User: junior@company.com
    - Groups: [engineering-junior]
    - Request: {env: prod, resource: database, action: Delete}
    - Policies: No match for prod access
    - Evaluation: DENY
         │
         ▼
    "Access denied: You don't have permissions to 
     perform Delete actions in prod environment"
```

### Performance

**Latency**: <1ms per request (in-memory evaluation)

**Caching Strategy**:
- Identity tokens cached for token TTL (~1 hour)
- First request: 50-100ms (validate with IdP)
- Subsequent requests: <1ms (cache hit)
- Policies loaded at startup, refreshed every 60s
- No network calls on hot path

**Overhead**: ~0.05-0.2% of total request time
- Policy check: <1ms
- LLM inference: 500-2000ms
- Tool execution: 10-500ms

### Audit Trail

Every request logged:
- **Timestamp**: When request occurred
- **User**: Identity (email, user ID)
- **Groups**: User's groups at time of request
- **Request**: What was requested (full query)
- **Decision**: Allow/Deny
- **Policy**: Which policy matched (or why denied)
- **Result**: What happened (if allowed)

## Safety Layer (Post-LLM)

**Purpose**: Prevent accidental or dangerous operations, even for authorized users.

**Risk Levels**:
- **Read-only**: No approval needed (get pod status)
- **Config-change**: Dry-run shown, approval required (scale deployment)
- **Destructive**: Explicit confirmation required (delete PVC)

**Approval Workflow**:

```
LLM selects: kubectl delete pvc data-postgres-0
         │
         ▼
    Risk Assessment: DESTRUCTIVE
         │
         ▼
    Dry-run execution (show what would happen)
         │
         ▼
    User prompt: 
    "This will DELETE persistent volume claim data-postgres-0
     containing 50GB of data in prod.
     
     Type 'confirm' to proceed or 'cancel' to abort:"
         │
         ├─ cancel → Operation aborted
         │
         └─ confirm → Execute + Audit log
```

**Rollback Capabilities**:
- Configuration changes stored before mutation
- Automatic rollback on execution failure
- Manual rollback command available

**Sandboxing**:
- All tool executions run in isolated contexts
- Resource limits applied (CPU, memory, execution time)
- Network policies enforced

**Emergency Shutdown (Panic Mode)**:
- Kill switch for immediate halt: `/panic` REPL, `joe panic` CLI, `POST /api/v1/panic`, `SIGUSR1`
- Cancels all in-flight operations, stops accepting requests
- Restarts in safe mode (T1 read-only until explicit unlock)
- Requires `joe unlock --reason "..."` to resume normal operation
- Full details in `docs/security-in-layers.md` Part 7

## Security Best Practices

### For Operators

1. **Principle of Least Privilege**: Grant minimum required permissions
2. **Regular Policy Audits**: Review who has access to what
3. **Time-limited Elevated Access**: Use approval constraints for sensitive ops
4. **Monitor Audit Logs**: Watch for anomalous access patterns
5. **Rotate API Keys**: Regular rotation for automation accounts

### For Developers

1. **Identity Provider Integration**: Use existing corporate IdP
2. **Policy as Code**: Store policies in git with review process
3. **Separate Dev/Prod Policies**: Different rules for different environments
4. **Secret Management**: Never log or cache secrets
5. **Token Security**: Short TTLs, secure storage, rotation

### For Users

1. **Token Protection**: Treat Joe tokens like passwords
2. **Verify Scope**: Check what environment you're working in
3. **Review Dry-runs**: Read what will happen before confirming
4. **Report Issues**: Notify security team of suspicious activity
5. **Use MFA**: Enable multi-factor auth where supported

## Threat Model

### Threats Mitigated

1. **Unauthorized Access**: RBAC prevents users without permissions
2. **Privilege Escalation**: Scoped permissions per environment/namespace
3. **Accidental Deletion**: Approval workflows for destructive ops
4. **LLM Hallucination**: Dry-run + human approval catches mistakes
5. **Audit Trail Gaps**: Comprehensive logging of all operations
6. **Token Theft**: Short TTLs, revocation capability

### Threats NOT Mitigated

1. **Compromised Admin Account**: User with full permissions can do anything
   - Mitigation: MFA, approval constraints, audit monitoring
2. **Social Engineering**: User tricks authorized person into action
   - Mitigation: Approval workflows, audit trail
3. **LLM Prompt Injection**: Crafted inputs might confuse LLM
   - Mitigation: Input sanitization, RBAC still enforces limits
4. **Insider Threat**: Authorized user acts maliciously
   - Mitigation: Audit logs, approval workflows, separation of duties

## Compliance Considerations

**SOC 2**:
- Audit logging: All access recorded
- Access controls: RBAC with least privilege
- Change management: Approval workflows

**GDPR**:
- Right to deletion: Audit log retention policies
- Access transparency: Users can see their permissions
- Data minimization: Only necessary identity info cached

**HIPAA** (if handling healthcare data):
- Access controls: RBAC with environment/namespace isolation
- Audit trails: Comprehensive logging
- Encryption: Tokens encrypted at rest and in transit

## Configuration

### Policy Storage

Policies can be stored in:
1. **Local config**: `~/.joe/policies.yaml` (development)
2. **Git repository**: Versioned, reviewed (recommended)
3. **External policy engine**: OPA, Cedar (enterprise)

### Identity Provider Setup

Example Entra ID configuration:
```yaml
identity:
  provider: entra
  config:
    tenant_id: "your-tenant-id"
    client_id: "joe-app-id"
    client_secret_env: "ENTRA_CLIENT_SECRET"
```

Example LDAP configuration:
```yaml
identity:
  provider: ldap
  config:
    url: "ldaps://ldap.company.com:636"
    bind_dn: "cn=joe-service,ou=services,dc=company,dc=com"
    bind_password_env: "LDAP_BIND_PASSWORD"
    user_base_dn: "ou=users,dc=company,dc=com"
    group_base_dn: "ou=groups,dc=company,dc=com"
```

## FAQ

**Q: Can I bypass RBAC for emergencies?**  
A: No. Use a break-glass account with full permissions. All access is audited.

**Q: What if my token expires mid-operation?**  
A: Joe will prompt for re-authentication. Long-running ops use token refresh.

**Q: Can I have different permissions in different clusters?**  
A: Yes. Policies can include cluster selectors.

**Q: How are groups synced from IdP?**  
A: Automatically during token validation. Changes take effect on next request.

**Q: What's the performance impact?**  
A: <1ms per request. Negligible compared to LLM and tool execution times.

**Q: Can service accounts use Joe?**  
A: Yes. Use API key authentication with scoped permissions.

**Q: How do I debug policy denials?**  
A: Audit logs show which policy was evaluated and why it denied access.

**Q: Is there a UI for policy management?**  
A: Roadmap feature. Currently policies managed via YAML in git.
