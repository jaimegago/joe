---
title: Guides
weight: 70
description: Task-focused how-to.
---

# Guides

This section is the practical how-to for operating Joe's day-to-day surfaces. Each
page is goal-directed: it tells you the real entry point — a `joe` subcommand, an
HTTP path, or a UI surface — and the steps to get the job done against the running
binary. For the *why* behind any of these, the pages link back to
[Concepts](../concepts/); for exact configuration keys they link to
[Configuration](../configuration/).

## Pages

- [The web UI and human login](web-ui/) — log a human in over OIDC, the admin
  bootstrap, and what the post-login app actually looks like.
- [Register a Kubernetes component](register-kubernetes/) — bring a cluster under Joe's
  management through the UI: register it inert, promote it with a static-bearer or
  Entra-exchange reference, and confirm it is live.
- [Register a Git repository](register-git/) — register a repository by its clone URL and
  promote it with a token reference, or as public.
- [Register a GitHub, GitLab, or PagerDuty component](register-token-components/) — promote
  it with a reference to an API token and confirm it is live.
- [Register a Prometheus, Mimir, Loki, Tempo, Jaeger, Alertmanager, or Falco component](register-url-components/)
  — register a backend by its URL and promote it with a bearer-token reference, or connect
  it without one if it needs no token.
- [Register a Grafana, Argo CD, Splunk, Dynatrace, or New Relic component](register-url-token-components/)
  — register it with its URL or account ID and promote it with a reference to an API token.
- [Register a Terraform or Envoy component](register-terraform-envoy/) — the two
  credential-less types: register, assign a zone, and test, with no promotion step.
- [Chat sessions](chat-sessions/) — create a session, who can read it, and link one
  to an incident.
- [The incident regime](incidents/) — declare and resolve an incident, and what the
  regime changes while it is active.
- [Skills](skills/) — install, approve, and manage Agent Skills from the `joe skills`
  subcommand.
- [The MCP server](mcp/) — connect an editor or other MCP client and the tool surface
  Joe exposes. This is the page the [Components](../components/) section points to.
- [The Slack bot](slack/) — bring up `joe slack` as a Socket Mode daemon client.
- [The knowledge graph](knowledge-graph/) — query the graph and understand how it
  populates.
