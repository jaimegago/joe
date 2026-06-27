---
title: Integrations
weight: 60
description: The systems Joe connects to.
---

# Integrations

This section is the practical guide to connecting Joe to the systems it manages. The
shape is always the same — **register, then promote, then arm** — so this page documents
that flow once as the spine, then gives the per-type credential mechanism for each system
you can connect today.

For the *why* behind the two-step lifecycle, read
[Components and promotion](../concepts/components-and-promotion/). For the configuration
of credential references, see [Configuration](../configuration/). All the endpoints below
are admin-gated; authenticate with a service-account bearer key (see
[Install and Build](../install-and-build/)).

## The spine: register → promote → arm

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
        "type": "github",
        "name": "acme-org",
        "config": { "org": "acme" }
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
reference is a kubeconfig locator rather than an enumerable list.

### 3. Promote (arms the component)

Promotion is the single governed path from inert to **armed**. It is admin-gated and
audited, and it writes a credential **reference** — never an inline secret value. A type
with no wired credential provider cannot be promoted at all.

For a static-credential type, set the secret in an environment variable in the daemon's
environment, then reference that variable by name:

```sh
# In the daemon's environment:
export JOE_GITHUB_PROD="ghp_your_token"

curl -s http://localhost:7777/api/v1/components/<id>/promote \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "env_var": "JOE_GITHUB_PROD" }'
```

For Kubernetes, reference a kubeconfig instead — either in-cluster or a kubeconfig path:

```sh
curl -s http://localhost:7777/api/v1/components/<id>/promote \
  -H "Authorization: Bearer $JOE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{ "in_cluster": true }'
# or: -d '{ "kubeconfig": "/path/to/kubeconfig" }'
```

An inline `value` is refused on both paths — the armed record carries a reference, not a
secret. Re-promoting an armed component overwrites its reference as another audited event.

### 4. Test connectivity

Promotion records the reference but does not itself authenticate. Check that the reference
actually works with a separate connectivity test:

```sh
curl -s http://localhost:7777/api/v1/components/<id>/test \
  -H "Authorization: Bearer $JOE_API_KEY"
```

A component armed with a working credential can read with that credential; whether it may
*mutate* its target is governed separately by the [write floor](../concepts/observation-mode-and-the-write-floor/)
and [RBAC](../concepts/rbac-zones-and-read-posture/) — arming a component does not by
itself grant mutation.

## Connectable systems and their credential mechanism

Each system below has a working adapter and a credential path you can complete through
promotion. The **credential mechanism** column tells you what kind of reference promotion
expects.

### Kubernetes

| System | Type | Credential mechanism |
| --- | --- | --- |
| Kubernetes | `kubernetes` | kubeconfig-exec — `in_cluster: true` or a `kubeconfig` path |

### Source control and code review

| System | Type | Credential mechanism |
| --- | --- | --- |
| GitHub | `github` | static / env var (`JOE_GITHUB_<LABEL>`) |
| GitLab | `gitlab` | static / env var (`JOE_GITLAB_<LABEL>`) |

### Metrics, logs, and traces

| System | Type | Credential mechanism |
| --- | --- | --- |
| Prometheus | `prometheus` | static / env var (`JOE_PROMETHEUS_<LABEL>`) |
| Mimir | `mimir` | static / env var (`JOE_MIMIR_<LABEL>`) |
| Loki | `loki` | static / env var (`JOE_LOKI_<LABEL>`) |
| Tempo | `tempo` | static / env var (`JOE_TEMPO_<LABEL>`) |
| Jaeger | `jaeger` | static / env var (`JOE_JAEGER_<LABEL>`) |
| Splunk | `splunk` | static / env var (`JOE_SPLUNK_<LABEL>`) |
| Dynatrace | `dynatrace` | static / env var (`JOE_DYNATRACE_<LABEL>`) |
| New Relic | `newrelic` | static / env var (`JOE_NEWRELIC_<LABEL>`) |

### Alerting

| System | Type | Credential mechanism |
| --- | --- | --- |
| Alertmanager | `alertmanager` | static / env var (`JOE_ALERTMANAGER_<LABEL>`) |
| PagerDuty | `pagerduty` | static / env var (`JOE_PAGERDUTY_<LABEL>`) |
| Grafana | `grafana` | static / env var (`JOE_GRAFANA_<LABEL>`) |

### GitOps and security

| System | Type | Credential mechanism |
| --- | --- | --- |
| Argo CD | `argocd` | static / env var (`JOE_ARGOCD_<LABEL>`) |
| Falco | `falco` | static / env var (`JOE_FALCO_<LABEL>`) |

### Credential-less systems

These connect without any credential — there is nothing to arm. Register them and they
function.

| System | Type | Credential mechanism |
| --- | --- | --- |
| Terraform | `terraform` | none — reads local state |
| Envoy | `envoy` | none — unauthenticated admin API |

## Front-end integrations

Beyond managed systems, Joe ships two front-ends that connect *to* a running daemon over
HTTP. Both are subcommands of the same binary.

### MCP server (editors)

`joe mcp` exposes Joe over the Model Context Protocol so an editor (Claude Code, Cursor,
and other MCP clients) can query live infrastructure. It reads `JOE_SERVER` and
`JOE_API_KEY` from its environment.

```json
{
  "mcpServers": {
    "joe": {
      "command": "joe",
      "args": ["mcp"],
      "env": {
        "JOE_SERVER": "http://localhost:7777",
        "JOE_API_KEY": "<service-account-key>"
      }
    }
  }
}
```

It registers eight tools: `joe_graph_query`, `joe_graph_related`, `joe_k8s`,
`joe_metrics`, `joe_logs`, `joe_traces`, `joe_alerts`, and `joe_knowledge_search`. The
observability tools take a service and a natural-language question — Joe resolves the
backend from the graph.

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

## Not yet supported

You will see other component types named in the type enum, but they cannot be completed
into a working integration at launch and are not documented here: `azure`, `helm`,
`nginx-ingress`, `git`, `aws`, `datadog`, `postgresql`, `mysql`, `redis`, `mongodb`,
`kafka`, `elasticsearch`, `cloudwatch`, `azuremonitor`, `oci_registry`, `dockerhub`,
`artifactory`, and `ecr`. They are either not yet connected, have no governed credential
path, or have no usable adapter. Do not rely on them yet.

## Where to go next

- Why registration and promotion are split → [Components and promotion](../concepts/components-and-promotion/)
- Credential references and the full environment surface → [Configuration](../configuration/)
- Running and observing Joe → [Operations](../operations/)
