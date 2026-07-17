---
title: API Reference
weight: 90
description: The HTTP surface.
---

# API Reference

This is the reference for Joe's HTTP API: every operator- and user-facing endpoint,
the method and path it answers on, the authentication it requires, and the request and
response shapes it actually reads and returns. It is a lookup surface. For *why* a
mechanism exists see [Concepts](../concepts/); for the procedure that uses an endpoint
see [Guides](../guides/).

## Base path and transport

All API endpoints live under `/api/v1` and are served on the daemon's API port (default
`7777`, set by `server.address` — see [Configuration](../configuration/)). Requests and
responses are JSON unless noted. The Prometheus `/metrics` endpoint is **not** part of
this API — it is served on a separate metrics port and is documented under
[Operations](../operations/).

## Authentication tiers

Every endpoint falls into one of three tiers. The tier is named in each entry below.

- **No-auth** — reachable without a credential. Only the paths under `/api/v1/auth/`
  are public.
- **Authenticated** — requires a resolved principal: a human session cookie or a
  service-account bearer key. Unauthenticated requests on these paths receive `401`. The
  daemon refuses to start without an identity configuration, so in a running deployment
  every non-public request carries a principal. See
  [Principals and identity](../concepts/principals-and-identity/).
- **Admin-gated** — additionally requires that the principal hold the admin capability.
  Non-admins receive `403`. See
  [RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/).

A human presents its session via the `joe_session` cookie set by the login flow; a
machine presents its key as `Authorization: Bearer <key>`.

Component-scoped reads pass through the read-posture and RBAC layer in addition to the
tier above. In the default `team_flat` posture every authenticated principal reads every
component; under `zoned` a read requires a grant. Mutating a component is governed
independently by RBAC and the write floor regardless of read posture — see
[RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/) and
[Observation mode and the write floor](../concepts/observation-mode-and-the-write-floor/).

## Conventions

- **Path parameters** are written `{name}` and substituted into the path.
- **Errors** are returned as `{"error": "<code>", "message": "<text>", "details": {…}}`
  with the matching HTTP status; `details` is present only when the handler supplies it.
- **Timestamps** are RFC 3339 strings.

---

## Status and version

### `GET /api/v1/status`

The liveness surface. Returns the running status, the build version, and the server
time. **Auth:** authenticated.

Response `200`:

```json
{ "status": "ok", "version": "v1.2.3", "time": "2026-06-28T12:00:00Z" }
```

There is no separate `/livez`, `/readyz`, or `/healthz` endpoint; this is the liveness
surface.

### `GET /api/v1/version`

Returns the full build-identity record. **Auth:** authenticated.

Response `200`:

```json
{
  "version": "v1.2.3",
  "commit": "abc1234",
  "build_time": "2026-06-28T00:00:00Z",
  "ui_digest": "sha256:…"
}
```

`ui_digest` is a sha256 over the embedded UI bytes the binary serves; `/status` does not
carry it.

---

## Mutate status and panic

### `GET /api/v1/mutate-status`

Reports whether Joe may currently mutate the managed system, and why, from the
boot-resolved write floor. **Auth:** authenticated.

Response `200`:

```json
{ "can_mutate": false, "reason": "observation" }
```

`reason` is one of `full` (floor down, mutates permitted subject to RBAC),
`observation` (booted into observation mode), or `safe_mode` (panic recovery). The
`full` value is part of the enum but is **not currently returned** — the governed
full-capabilities mode that brings the floor down is not yet implemented, so a shipped
Joe reports `observation` or `safe_mode`. See
[Observation mode and the write floor](../concepts/observation-mode-and-the-write-floor/).

### `POST /api/v1/panic`

Triggers emergency shutdown: persists the panic state and schedules a process exit so the
daemon restarts into safe mode. **Auth:** authenticated.

Request body (optional):

```json
{ "reason": "string" }
```

Response `200`:

```json
{ "acknowledged": true, "message": "emergency shutdown initiated — joe will restart in safe mode" }
```

The call is idempotent — triggering while already panicked returns `acknowledged: true`
with a message noting the existing state. There is no HTTP endpoint to clear safe mode;
recovery is the `joe unlock` CLI plus a restart, documented under
[Operations](../operations/).

### `GET /api/v1/panic/status`

Reports the current safe-mode status. **Auth:** authenticated.

Response `200` (not in safe mode):

```json
{ "safe_mode": false }
```

Response `200` (in safe mode), enriched from the persisted panic row when available:

```json
{
  "safe_mode": true,
  "triggered_at": "2026-06-28T12:00:00Z",
  "trigger_source": "api",
  "trigger_reason": "string"
}
```

---

## Authentication

These endpoints are public (no-auth) and active only when OIDC is configured. See
[Principals and identity](../concepts/principals-and-identity/).

### `GET /api/v1/auth/config`

Reports whether human (OIDC) login is available, so a client knows whether to offer the
login button. **Auth:** no-auth.

Response `200`:

```json
{ "oidc_enabled": true }
```

### `GET /api/v1/auth/login`

Begins the OIDC login flow. Sets a short-lived state cookie and redirects (`302`) to the
identity provider. **Auth:** no-auth. No JSON body.

### `GET /api/v1/auth/callback`

The OIDC redirect target. Reads `state`, `code`, and an optional `error` query
parameter, validates the flow, mints a session, sets the session cookie, and redirects
(`302`) to the post-login path. On failure it returns the standard JSON error shape.
**Auth:** no-auth.

### `POST /api/v1/auth/logout`

Revokes the caller's server-side session and clears the session cookie. **Auth:**
no-auth (it acts on the presented cookie). No request body.

Response `200`:

```json
{ "status": "logged out" }
```

### `GET /api/v1/me`

Returns the resolved caller: the principal, whether it is an admin, whether RBAC and OIDC
are active, and the zones the principal may act in. **Auth:** authenticated.

Response `200`:

```json
{
  "principal": "user:alice@example.com",
  "is_admin": true,
  "rbac_enabled": true,
  "oidc_enabled": true,
  "zones": [ { "id": "prod-readonly", "allowed_actions": ["read"] } ]
}
```

---

## Sessions

Chat sessions are owned and lifecycle-managed. A non-owner's access is read-only or
denied per the sharing rules in [Chat sessions](../concepts/chat-sessions/). All session
endpoints are **authenticated**; ownership is enforced inside the handler. A session
object (`webUISession`) carries:

```json
{
  "id": "string",
  "started_at": "2026-06-28T12:00:00Z",
  "last_activity_at": "2026-06-28T12:05:00Z",
  "summary": "string",
  "message_count": 12,
  "title": "string",
  "read_only": false,
  "linked_incident_id": "string",
  "linked_incident_title": "string",
  "incident_involved": false,
  "type": "string",
  "incident_state": "string",
  "shared_by": "string",
  "creator_principal": "string",
  "trashed_at": "2026-06-28T12:00:00Z",
  "archived_at": "2026-06-28T12:00:00Z",
  "purge_after": "2026-06-28T12:00:00Z"
}
```

### `GET /api/v1/sessions`

Lists sessions. Query parameters: `limit` (default 20), `mine` (`true` restricts to the
caller's own). Response `200`: `{ "sessions": [ … ], "count": N }`.

### `POST /api/v1/sessions`

Creates a new session owned by the caller. No request body. Response `201`: the session
object.

### `GET /api/v1/sessions/trash`

Lists the caller's trashed sessions. Query parameter: `limit` (default 20). Response
`200`: `{ "sessions": [ … ], "count": N }`.

### `GET /api/v1/sessions/{id}`

Returns one session. Response `200`: the session object. `404` if not found or not
visible to the caller.

### `GET /api/v1/sessions/{id}/messages`

Returns the session's messages. Response `200`: `{ "messages": [ … ], "count": N }`,
where each message is:

```json
{
  "id": "string",
  "session_id": "string",
  "role": "user",
  "content": "string",
  "tool_name": "string",
  "created_at": "2026-06-28T12:00:00Z"
}
```

### `PATCH /api/v1/sessions/{id}`

Renames a session. Request body `{ "title": "string" }` (required, non-empty). Response
`200`: the updated session. `400` on an empty title, `404` if not owned.

### `DELETE /api/v1/sessions/{id}`

Moves a session to trash. Response `204`. `404` if not owned.

### `POST /api/v1/sessions/{id}/restore`

Restores a trashed session. No request body. Response `200`: the session. `409` if the
session is not in trash.

### `POST /api/v1/sessions/{id}/link-incident`

Links the session to the active incident. No request body. Response `200`: the session.
`409` if there is no active incident or the link is not allowed. See
[Incidents](../guides/incidents/).

---

## Tasks

A task runs the agent loop against a message and returns the transcript. Both endpoints
are **authenticated**; when `session_id` is supplied, session ownership applies. See
[The agent loop and autonomy](../concepts/agent-loop-and-autonomy/).

Request body (shared by both endpoints):

```json
{
  "message": "string",
  "session_id": "string",
  "config": {
    "max_iterations": 10,
    "safety_tier": "string",
    "timeout": "30s",
    "allowed_zones": ["string"],
    "allowed_namespaces": ["string"],
    "namespace_zones": { "namespace": "zone" }
  }
}
```

`config` and its fields are optional.

### `POST /api/v1/tasks`

Runs the task to completion and returns the full result synchronously. Response `200`:

```json
{
  "task_id": "string",
  "session_id": "string",
  "status": "string",
  "iterations": 3,
  "steps": [ { "step_number": 1, "llm_request": {…}, "llm_response": {…}, "tool_results": [ … ] } ],
  "final_answer": "string",
  "tools_used": ["string"],
  "total_tokens": { "input_tokens": 100, "output_tokens": 50 },
  "duration_ms": 1234,
  "error": "string",
  "error_code": "string"
}
```

Each step carries the LLM request (`message_count`, `tools_available`), the LLM response
(`content`, optional `tool_calls`, `usage`), and any `tool_results`. The response also
surfaces context-management fields (`history_trimmed`, `messages_dropped`,
`tool_results_truncated`, `user_message_truncated`, `context_window_tokens`) when they
apply.

### `POST /api/v1/tasks/stream`

Runs the task and streams progress as Server-Sent Events
(`Content-Type: text/event-stream`). Each loop iteration emits a `step` event whose data
is a task step; a terminal `final` event carries the same object as the synchronous
`POST /api/v1/tasks` response.

---

## Components

A component is a registered external system. Listing and reading types is
**authenticated**; registering, deleting, inspecting promotion, and promoting are
**admin-gated**. Registration accepts no credential material — a component is armed
later by promotion, which stores a credential *reference* (an env-var name), never a
secret value. See [The component lifecycle](../concepts/component-lifecycle/) and
[Components](../components/).

### `GET /api/v1/component-types`

Lists the supported component types. **Auth:** authenticated. Response `200`:
`{ "component_types": ["string"], "count": N }`.

### `GET /api/v1/components`

Lists registered components as a read-model projection. **Auth:** authenticated.
Response `200`: `{ "components": [ … ], "count": N }`, where each entry is:

```json
{
  "id": "string",
  "type": "string",
  "name": "string",
  "armed": false,
  "provider": "string",
  "status": "string",
  "last_sync_at": "2026-06-28T12:00:00Z",
  "last_error": "string",
  "created_at": "2026-06-28T12:00:00Z",
  "updated_at": "2026-06-28T12:00:00Z"
}
```

### `POST /api/v1/components`

Registers a component. **Auth:** admin-gated. Request body:

```json
{ "id": "string", "type": "string", "name": "string", "config": { } }
```

Credential-bearing fields in `config` are rejected. Response `201`: the stored component
(`id`, `type`, `name`, `config`, `status`, `last_sync_at`, `last_error`, `created_at`,
`updated_at`). `409` if the id exists, `400` if the type is unsupported.

### `GET /api/v1/components/{id}`

Returns one component as the read-model projection (the same shape as the list entry; the
raw config is not echoed). **Auth:** authenticated. `404` if not found.

### `DELETE /api/v1/components/{id}`

Removes a component and unregisters its adapter. **Auth:** admin-gated. Response `204`.

### `GET /api/v1/components/{id}/promotion-requirements`

Describes what arming this component's type requires — the credential provider kind and
its locator fields and constraints — without contacting any backend. **Auth:**
admin-gated. Response `200` for a wired type:

```json
{ "type": "string", "wired": true, "kind": "string", "locator_fields": [ … ], "constraints": [ … ] }
```

For an unwired type the response is `{ "type": …, "wired": false, "armable_types": [ … ] }`.

### `GET /api/v1/components/{id}/promotion-candidates`

Lists the environment-variable *names* that could supply this component's credential
reference, reflecting the live environment. Names only — never values. **Auth:**
admin-gated. Response `200` for a wired type:

```json
{
  "type": "string",
  "wired": true,
  "kind": "string",
  "prefix": "JOE_<SEGMENT>_",
  "applicable": true,
  "candidates": [ { "label": "string", "env_var_name": "string" } ]
}
```

For an unwired type the response mirrors promotion-requirements
(`{ "type": …, "wired": false, "armable_types": [ … ] }`).

### `POST /api/v1/components/{id}/promote`

Arms a component by attaching a credential reference. **Auth:** admin-gated. Request
body (fields depend on the provider):

```json
{
  "credential_provider": "string",
  "env_var": "string",
  "value": "string",
  "auth_method": "static-bearer | entra-exchange",
  "api_server": "string",
  "ca_data": "string",
  "namespace": "string",
  "in_cluster": false,
  "tenant_id": "string",
  "client_id": "string",
  "client_secret_env_var": "string",
  "audience": "string"
}
```

For a static-credential component the reference is an `env_var`. For Kubernetes the
reference carries the cluster coordinates (`api_server`, `ca_data`, optional `namespace`)
plus a credential for one of two `auth_method` values:

- **`static-bearer`** — a bearer-token source, either an `env_var` name or
  `in_cluster: true` (the pod-mounted service-account token).
- **`entra-exchange`** (AKS) — Joe mints a short-lived bearer token via an Azure Entra
  OAuth2 client-credentials exchange, using `tenant_id`, `client_id`, `audience` (the
  scope), and `client_secret_env_var` (the variable holding the app registration's client
  secret). `client_secret_env_var` is a distinct field from the static-bearer `env_var`
  and is exempt from the env-var uniqueness guard, so one app registration's client secret
  may be shared across clusters.

An inline `value` (an embedded secret) is refused; every reference must be an indirection.
Promotion resolves no credential. Response `200`:

```json
{ "component_id": "string", "type": "string", "provider": "string", "armed": true, "rearm": false }
```

### `POST /api/v1/components/{id}/test`

Tests connectivity for a registered component. **Auth:** authenticated. No request body.
Response `200`: `{ "ok": true, "message": "string" }`. `404` if the component is not
found.

---

## Observe (category-based)

These endpoints answer a natural-language question against a category of telemetry. Joe
resolves the backing component for the named service through the knowledge graph,
translates the question to a native query, executes it, and returns a normalized result.
All are **authenticated** (component reads are subject to read posture). See
[The knowledge graph](../concepts/knowledge-graph/) and the
[Knowledge graph guide](../guides/knowledge-graph/).

Shared request body:

```json
{ "service": "string", "question": "string" }
```

For `metrics`, `logs`, `traces`, and `k8s`, both fields are required; for `alerts`,
`service` is required and `question` is an optional filter. A `404` is returned when no
backing component for the service is found via the appropriate graph edge.

### `POST /api/v1/observe/metrics`
### `POST /api/v1/observe/logs`
### `POST /api/v1/observe/traces`

Each returns a normalized observability result. Response `200`:

```json
{
  "source": "string",
  "component_id": "string",
  "native_query": "string",
  "data": [ { "timestamp": "2026-06-28T12:00:00Z", "labels": { }, "value": 0 } ],
  "raw_result": { }
}
```

### `POST /api/v1/observe/alerts`

Resolves the alerts (or paging) backend for the service and returns its alerts. Response
`200`:

```json
{
  "source": "string",
  "component_id": "string",
  "alerts": [ { "name": "string", "state": "string", "labels": { }, "summary": "string" } ],
  "count": 0
}
```

### `POST /api/v1/observe/k8s`

Resolves the Kubernetes backend for the service and returns pods (or pod logs when the
question mentions logs). Response `200`:

```json
{ "source": "kubernetes", "component_id": "string", "native_query": "string", "data": { } }
```

---

## Knowledge graph

The graph query endpoints are **authenticated**. The knowledge graph models
infrastructure relationships; see [The knowledge graph](../concepts/knowledge-graph/).

### `GET /api/v1/graph/query`

Searches for nodes. Query parameter: `q` (required). Response `200`:
`{ "nodes": [ … ], "count": N }`, where each node carries `ID`, `Type`, `ComponentID`,
`Metadata`, `FirstSeen`, `LastSeen`. `400` if `q` is missing.

### `GET /api/v1/graph/related`

Returns the subgraph around a node. Query parameters: `nodeID` (required), `depth`
(optional, default 1). Response `200`: a subgraph `{ "Nodes": [ … ], "Edges": [ … ] }`;
each edge carries `From`, `To`, `Relation`, `Confidence`, `Source`, `ComponentID`,
`Context`, `CreatedAt`. `404` if the node is not found.

### `GET /api/v1/graph/summary`

Returns graph totals. Response `200`: `{ "NodeCount", "EdgeCount", "NodesByType",
"RecentlyAdded", "RecentlyUpdated" }`.

### Web-UI graph views

The web UI consumes a presentation-shaped view of the same graph. These are
**authenticated**.

- `GET /api/v1/graph` — the full graph as `{ "nodes": [ … ], "edges": [ … ] }`, where a
  node carries `id`, `kind`, `name`, `namespace`, `cluster`, `metadata`, `labels`,
  `status` and an edge carries `id`, `source`, `target`, `type`.
- `GET /api/v1/graph/node/{id}` — one node in the same node shape; `404` if not found.
- `GET /api/v1/graph/node/{id}/related` — `{ "nodes", "edges" }` around the node; query
  parameter `depth` (default 1).

---

## Incident regime

These endpoints read and change the install-wide incident regime and a session's
incident state. Reading the regime is **authenticated**. Declaring, resolving, promoting,
and advancing state require the principal to hold access to the `regime-control` zone —
authorization is enforced inside the handler and refused with `403`; they also return
`503` when RBAC is not configured. See [The incident regime](../concepts/incident-regime/)
and [Incidents](../guides/incidents/).

### `GET /api/v1/regime`

Returns the current regime, enriched with the active incident's session id, state, and
title when one is active. Response `200`:

```json
{
  "Mode": "normal",
  "DeclaredAt": "2026-06-28T12:00:00Z",
  "DeclaredByPrincipal": "user:alice@example.com",
  "DeclaredKind": "human",
  "IncidentSessionID": "string",
  "IncidentState": "string",
  "IncidentTitle": "string"
}
```

### `POST /api/v1/regime/declare`

Declares an incident regime and makes the declaring human captain of the named session.
Request body `{ "session_id": "string", "declared_kind": "human" }` (`session_id`
required; `declared_kind` defaults to `human` — `joe` is refused with `403`). Response
`201`: `{ "session_id": "string", "captain_id": "string", "declared_by": "string" }`.
`409` if a regime or session is already an incident.

### `POST /api/v1/regime/resolve`

Resolves the active incident regime. Optional body `{ "as_joe": false }`. Response `200`:
`{ "session_id": "string", "resolved_by": "string" }`. `409` if there is no active
incident or it is not yet mitigated.

### `POST /api/v1/sessions/{id}/promote-incident`

Promotes an existing session to the incident master in place. Optional body
`{ "declared_kind": "human" }`. Response `201`: same shape as declare. Same authorization
and conflict semantics as declare.

### `POST /api/v1/sessions/{id}/incident-state`

Advances the incident's state. Request body
`{ "state": "being_worked" | "believed_mitigated" }` (required). Response `200`:
`{ "session_id": "string", "incident_state": "string" }`. `409` if the session is not an
incident or the incident is already terminal.

---

## Admin surface

Every endpoint under `/api/v1/admin/` is **admin-gated**: a non-admin principal receives
`403` and the attempt is audited. These endpoints govern RBAC, identity, read posture,
and credential diagnostics. See
[RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/) and
[Principals and identity](../concepts/principals-and-identity/).

### Zones

- `GET /api/v1/admin/zones` — `{ "zones": [ … ], "count": N }`; a zone is
  `{ "id", "name", "description", "allowed_actions", "created_at" }`.
- `POST /api/v1/admin/zones` — body a zone (`allowed_actions` defaults to `["read"]`).
  Response `201`: the zone.
- `PATCH /api/v1/admin/zones/{id}` — partial update
  `{ "name", "description", "allowed_actions" }`. Response `200`: the zone.
- `DELETE /api/v1/admin/zones/{id}` — `{ "status": "deleted", "id": "string" }`.

### Component-zone assignments

- `GET /api/v1/admin/component-zones` — `{ "assignments": [ … ], "count": N }`; an
  assignment is `{ "component_id", "zone_id", "assigned_by", "reason", "assigned_at" }`.
- `POST /api/v1/admin/component-zones` — body
  `{ "component_id", "zone_id", "assigned_by", "reason" }`. Response `200`: the
  assignment.
- `DELETE /api/v1/admin/component-zones/{componentID}` — `{ "component_id", "removed",
  "note" }`; the component falls back to the default unassigned zone.

### Policies

- `GET /api/v1/admin/policies` — `{ "policies": [ … ], "count": N }`; a policy is
  `{ "id", "principal", "zone_id", "created_at" }`.
- `POST /api/v1/admin/policies` — body `{ "principal", "zone_id" }` (`principal` must be
  prefixed `user:`, `group:`, or `svc:`). Response `201`: the policy.
- `POST /api/v1/admin/policies/revoke` — body `{ "principal", "zone_id" }`. Response
  `200`: `{ "status": "revoked", "principal", "zone_id", "removed" }`.
- `DELETE /api/v1/admin/policies/{id}` — `{ "status": "deleted" }`.
- `GET /api/v1/admin/unassigned` — components with no zone:
  `{ "component_ids": ["string"], "count": N }`.

### Admin roster

- `GET /api/v1/admin/admins` — `{ "admins": [ … ], "count": N }`; an entry is
  `{ "principal", "granted_at", "granted_by", "reason" }`.
- `POST /api/v1/admin/admins` — body `{ "principal", "reason" }`. Response `200`:
  `{ "principal", "granted" }`.
- `DELETE /api/v1/admin/admins/{principal}` — `{ "principal", "removed" }`.

### Principals

- `GET /api/v1/admin/principals` — `{ "principals": [ … ], "count": N }`; a record is
  `{ "principal", "created_at", "status", "disabled_at", "disabled_by", "display_name",
  "last_seen_at" }`.
- `POST /api/v1/admin/principals/{principal}/disable` — `{ "principal", "status":
  "disabled", "sessions_revoked", "registry_modified" }`.
- `POST /api/v1/admin/principals/{principal}/enable` — `{ "principal", "status":
  "active" }`.

### Read posture and read promotions

- `GET /api/v1/admin/read-posture` — `{ "posture": "team_flat" | "zoned" }`.
- `POST /api/v1/admin/read-posture` — body `{ "posture": "team_flat" | "zoned" }`.
  Response `200`: `{ "posture": "string" }`.
- `GET /api/v1/admin/read-promotions` — `{ "read_promotions": [ { "component_type",
  "enabled" } ], "count": N }`.
- `POST /api/v1/admin/read-promotions` — body `{ "component_type", "enabled" }`. Response
  `200`: `{ "component_type", "enabled" }`.

### Credential status

- `GET /api/v1/admin/credential-status` — credential descriptors for registered
  components, with no backend contact: `{ "statuses": [ … ], "count": N }`. A status is
  `{ "component_id", "type", "name", "descriptor": { "provider", "audience", "context",
  "expires_at" }, "error" }`.
- `POST /api/v1/admin/credential-status/{componentID}/probe` — performs a live resolve
  and probe: `{ "component_id", "diagnostic": { "component_id", "provider", "audience",
  "expires_at", "stage", "ok", "reason" }, "stderr_available" }`.
- `POST /api/v1/admin/credential-status/{componentID}/probe/stderr` — returns captured
  plugin stderr from the last probe: `{ "component_id", "stderr" }`.

---

## Endpoints not documented individually

The following served endpoints exist but are outside the operator- and user-facing
surface above. They are grouped here rather than documented one by one.

- **Agent-runtime and captaincy surface** — the persistence and coordination endpoints
  the agent loop and captaincy transfer drive internally, under
  `/api/v1/sessions/{id}/runs`, `/api/v1/runs/{id}/…`, `/api/v1/solicitations/{id}/…`,
  `/api/v1/sessions/{id}/findings`, and `/api/v1/sessions/{id}/captain/…`. These are
  machine-facing and not part of the stable public surface.
- **Typed per-backend passthroughs** — direct, component-scoped read endpoints for
  specific backends (Alertmanager, PagerDuty, Grafana, Falco, nginx, Envoy, the
  datastores PostgreSQL/MySQL/Redis/MongoDB/Kafka/Elasticsearch, and the OCI/Artifactory/
  ECR registries) under their own `/api/v1/<backend>/{componentID}/…` paths. These are
  the raw layer beneath the category-based [observe](#observe-category-based) endpoints;
  prefer the observe endpoints.
- **LLM instrumentation and skills** — model and LLM-settings management
  (`/api/v1/models…`, `/api/v1/llm/settings…`, `/api/v1/llm/usage…`,
  `/api/v1/llm/providers`) and skills management (`/api/v1/skills…`). LLM-settings writes
  and the per-principal usage breakdown are admin-gated; the rest are authenticated.
- **Warnings** — `/api/v1/warnings`.

There is no `POST /api/v1/chat` endpoint; conversational turns go through
[Tasks](#tasks) and are recorded against [Sessions](#sessions). There is no HTTP endpoint
to leave safe mode — see [panic](#mutate-status-and-panic) and [Operations](../operations/).

## Where to go next

- Why the surface is governed the way it is → [Concepts](../concepts/)
- Task-focused procedures that use these endpoints → [Guides](../guides/)
- Running, observing, and recovering the daemon → [Operations](../operations/)
- The config and environment behind the server → [Configuration](../configuration/)
