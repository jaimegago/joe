# Break-Glass Access — Joe

How an authorized operator reaches Joe when the normal OIDC human-login path is unavailable. This is an operator how-to, not a design spec: it tells you how to configure a break-glass credential, grant it admin, use it, confirm it is actually enforced, and review its use in the audit log. For the identity and authentication design behind it, see [joe-identity-design.md](reference/joe-identity-design.md); for the security overview, see [security-in-layers.md](reference/security-in-layers.md).

---

## What break-glass is

A **break-glass credential** is a service-account bearer key that resolves to a `svc:<name>` principal and is granted admin. "Break-glass" is an operational role, not a separate subsystem: the credential is an ordinary service-account key, and it becomes break-glass only because policy grants it admin and reserves it for emergencies.

It is the **same mechanism as the dev-token path** — the key a `joe` subcommand (e.g. `joe mcp`) uses to talk to a co-located `joe` server is the same kind of service-account bearer key. Break-glass and the dev token differ only in operational policy (who holds the key, what it is granted, and when you reach for it), not in code. Do not treat them as two systems.

Break-glass exists so that when the OIDC issuer is down, misconfigured, or otherwise unreachable, an authorized operator can still authenticate to Joe and act.

---

## Configuring a key

### Defining a service account

Service-account keys are defined under `server.service_accounts` in Joe's config file. Each entry is a `name` and a `key`; the principal Joe mints for that entry is `svc:<name>`.

```yaml
server:
  service_accounts:
    - name: breakglass-oncall
      key: "a-long-random-high-entropy-secret"
```

The example above defines the principal `svc:breakglass-oncall`, authenticated by the bearer key shown.

### Keys are plaintext at rest

Service-account keys are stored **in plaintext** in the config file — there is no hashing or encryption at rest. The key *is* the credential: anyone who can read the config file holds the credential. Protect the config file accordingly:

- Restrict file permissions so only the account running `joe` can read it.
- Never commit the config file to version control.
- Treat the file as you would any secret store.

### The one environment-variable case

There is exactly one service account you can provision through the environment. Setting `JOE_API_KEY` provisions only the single reserved account `svc:server` — the co-located / loopback client key that the `joe` CLI on the same host presents. It does not let you name arbitrary accounts.

Any named break-glass account (`svc:breakglass-oncall`, `svc:operator`, and so on) **must be defined in the config YAML under `server.service_accounts`**, not via an environment variable.

---

## Granting admin

A service-account principal becomes admin **exactly like a user principal**. The principal is an opaque string; admin grants do no prefix-specific logic, so granting admin to `svc:breakglass-oncall` is the same operation as granting it to `user:alice@example.com`.

Admin is minted two ways: the **OIDC admin-email bootstrap** (`auth.admin_email` in config) and the **admin REST API** (`/api/v1/admin/admins`). The bootstrap is the only non-circular cold-start path — it is the sole way to create the *first* admin, because the REST endpoints are themselves admin-gated and so cannot bootstrap. Every REST grant writes an append-only `admin_access` audit row in the same transaction as the mutation. There is no longer a CLI path (the operator CLI was removed in identity Stage 4): the audited REST surface is the single writer to admin state.

Grant admin to a service account (requires an existing admin's credential):

```bash
curl -X POST -H "Authorization: Bearer <existing-admin-credential>" \
  -H "Content-Type: application/json" \
  -d '{"principal":"svc:breakglass-oncall","reason":"on-call break-glass for OIDC outages"}' \
  http://localhost:7777/api/v1/admin/admins
```

Revoke it when it is no longer needed:

```bash
curl -X DELETE -H "Authorization: Bearer <existing-admin-credential>" \
  http://localhost:7777/api/v1/admin/admins/svc:breakglass-oncall
```

List the principals that currently hold admin:

```bash
curl -H "Authorization: Bearer <existing-admin-credential>" \
  http://localhost:7777/api/v1/admin/admins
```

---

## Using the key

### Direct request

Authenticate by sending the key in an `Authorization: Bearer` header against any protected endpoint:

```bash
curl -H "Authorization: Bearer a-long-random-high-entropy-secret" \
  http://localhost:7777/api/v1/graph
```

### Through the Web UI

The Web UI exposes the **same path**. When OIDC is enabled, the login screen shows a small disclosure labeled **"Use a service-account key"**; revealing it shows a key field. Enter the break-glass key there and the UI sends it as the same `Authorization: Bearer` header.

The key is held in the browser's `sessionStorage` only — it is cleared when the tab closes and is never written to disk or to a cookie.

The UI path and a direct `curl` (or any API client) are therefore equivalent: same header, same backend resolution to the `svc:<name>` principal. The UI is a convenience front-end over the bearer credential, not a different mechanism.

### Precedence with human sessions

A valid human session cookie takes **precedence** over a bearer key. If a request carries an active human session, that session's `user:` principal is used and the bearer key is ignored. Break-glass is therefore the credential that applies when there is no active human session — which is exactly the situation it exists for.

---

## Confirming break-glass is actually enforced

Auth is enforced only when service accounts **or** OIDC are configured. With **neither** configured, Joe runs permit-all: every request is allowed and a bearer key grants nothing special. Before you rely on break-glass as a real boundary, confirm the running binary is in an enforcing mode.

Check the `joe` startup log. One of these lines tells you the mode:

| Startup log line | What it means for break-glass |
| --- | --- |
| `API authentication enabled (OIDC login + service-account keys)` | Enforcing; service-account keys are loaded — break-glass is a real boundary. |
| `API authentication enabled (service-account keys)` | Enforcing; service-account keys are loaded — break-glass is a real boundary. |
| `API authentication enabled (OIDC login)` | Enforcing, but no service-account keys are configured — there is no break-glass key to present. |
| `API authentication disabled — set auth.oidc.issuer for human login or server.service_accounts for machine access` | Permit-all dev binary — no authentication, bearer keys grant nothing special. |

If you see an "enabled" line that mentions **service-account keys**, your break-glass key is loaded and enforced. If you see the "disabled" warning, you are running a permit-all build and break-glass is not a boundary — configure `server.service_accounts` (and/or OIDC) and restart.

---

## What gets audited, and what the audit proves

Break-glass use is recorded in the append-only, tamper-evident `audit_log`. Each recorded use is a row with:

- `kind` = `auth_login`
- `action` = `break_glass_use`
- the `svc:<name>` principal that was presented
- the source remote address the request came from
- the user agent
- an `allow` decision

Credential use is audited so that every break-glass action leaves a reviewable trail in the audit log.

### What it proves — and what it does not

Be precise about the evidentiary value. An audit row proves that **a given service-account credential was presented from a given source address and was allowed**. It does **not** identify which human used the key. A key shared among several operators cannot be attributed to a specific person from the log alone.

### Deduplication

The log is deduplicated: at most **one row per `(principal, source address)` per window**, where the window is the session TTL. The log therefore shows that break-glass was *active* in a window from a given address — not one row per request. Do not read the row count as a request count.

The dedup state is held in memory. A `joe` restart resets it, so a restart may produce one extra row for an already-seen `(principal, source address)` pair within the same window. This is expected.

### Failure posture: fail-open-but-loud

An audit-write failure does **not** block a break-glass request. This is deliberate: an audit outage must not lock administrators out at the moment they most need access. The failure is logged loudly at error level so it is visible. Treat the posture as **fail-open-but-loud** — access continues, but the failure is surfaced for follow-up.

---

## Operational guidance

Break-glass is an internal-tool capability, not an internet-facing one; keep its handling proportionate to that.

- **Use it for what it is for.** Reach for break-glass when OIDC is unavailable, not as a routine login path.
- **Prefer per-operator named accounts where attribution matters.** Because a shared key is not personally attributable in the audit log, give individual operators their own named service accounts (`svc:operator-alice`, `svc:operator-bob`) when you need to know who acted.
- **Revoke what you no longer need.** Use `DELETE /api/v1/admin/admins/svc:<name>` to drop admin from a key once the situation is resolved, and remove unused keys from the config.
- **Review use in the audit log.** The `audit_log` (`kind` = `auth_login`, `action` = `break_glass_use`) is where break-glass activity is reviewed.

---

## See also

- [joe-identity-design.md](reference/joe-identity-design.md) — the identity and authentication design that break-glass is part of.
- [security-in-layers.md](reference/security-in-layers.md) — the security architecture overview.
