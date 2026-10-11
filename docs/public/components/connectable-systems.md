---
title: Connectable systems
weight: 10
description: The per-type credential mechanism and registration routing for every system you can connect today.
---

# Connectable systems

Each system below has a working adapter and a credential path you can complete through
promotion, and every one comes live at runtime — promotion connects it, and the
connectivity test confirms it — with no daemon restart.

- The **credential mechanism** column tells you what kind of reference promotion expects.
- The **routing config** column names the non-credential fields the component's `config`
  needs at registration. `—` means none; a field in parentheses is optional.
- The **web UI** column tells you whether the whole flow can be done from the web UI. The
  UI's Register dialog cannot yet collect a routing field, so a type that needs one is
  registered through the API — see [Components](../) — and then promoted and tested from
  either surface.

### Kubernetes

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Kubernetes | `kubernetes` | `auth_method: static-bearer` — `api_server` + `ca_data` + a bearer token via `env_var` or `in_cluster: true`; **or** `auth_method: entra-exchange` (AKS) — `api_server` + `ca_data` + `tenant_id` + `client_id` + `audience` + `client_secret_env_var` | — (the cluster coordinates are supplied at promotion) | yes — [guide](../../guides/register-kubernetes/) |

### Source control and code review

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Git | `git` | static / env var (`JOE_GIT_<LABEL>`), **or** explicitly no credential for a public repository | `url` (`branch`, `provider_component_id`) | yes — [guide](../../guides/register-git/) |
| GitHub | `github` | static / env var (`JOE_GITHUB_<LABEL>`) | — (`base_url`, for GitHub Enterprise Server) | yes, for GitHub.com — [guide](../../guides/register-token-components/) |
| GitLab | `gitlab` | static / env var (`JOE_GITLAB_<LABEL>`) | — (`base_url`, for a self-managed instance) | yes, for GitLab.com — [guide](../../guides/register-token-components/) |

### Metrics, logs, and traces

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Prometheus | `prometheus` | static / env var (`JOE_PROMETHEUS_<LABEL>`) | `url` | API only |
| Mimir | `mimir` | static / env var (`JOE_MIMIR_<LABEL>`) | `url` | API only |
| Loki | `loki` | static / env var (`JOE_LOKI_<LABEL>`) | `url` | API only |
| Tempo | `tempo` | static / env var (`JOE_TEMPO_<LABEL>`) | `url` | API only |
| Jaeger | `jaeger` | static / env var (`JOE_JAEGER_<LABEL>`) | `url` | API only |
| Splunk | `splunk` | static / env var (`JOE_SPLUNK_<LABEL>`) | `url` | API only |
| Dynatrace | `dynatrace` | static / env var (`JOE_DYNATRACE_<LABEL>`) | `url` | API only |
| New Relic | `newrelic` | static / env var (`JOE_NEWRELIC_<LABEL>`) | `account_id` (`region`) | API only |

### Alerting

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Alertmanager | `alertmanager` | static / env var (`JOE_ALERTMANAGER_<LABEL>`) | `url` | API only |
| PagerDuty | `pagerduty` | static / env var (`JOE_PAGERDUTY_<LABEL>`) | — (`api_url`, for the EU service region) | yes, for the US service region — [guide](../../guides/register-token-components/) |
| Grafana | `grafana` | static / env var (`JOE_GRAFANA_<LABEL>`) | `url` | API only |

### GitOps and security

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Argo CD | `argocd` | static / env var (`JOE_ARGOCD_<LABEL>`) | `url` | API only |
| Falco | `falco` | static / env var (`JOE_FALCO_<LABEL>`) | `url` | API only |

### Credential-less systems

These connect without any credential — there is nothing to arm. Register them with their
routing config, run the connectivity test, and they function.

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Terraform | `terraform` | none — reads local state | `state_path` | API only |
| Envoy | `envoy` | none — unauthenticated admin API | `url` | API only |
