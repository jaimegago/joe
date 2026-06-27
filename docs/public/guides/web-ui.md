---
title: The web UI and human login
weight: 10
description: Log a human in over OIDC, bootstrap the first admin, and read the app shape.
---

# The web UI and human login

The [Quickstart](../quickstart/) deliberately authenticated with a service-account
bearer key and never logged a *human* in. This page covers the human path: people sign
in to the web UI through **OIDC**, so the UI requires an OIDC provider to be configured
before anyone can log in.

For the exact configuration keys named below, see
[Configuration](../configuration/); this page stays at the how-to level.

## Prerequisite: configure OIDC

Human login is only available when the daemon has a complete OIDC configuration — the
`auth.oidc` block needs an `issuer`, a `client_id`, and a `redirect_url`. If any of the
three is missing, the daemon does **not** register the login routes, and the only way in
is a service-account bearer key (machine access, not a human session).

Register Joe as an application with your identity provider, set the three values, and
point `redirect_url` at the daemon's `/api/v1/auth/callback`. The login page reads a
public capability flag and only shows the **Sign in** button when OIDC is actually
configured.

## The login flow

1. Open the web UI in a browser and click **Sign in**.
2. The browser is redirected to `GET /api/v1/auth/login`, which starts the OIDC flow
   (state, nonce, and PKCE) and forwards you to your identity provider.
3. After you authenticate with the provider, it redirects back to
   `GET /api/v1/auth/callback`. Joe verifies the ID token, requires a verified email,
   and mints an HttpOnly session cookie.
4. You land in the app, signed in for the duration of the session (its lifetime is the
   configured `auth.session_ttl`).

Sign out with the **Sign out** control, which calls `POST /api/v1/auth/logout` and
revokes the server-side session immediately.

## Bootstrap the first admin

There is no separate "create admin" command. Set `auth.admin_email` to the email of the
person who should hold admin rights. The first time a human logs in whose verified OIDC
email matches that value, Joe grants them admin automatically. The grant is idempotent —
logging in again changes nothing — and it is audited the first time it escalates a
principal.

Everyone else logs in as an ordinary operator until an existing admin grants them more.

## What the app looks like

The post-login landing surface is **Chat** — opening the UI root redirects you straight
to `/chat`. There is no dashboard landing page; do not look for one.

The navigation is flat. Every signed-in operator sees the same top-level entries:

- **Chat** — the default surface; talk to Joe and drive tasks.
- **Sessions** — browse chat sessions (see [Chat sessions](chat-sessions/)).
- **Components** — the managed systems Joe knows about.

Admins additionally see a single **Admin** subgroup, revealed only when the signed-in
principal's `is_admin` flag is true (Joe reports it from `GET /api/v1/me`). It collects
the governance surfaces — Zones, Policies, Autonomous Reads, Skills, Admins, Users,
Credentials, and LLM Settings. A non-admin who navigates directly to an admin route is
redirected away; the subgroup is hidden, not merely disabled.

## Where to go next

- The identity model behind these principals → [Principals and identity](../concepts/principals-and-identity/)
- The full OIDC and admin configuration keys → [Configuration](../configuration/)
- Who may read which components in the UI → [RBAC, zones, and read posture](../concepts/rbac-zones-and-read-posture/)
