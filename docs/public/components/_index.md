---
title: Components
weight: 60
description: The systems Joe connects to.
aliases:
  - /docs/integrations/
---

# Components

A **component** is Joe's name for a system it manages — a Kubernetes cluster, a Prometheus,
a GitHub org, a Grafana. Components anchor the
[knowledge graph](../concepts/knowledge-graph/) — Joe hangs the resources it discovers
inside them off these anchors as further nodes — and they are the things zones and RBAC
grant access to. Everything Joe does — answering questions, walking the graph, proposing changes —
happens through the components you register. Without them, Joe boots, authenticates, and
answers, but has nothing real to say.

This section is the practical guide to connecting Joe to the systems it manages. Every
system follows the same first three steps — **register, then promote, then arm** — but the
final **activation** step differs by type. Most types come live at runtime through a
connectivity test; a few can only be brought live at the next daemon restart. This page
documents the shared flow once, flags which activation path each type takes, then points you
to [Connectable systems](connectable-systems/) for the per-type credential mechanism of each
system you can connect today.

For the *why* behind the two-step lifecycle, read
[The component lifecycle](../concepts/component-lifecycle/). For the configuration
of credential references, see [Configuration](../configuration/). All the endpoints below
are admin-gated; authenticate with a service-account bearer key (see
[Install and Build](../install-and-build/)).

## The spine: register → promote → arm → activate

A managed system is a **component**. Bringing one under Joe's management is two separate
decisions, and the split is a security boundary.

### 1. Register (lands inert)

Registering a component creates a record and nothing more — no connection, no credential,
no network call. The component lands **inert** and read-only. Credential-bearing fields
presented at registration are **rejected**, not stored: you cannot supply a secret here.

```sh
curl -s http://localhost:7777/api/v1/components \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
        "type": "prometheus",
        "name": "prod-metrics",
        "config": { "url": "http://prometheus.monitoring:9090" }
      }'
```

The `config` carries only non-credential routing fields (endpoint, org, path, and so on).

### 2. Inspect what arming requires

Before promoting, ask the component what its credential reference must look like and which
references are available right now:

```sh
curl -s http://localhost:7777/api/v1/components/<id>/promotion-requirements \
  -H "Authorization: Bearer $JOE_API_KEY"

curl -s http://localhost:7777/api/v1/components/<id>/promotion-candidates \
  -H "Authorization: Bearer $JOE_API_KEY"
```

For a static-credential type, the candidates are the matching `JOE_<SEGMENT>_<LABEL>`
environment variables currently set in the daemon's environment. For Kubernetes the
reference is the cluster coordinates plus a free-form bearer-token source rather than an
enumerable list.

### 3. Promote (arms the component)

Promotion is the single governed path from inert to **armed**. It is admin-gated and
audited, and it writes a credential **reference** — never an inline secret value. A type
with no wired credential provider cannot be promoted at all.

For a static-credential type, set the secret in an environment variable in the daemon's
environment, then reference that variable by name:

```sh
# In the daemon's environment:
export JOE_PROMETHEUS_PROD="your-bearer-token"

curl -s http://localhost:7777/api/v1/components/<id>/promote \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "env_var": "JOE_PROMETHEUS_PROD" }'
```

The environment variable name must be **unique per component**: each component
references its own variable, and promotion rejects a name already in use by
another component (environment variables are process-global, so two components
sharing a name would read the same secret).

For Kubernetes, reference the cluster coordinates plus a credential for one of the two
authentication methods. The **static-bearer** method takes a service-account token from an
environment variable, or the in-cluster pod-mounted token:

```sh
curl -s http://localhost:7777/api/v1/components/<id>/promote \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "auth_method": "static-bearer", "api_server": "https://api.cluster.example.com:6443", "ca_data": "<PEM>", "env_var": "JOE_KUBERNETES_PROD_TOKEN" }'
# or, in-cluster: -d '{ "auth_method": "static-bearer", "api_server": "...", "ca_data": "<PEM>", "in_cluster": true }'
```

The **entra-exchange** method (AKS) mints a short-lived bearer token via an Azure Entra
OAuth2 client-credentials exchange. Reference the same cluster coordinates plus the tenant,
client (application) ID, the audience the token is scoped to, and a `client_secret_env_var`
naming the variable that holds the app registration's client secret. That secret field is
distinct from the static-bearer `env_var`, so the same client secret **may be shared** by
several clusters (one app registration fronting many):

```sh
curl -s http://localhost:7777/api/v1/components/<id>/promote \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "auth_method": "entra-exchange", "api_server": "https://aks.example.com:443", "ca_data": "<PEM>", "tenant_id": "<tenant>", "client_id": "<app-id>", "audience": "<aks-audience>", "client_secret_env_var": "JOE_AZURE_APP_SECRET" }'
```

An inline `value` is refused on both paths — the armed record carries a reference, not a
secret. Re-promoting an armed component overwrites its reference as another audited event.

### 4. Activate the component

Promotion records the reference but does not itself authenticate or open a live connection.
How an armed component becomes live depends on its type, and the two paths are **not**
interchangeable — sending a boot-only type down the runtime path silently does nothing.
The per-type tables on [Connectable systems](connectable-systems/) mark which path each
type takes.

#### Runtime activation (most types)

For a **runtime-registerable** type, a connectivity test constructs the adapter,
authenticates with the armed reference, and registers the live connection immediately — no
restart needed:

```sh
curl -s http://localhost:7777/api/v1/components/<id>/test \
  -H "Authorization: Bearer $JOE_API_KEY"
```

A response of `"ok": true, "message": "connection successful"` means the component is live
now and its tools are usable.

#### Boot activation (boot-config-only types)

A few types — `github`, `gitlab`, `splunk`, `dynatrace`, and `newrelic` — have **no
runtime construction path**. For these, the connectivity test cannot build an adapter: it
returns `"ok": true` with a message like `type "github" has no connection to test` and
registers **nothing**. Do not read that as success — the component is still not live.

These types come live only at daemon **startup**. The full bring-up is:

1. Register the component (step 1) and promote it (step 3), exactly as above — both work at
   runtime and land the armed record in the store.
2. Make sure the component's static credential environment variable
   (`JOE_GITHUB_<LABEL>`, `JOE_GITLAB_<LABEL>`, `JOE_SPLUNK_<LABEL>`,
   `JOE_DYNATRACE_<LABEL>`, or `JOE_NEWRELIC_<LABEL>`) is set in the daemon's environment.
3. **Restart the daemon.** On boot Joe loads every armed component of these types from the
   store and opens its connection.

```sh
# In the daemon's environment, before (re)starting:
export JOE_GITHUB_PROD="ghp_your_token"
# ...then restart the joe process.
```

A component armed with a working credential can read with that credential; whether it may
*mutate* its target is governed separately by the [write floor](../concepts/observation-mode-and-the-write-floor/)
and [RBAC](../concepts/rbac-zones-and-read-posture/) — arming a component does not by
itself grant mutation.

## Front-end integrations

Beyond managed systems, Joe ships two front-ends that connect *to* a running daemon over
HTTP. Both are subcommands of the same binary.

### MCP server (editors)

`joe mcp` exposes Joe over the Model Context Protocol so an editor (Claude Code, Cursor,
and other MCP clients) can query live infrastructure. Setup and the full tool surface are
documented in [Guides](../guides/).

### Slack bot

`joe slack` connects Joe to Slack over Socket Mode (no public URL required). It requires
`SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN`, and reads `JOE_SERVER` / `JOE_API_KEY`.

```sh
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_APP_TOKEN="xapp-..."
export JOE_SERVER="http://localhost:7777"
export JOE_API_KEY="<service-account-key>"

joe slack
```

## Where to go next

- The per-type credential mechanism and activation path for every connectable system → [Connectable systems](connectable-systems/)
- Why registration and promotion are split → [The component lifecycle](../concepts/component-lifecycle/)
- Credential references and the full environment surface → [Configuration](../configuration/)
- Running and observing Joe → [Operations](../operations/)
