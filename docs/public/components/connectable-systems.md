---
title: Connectable systems
weight: 10
description: The per-type credential mechanism and activation path for every system you can connect today.
---

# Connectable systems

Each system below has a working adapter and a credential path you can complete through
promotion. The **credential mechanism** column tells you what kind of reference promotion
expects; the **activation** column tells you whether it comes live at runtime (the `/test`
call) or only at the next daemon restart (boot).

### Kubernetes

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| Kubernetes | `kubernetes` | `auth_method: static-bearer` — `api_server` + `ca_data` + a bearer token via `env_var` or `in_cluster: true`; **or** `auth_method: entra-exchange` (AKS) — `api_server` + `ca_data` + `tenant_id` + `client_id` + `audience` + `client_secret_env_var` | runtime (`/test`) |

For the click-by-click web-UI walkthrough of registering, promoting, and taking a cluster
live, see [Register a Kubernetes component](../../guides/register-kubernetes/).

### Source control and code review

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| GitHub | `github` | static / env var (`JOE_GITHUB_<LABEL>`) | boot (restart) |
| GitLab | `gitlab` | static / env var (`JOE_GITLAB_<LABEL>`) | boot (restart) |

### Metrics, logs, and traces

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| Prometheus | `prometheus` | static / env var (`JOE_PROMETHEUS_<LABEL>`) | runtime (`/test`) |
| Mimir | `mimir` | static / env var (`JOE_MIMIR_<LABEL>`) | runtime (`/test`) |
| Loki | `loki` | static / env var (`JOE_LOKI_<LABEL>`) | runtime (`/test`) |
| Tempo | `tempo` | static / env var (`JOE_TEMPO_<LABEL>`) | runtime (`/test`) |
| Jaeger | `jaeger` | static / env var (`JOE_JAEGER_<LABEL>`) | runtime (`/test`) |
| Splunk | `splunk` | static / env var (`JOE_SPLUNK_<LABEL>`) | boot (restart) |
| Dynatrace | `dynatrace` | static / env var (`JOE_DYNATRACE_<LABEL>`) | boot (restart) |
| New Relic | `newrelic` | static / env var (`JOE_NEWRELIC_<LABEL>`) | boot (restart) |

### Alerting

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| Alertmanager | `alertmanager` | static / env var (`JOE_ALERTMANAGER_<LABEL>`) | runtime (`/test`) |
| PagerDuty | `pagerduty` | static / env var (`JOE_PAGERDUTY_<LABEL>`) | runtime (`/test`) |
| Grafana | `grafana` | static / env var (`JOE_GRAFANA_<LABEL>`) | runtime (`/test`) |

### GitOps and security

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| Argo CD | `argocd` | static / env var (`JOE_ARGOCD_<LABEL>`) | runtime (`/test`) |
| Falco | `falco` | static / env var (`JOE_FALCO_<LABEL>`) | runtime (`/test`) |

### Credential-less systems

These connect without any credential — there is nothing to arm. Register them, run the
connectivity test, and they function.

| System | Type | Credential mechanism | Activation |
| --- | --- | --- | --- |
| Terraform | `terraform` | none — reads local state | runtime (`/test`) |
| Envoy | `envoy` | none — unauthenticated admin API | runtime (`/test`) |
