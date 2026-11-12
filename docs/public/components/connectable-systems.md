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
  needs at registration. `—` means none; a field in parentheses is optional. These are the
  only fields registration accepts for the type: an undeclared field — an inline `token`,
  `api_key` or `webhook_secret` among them — is refused, and so is a missing required one.
- The **web UI** column tells you whether the whole flow can be done from the web UI. The
  UI's Register dialog renders each type's routing fields and marks the required ones, so
  every type below registers there as well as through the API — see [Components](../).
- A component registered before Joe enforced these fields, without its required locator,
  cannot be given one later: delete it, register it again with the locator, and re-assign
  its security zone, since the delete removes the zone assignment.

### Kubernetes

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Kubernetes | `kubernetes` | `auth_method: static-bearer` — `api_server` + `ca_data` + a bearer token via `env_var` or `in_cluster: true`; **or** `auth_method: entra-exchange` (AKS) — `api_server` + `ca_data` + `tenant_id` + `client_id` + `audience` + `client_secret_env_var` | — (the cluster coordinates are supplied at promotion) | yes — [guide](../../guides/register-kubernetes/) |

### Source control and code review

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Git | `git` | static / env var (`JOE_GIT_<LABEL>`), **or** explicitly no credential for a public repository | `url` (`branch`, `provider_component_id`) | yes — [guide](../../guides/register-git/) |
| GitHub | `github` | static / env var (`JOE_GITHUB_<LABEL>`) | — (`base_url`, for GitHub Enterprise Server) | yes — [guide](../../guides/register-token-components/) |
| GitLab | `gitlab` | static / env var (`JOE_GITLAB_<LABEL>`) | — (`base_url`, for a self-managed instance) | yes — [guide](../../guides/register-token-components/) |

### Metrics, logs, and traces

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Prometheus | `prometheus` | static / env var (`JOE_PROMETHEUS_<LABEL>`) | `url` (`org_id`) | yes |
| Mimir | `mimir` | static / env var (`JOE_MIMIR_<LABEL>`) | `url` (`org_id`) | yes |
| Loki | `loki` | static / env var (`JOE_LOKI_<LABEL>`) | `url` (`org_id`) | yes |
| Tempo | `tempo` | static / env var (`JOE_TEMPO_<LABEL>`) | `url` (`org_id`) | yes |
| Jaeger | `jaeger` | static / env var (`JOE_JAEGER_<LABEL>`) | `url` | yes |
| Splunk | `splunk` | static / env var (`JOE_SPLUNK_<LABEL>`) | `url` (`index`) | yes |
| Dynatrace | `dynatrace` | static / env var (`JOE_DYNATRACE_<LABEL>`) | `url` | yes |
| New Relic | `newrelic` | static / env var (`JOE_NEWRELIC_<LABEL>`) | `account_id`, an integer (`region`) | yes |

### Alerting

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Alertmanager | `alertmanager` | static / env var (`JOE_ALERTMANAGER_<LABEL>`) | `url` | yes |
| PagerDuty | `pagerduty` | static / env var (`JOE_PAGERDUTY_<LABEL>`) | — (`api_url`, for the EU service region) | yes — [guide](../../guides/register-token-components/) |
| Grafana | `grafana` | static / env var (`JOE_GRAFANA_<LABEL>`) | `url` | yes |

### GitOps and security

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Argo CD | `argocd` | static / env var (`JOE_ARGOCD_<LABEL>`) | `url` (`insecure_tls`) | yes |
| Falco | `falco` | static / env var (`JOE_FALCO_<LABEL>`) | `url` | yes |

### Credential-less systems

These connect without any credential — there is nothing to arm. Register them with their
routing config, run the connectivity test, and they function.

| System | Type | Credential mechanism | Routing config | Web UI |
| --- | --- | --- | --- | --- |
| Terraform | `terraform` | none — reads local state | `state_path` | yes |
| Envoy | `envoy` | none — unauthenticated admin API | `url` (`timeout_ms`) | yes |
