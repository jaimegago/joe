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
  it with a reference to an API token and confirm it is live. Types that need an endpoint
  URL are registered through the API; see [Components](../components/).
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
