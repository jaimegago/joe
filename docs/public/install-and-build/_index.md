---
title: Install and Build
weight: 40
description: Build the joe binary from source, run it, and wire the identity it refuses to boot without.
---

# Install and Build

Joe is distributed as **source only**. There are no published release binaries,
install scripts, or package-manager artifacts today — you build the `joe` binary
yourself from this repository. A release pipeline is armed and publishes signed
archives and checksums to a GitHub Release when a version tag is pushed, but no
version has been tagged yet.

This page is the procedure: build the binary, run the daemon, and configure the
identity it requires. If you just want a guided first run from nothing to one answer,
follow the [Quickstart](../quickstart/) instead — it is the on-rails version of what
follows. For *why* identity is mandatory and what observation mode protects, see
[Concepts](../concepts/). For the full configuration surface, see
[Configuration](../configuration/).

## Prerequisites

- **Go 1.25 or newer** — to compile the binary.
- **Node.js and npm** — to build the web UI, which is compiled and embedded into the
  binary. `make build` runs `npm ci && npm run build` for you; without a working npm
  the build stops before it links the binary.
- **git** — to clone the source and to stamp build identity (`make build` reads the
  current commit and tag).

## Build from source

From the repository root:

```sh
make build
```

This produces a `./joe` binary in the repository root. `make build` first builds the
production web UI and stages it into the embed directory, then compiles `./cmd/joe`
with the UI embedded and build identity (version, commit, build time) injected into
the binary. The result is a single self-contained binary: server daemon plus all
subcommands, with the UI served from inside it.

A plain `go build ./...` also compiles, but it does **not** embed a freshly built UI
or inject build identity — such a binary reports the unset `dev` build defaults. Use
`make build` for anything you intend to run.

### Why nothing is published yet

The repository carries a GoReleaser configuration, and CI runs a snapshot build on
every change to prove the release path stays healthy. That configuration publishes a
GitHub Release automatically when a `v`-prefixed version tag is pushed — the pipeline
is armed, not disabled. No version has been tagged yet, so building from source is the
only supported way to obtain `joe` today.

## Run the daemon

A bare invocation starts the server — that is Joe's default behavior:

```sh
./joe
```

The daemon listens on `localhost:7777` by default (the HTTP API and the embedded web
UI). Prometheus metrics are served separately on port `9090`, not on the API port.

Joe reads its configuration file from, in order of precedence:

1. the `--config <path>` flag,
2. the `JOE_CONFIG` environment variable,
3. `~/.joe/config.yaml` (the default).

A **missing config file is not an error** — Joe boots on built-in defaults. What Joe
will *not* do is boot without an identity configuration, covered next.

The subcommands (`joe mcp`, `joe slack`, and the rest) ride alongside the server in the
same binary but are dispatched ahead of it; `joe mcp` and `joe slack` are clients of a
running daemon, not part of server boot.

## Identity is mandatory

Joe **refuses to start** without a usable identity configuration. There is no
auth-disabled or anonymous runtime mode to fall into — this refusal is what makes
"Joe running implies Joe governed" structural rather than a matter of discipline (see
[the governed-safety invariant](../concepts/governed-safety/) and
[principals and identity](../concepts/principals-and-identity/)).

A usable identity configuration means **at least one** of the following is present:

- one or more **service accounts** (non-human principals), or
- a complete **OIDC issuer** (human principals).

Configure at least one before you run, or boot will exit with a refusal.

### Non-human principals: service-account bearer keys

Joe's shipped machine clients are the MCP server and the Slack bot; they — along with
any other external caller an operator mints a service-account key for, such as a CI job,
a script, or `curl` — authenticate with a service-account bearer key presented as
`Authorization: Bearer <key>`. Joe
resolves the key to a `svc:<name>` principal. Define service accounts in config:

```yaml
server:
  service_accounts:
    - name: ci
      key: a-long-random-secret
```

A request bearing that key authenticates as principal `svc:ci`. Duplicate or empty
keys are a boot failure rather than a silent ambiguity.

As a shorthand, the `JOE_API_KEY` environment variable sets the key of a reserved
`server` service account (principal `svc:server`), creating it if absent. Setting
`JOE_API_KEY` alone satisfies the identity requirement, which is the smallest possible
identity configuration — the [Quickstart](../quickstart/) uses exactly this.

### Human principals: OIDC

People sign in through a single configured OIDC issuer using a real authorization-code
flow with PKCE; a verified login mints a `user:` principal. Configure the issuer under
`auth.oidc`:

```yaml
auth:
  oidc:
    issuer: https://your-idp.example.com
    client_id: joe
    client_secret: your-client-secret
    redirect_url: https://joe.example.com/api/v1/auth/callback
```

The login, callback, and logout endpoints are registered only when an issuer is
configured. OIDC discovery is lazy, so an identity-provider outage at startup is not a
boot failure — only new logins fail until it recovers.

### Admin bootstrap

The very first administrator is bootstrapped through OIDC. Set `auth.admin_email` to a
verified email address; when that person completes an OIDC login, Joe grants them admin
once (idempotent and audited):

```yaml
auth:
  admin_email: you@example.com
```

This bootstrap is **OIDC-only**: a service-account-only install has no self-escalation
path, so if you need the admin REST surface (zones, read posture, promotion), configure
OIDC and `admin_email`. Once one admin exists, further admins are granted through the
admin API by an existing admin.

## Observation mode and the write floor

Joe **boots with the write floor up by default**: the floor comes up read-only when
`JOE_MODE` is unset and, explicitly, with `JOE_MODE=observation`. Every attempt to
mutate a managed system is denied before any other gate is consulted, and the floor is
resolved once at boot and cannot be lowered while the process runs. This is the shipped
read-only posture — Joe can read and reason about your infrastructure but cannot change
it. A governed full-capabilities mode is forthcoming: `JOE_MODE=full` is refused at boot
pending implementation, and an unrecognized value is refused fail-closed. When full mode
lands, moving to a writable posture will be a deliberate restart with different boot
inputs, never a live transition. See
[observation mode and the write floor](../concepts/observation-mode-and-the-write-floor/)
for the full model.

## Where to go next

- A guided, guaranteed-success first run → [Quickstart](../quickstart/)
- Every configuration key, default, and environment variable → [Configuration](../configuration/)
- Connecting Joe to your systems → [Components](../components/)
- Running, observing, and recovering Joe in production → [Operations](../operations/)
- Why Joe is governed by construction → [Concepts](../concepts/)
