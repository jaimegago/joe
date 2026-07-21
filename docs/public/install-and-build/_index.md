---
title: Install and Build
weight: 40
description: Download a released joe binary or build it from source, run it, and wire the identity it refuses to boot without.
---

# Install and Build

Joe is available two ways: as **published release binaries** for the supported platform
set, or **built from source**. Both produce the same single self-contained artifact —
the server daemon, every subcommand, and the web UI compiled into one `joe` binary.

A published release carries the built archives and a `checksums.txt` file, and nothing
else. There is no signing, no install script, and no package-manager artifact — no
Homebrew tap, no Scoop bucket, no distribution package. Downloading an archive and
verifying it against the checksums file by hand is the whole of the install path.

This page is the procedure: obtain the binary, run the daemon, and configure the
identity it requires. If you just want a guided first run from nothing to one answer,
follow the [Quickstart](../quickstart/) instead — it is the on-rails version of what
follows. For *why* identity is mandatory and what observation mode protects, see
[Concepts](../concepts/). For the full configuration surface, see
[Configuration](../configuration/).

## Download a released binary

This is the shortest path to a running `joe`, and it needs neither a Go toolchain nor
Node.js — only a shell and a SHA-256 utility, both of which your system already has.

**1. Fetch the archive for your platform.** Open the repository's **GitHub Releases**
page and download the archive matching your operating system and CPU architecture from
the release's asset list, along with the `checksums.txt` file published beside it. The
platforms a release covers are exactly the `builds.goos` and `builds.goarch` matrix
declared in `.goreleaser.yaml` in the repository root — read that file rather than
trusting a list restated here, and let the Releases page's own asset list be the
authority on what a given release actually shipped.

**2. Verify the download against the published checksums.** Run this from the directory
holding both the archive and `checksums.txt`. On Linux:

```sh
sha256sum --ignore-missing --check checksums.txt
```

On macOS:

```sh
shasum --algorithm 256 --ignore-missing --check checksums.txt
```

`--ignore-missing` is what lets you verify the one archive you downloaded against a
checksums file that lists every published asset. Each verified file prints `OK`. If any
file prints `FAILED`, stop — do not extract or run it.

**3. Extract and run.** The archives are gzipped tarballs:

```sh
tar -xzf <the archive you downloaded>
./joe --help
```

A downloaded release binary reports **real injected build identity** — its version,
commit, and build time are stamped in at release time, so `GET /api/v1/version` and the
`joe_build_info` metric identify precisely which release is running. This is the
practical difference from a plain `go build`, described below.

## Build from source

Building from source is a first-class path, not a fallback. Reach for it when you are
contributing to Joe, when you want to run an untagged commit from `main`, or when your
platform is not in the release matrix.

### Prerequisites for building

These apply to the build path only — the download path above needs none of them.

- **Go 1.25 or newer** — to compile the binary.
- **Node.js and npm** — to build the web UI, which is compiled and embedded into the
  binary. `make build` runs `npm ci && npm run build` for you; without a working npm
  the build stops before it links the binary.
- **git** — to clone the source and to stamp build identity (`make build` reads the
  current commit and tag).

### Building

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

A **human** administrator is bootstrapped through OIDC. Set `auth.admin_email` to a
verified email address; when that person completes an OIDC login, Joe grants them admin
once (idempotent and audited):

```yaml
auth:
  admin_email: you@example.com
```

A service-account-only install has no OIDC callback, so that bootstrap cannot run there —
and it has no **self-escalation** path either: no running principal can grant itself
admin. Its first admin is created **offline** instead, by an operator with access to
Joe's database and config:

```bash
joe admin bootstrap svc:joe-admin

# If you start the daemon with an explicit config file, pass the same one here —
# it names both the service accounts and the database the grant is written to.
joe admin bootstrap svc:joe-admin --config /etc/joe/config.yaml
```

That command grants admin to a service account named in `server.service_accounts`, on a
database that has no admin yet. It refuses human identities — without an identity
provider they could never authenticate — and it is refused the moment any admin exists,
with no override. Name a **dedicated** administration account for it rather than the
shared general-purpose key. `--config` works exactly as it does on the daemon; without
it, the default `~/.joe/config.yaml` is used. Once one admin exists, further admins are granted through the
admin API by an existing admin; see [Operations](../operations/) for the break-glass
context.

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
