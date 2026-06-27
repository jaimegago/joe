# Backlog — Datastore URI credential provider (deferred from A003)

Status: deferred from A003 (promotion-boundary work) to its own future design
session.

## Problem

The datastore component types — `postgresql`, `mysql`, `mongodb`, `kafka`,
`elasticsearch` — authenticate via **connection-string URIs that embed
credentials** (e.g. `mongodb://user:password@host:port/db`), not single bearer
tokens. The credential-provider seam (`internal/credential/provider.go`) has only
`StaticProvider` (`internal/credential/static.go`) and `KubeconfigExecProvider`
(`internal/credential/kubeconfig_exec.go`).

`redis` (`internal/adapters/datastore/redis/`) is in the datastore family but
does **not** share the URI shape — see the carve-out below.

## Why the existing providers do not fit

`StaticProvider`'s `staticConfig` (`internal/credential/static.go`) models a
credential as an inline `value` or an `env_var` name and `Resolve` returns one
opaque string. A datastore URI is structurally different: the secret
(user:password) is **embedded inside** a larger string that also carries
non-secret routing (scheme, host, port, database, query params). Treating the
whole URI as the static value technically stores it, but it gives the seam no way
to know the string is credential-bearing — so the URI flows verbatim into any
code path that formats the config for humans, with no redaction boundary. A
datastore-shaped provider (or an explicit extension to `StaticProvider` that
marks a URI as the secret and knows how to redact it) is needed before these
types can be wired and promoted.

## Concrete leak this must fix (VERIFIED)

`internal/adapters/datastore/mongodb/mongodb.go` interpolates the raw
credential-bearing URI into two human/error-facing paths:

- `mongodb.go:106` — `return fmt.Errorf("ping MongoDB at %s: %w", cfg.URI, err)`:
  a failed ping surfaces the full `cfg.URI`, including any embedded
  `user:password`, in the returned error.
- `mongodb.go:135` — `Message: fmt.Sprintf(statusConnectedFmt, a.config.URI)`:
  the connected `Status.Message` embeds `a.config.URI` verbatim.

A URI embedding credentials therefore leaks through both the error path and the
status path. A credential provider that owns the URI must redact the secret
component before it reaches error strings or status messages; these two call
sites are the concrete fixes that gate promotion. (The other datastores should be
swept for the same pattern when the provider is designed — only mongodb's two
sites were verified here.)

## redis — same family, different credential shape (VERIFIED)

`redis` (`internal/adapters/datastore/redis/`) is a datastore type but does
**not** authenticate via a credential-bearing URI. Its config is discrete fields
— `host`, `port`, `password`, `db`, `tls_enabled` (`redis/config.go:10-14`) — and
it connects with `Addr: host:port` plus a separate `Password` field
(`redis/redis.go:128-129`). The secret is an isolated `password` value, so the
token-shaped `StaticProvider` model is a *closer* fit for redis than for the URI
stores (its `value` / `env_var` could carry the password), and redis may not need
a URI-shaped provider at all — this should be confirmed when redis is wired.

Notably, redis does **not** exhibit the URI leak: its ping error
(`redis/redis.go:141`) and connected `Status.Message` (`redis/redis.go:170`)
format only `host:port`, never the `Password` field. It is listed here only so
the datastore family is recorded completely, not because it shares the
URI-redaction problem.

## Promotion impact

`postgresql`, `mysql`, `mongodb`, `kafka`, and `elasticsearch` **cannot be
promoted under the credential-reference model** until a URI-shaped credential
provider (or a redaction-aware `StaticProvider` extension) exists. `redis` is
blocked only on a provider decision (likely the existing `StaticProvider` token
shape covers its discrete `password`), not on URI redaction.

## Why deferred

A003 is the promotion-boundary work. Modeling a credential-bearing URI — and the
redaction boundary it implies — is a distinct design exercise that would
front-run its own decisions if attempted inside A003.

Reference: `docs/project/adr/D-0026-credential-provider-abstraction.md`.
