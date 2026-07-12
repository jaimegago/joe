---
title: Principals and identity
weight: 30
description: How humans and machines are named and authenticated.
---

# Principals and identity

Every action in Joe is attributed to a **principal** — a typed, prefixed name that
says who is asking. Principals are how the [governed-safety
invariant](../governed-safety/) keeps its promise that nothing happens anonymously: a
request without a resolvable principal does not get past the edge.

## Humans authenticate via OIDC

People sign in through a single configured OIDC issuer using a real
authorization-code flow with PKCE. On success, Joe mints a principal carrying the
reserved **`user:`** prefix, derived from the verified email — a `user:` principal.
Joe verifies the identity at the edge once and then carries that principal by internal
context; it does not re-authenticate on every internal hop.

## Machines authenticate with service-account keys

Joe's shipped machine clients are the MCP server and the Slack bot; they — along with
any other external caller an operator mints a service-account key for, such as a CI
integration or a script — present a service-account bearer key. Joe resolves the key to a principal carrying the
reserved **`svc:`** prefix — a `svc:<name>` principal. Service accounts are defined in
configuration; a duplicate or empty key is a boot failure rather than a silent
ambiguity.

Joe's own autonomous background loop is itself a service principal, `svc:agent:core`.
Treating the agent as a named principal rather than a privileged exception is what lets
its reads flow through the same governed path as everyone else's.

## Reserved prefixes and the impersonation guard

The `user:` and `svc:` prefixes are **reserved**: they denote the *kind* of a
principal, and Joe will not let a caller forge one. If an identity provider hands back
an email that itself looks like a reserved-prefix principal, Joe rejects it rather than
minting a principal that could impersonate another kind. The prefix is a type, and the
type is trustworthy because it can only be assigned by Joe, never supplied by the
caller.

## Why both, and why it matters downstream

Two authentication paths exist because the two kinds of caller are genuinely different:
humans get an interactive login with a session; machines get a long-lived key. They
converge on the same thing — a typed principal — so that everything downstream
([RBAC and zones](../rbac-zones-and-read-posture/), the audit record,
[chat-session ownership](../chat-sessions/)) can reason about one uniform notion of "who"
without caring how that "who" arrived.

For how to configure an OIDC issuer or define service accounts, see
[Configuration](../../configuration/); to bring a Joe up with identity wired, see
[Install and Build](../../install-and-build/).
