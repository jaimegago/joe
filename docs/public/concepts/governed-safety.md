---
title: The governed-safety invariant
weight: 10
description: Why "Joe running implies Joe governed" is a structural guarantee.
---

# The governed-safety invariant

Most systems treat safety as something you configure: turn on auth, write the right
policy, remember to scope the credentials. The safety holds only as long as everyone
keeps doing the right thing. Joe is built so that its core guarantee does not depend
on anyone remembering anything.

> **Joe running implies Joe governed.**

If the daemon is up, it is authenticated, authorized, and write-gated. There is no
configuration that produces a running-but-ungoverned Joe, because the conditions for
"running" and the conditions for "governed" are the same conditions.

## Why it is structural, not disciplinary

Three properties make the invariant hold by construction rather than by convention.

**Boot refuses without identity.** The daemon will not start unless an identity
configuration is present — either a complete OIDC issuer for human logins, or at least
one service account for machine callers. There is no "auth disabled" mode and no flag
that removes the requirement. A misconfigured install fails to boot; it does not boot
into an open state.

**One governed path to every system.** Joe never reaches an adapter or the graph
directly. Both human-facing requests and Joe's own background loop go through a single
guarded accessor that evaluates the caller's principal against the requested action
and writes the decision to an append-only audit record. The same seam serves the HTTP
handlers and the in-process agent, so there is no privileged shortcut for Joe's own
reasoning.

**The single path cannot be bypassed.** That the guarded accessor is the *only* way to
reach an adapter is not a convention that a future change might quietly break — it is
enforced by a build-failing structural test. Code outside a narrow allowlist that
tries to resolve an adapter directly does not compile. The governance seam is therefore
load-bearing in the literal sense: removing it breaks the build.

## What the invariant does and does not promise

The invariant guarantees that every read and every mutation is attributed to an
authenticated principal and checked before it happens. It is the foundation the other
concepts build on: the [write floor](../observation-mode-and-the-write-floor/) decides
whether mutations are allowed at all, [principals and identity](../principals-and-identity/)
decide who is asking, and [RBAC, zones, and the read posture](../rbac-zones-and-read-posture/)
decide what each principal may reach.

It does not, by itself, promise that Joe will refuse a dangerous-but-authorized
action; that is what observation mode, the incident regime, and policy are for. The
invariant promises something narrower and more durable: there is no ungoverned way in.
