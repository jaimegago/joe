# Backlog — Datastore URI credential provider (deferred from A003)

Status: deferred from A003 (promotion-boundary work) to its own future design
Priority: later
session.

## Problem

The datastore component types — `postgresql`, `mysql`, `mongodb`,
`elasticsearch` — authenticate via **connection-string URIs that embed
credentials** (e.g. `mongodb://user:password@host:port/db`), not single bearer
tokens. The credential-provider seam (`internal/credential/provider.go`) has only
`StaticProvider` (`internal/credential/static.go`) and `KubeconfigExecProvider`
(`internal/credential/kubeconfig_exec.go`).

`redis` (`internal/adapters/datastore/redis/`) and `kafka`
(`internal/adapters/datastore/kafka/`) are in the datastore family but do
**not** share the URI shape — both use discrete credential fields. See the
carve-outs below.

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

## kafka — same family, discrete SASL fields, not URI-shaped (VERIFIED)

`kafka` (`internal/adapters/datastore/kafka/`) is a datastore type but does
**not** authenticate via a credential-bearing URI. Like redis, its config is
discrete fields — `brokers` (`[]string`), `tls_enabled`, `sasl_mechanism`,
`username`, `password` (`kafka/config.go:10-14`), with no URI field on the
struct at all. It connects by dialing a broker address
(`kafka.go:157`) and builds the admin client from `Addr: kafkago.TCP(brokers...)`
(`kafka.go:163-165`). The secrets are isolated `username` / `password` values, so
— as with redis — the token-shaped `StaticProvider` model is a *closer* fit than
the URI-shaped provider, and kafka may not need a URI-shaped provider at all;
this should be confirmed when kafka is wired.

kafka does **not** exhibit the URI leak: its dial error
(`kafka.go:159`) formats only a broker address (`cfg.Brokers[0]`) and its
connected `Status.Message` (`kafka.go:192`, format string `kafka.go:17`) formats
only `a.config.Brokers` — neither the `Username` nor `Password` field is ever
interpolated into an error or status string (a sweep of the adapter finds
`username`/`password`/`sasl` only in the `config.go` struct declarations). It is
recorded here so the datastore family is complete and to correct its earlier
mislisting among the URI-shaped stores.

## kafka — SASL parsed but never applied (VERIFIED)

kafka's config parses `sasl_mechanism`, `username`, and `password`
(`kafka/config.go:12-14`) into `a.config` (`kafka.go:154`), but those fields are
**never applied** when constructing the broker client or during the connectivity
dial:

- The connectivity dial (`kafka.go:157`) is a bare
  `kafkago.DialContext(ctx, "tcp", cfg.Brokers[0])` — no SASL mechanism, no TLS,
  no auth.
- The admin client (`kafka.go:163-165`) is built with `Addr` only
  (`kafkago.TCP(cfg.Brokers...)`) — no `Transport` and no `SASLMechanism`.

Consequently a component configured with SASL credentials **connects (or fails)
unauthenticated**, and no error is raised to signal that the configured
credentials were ignored.

Disposition (preserved verbatim from D-0026's documented-gaps clause): **security
finding, arguably fix-before-launch**. The tracking GitHub issue for this finding
is being deleted, so this backlog entry is now its sole home.

**Fix direction (requirement, not implementation):** the configured
`sasl_mechanism`, `username`, and `password` must be applied to **both** the
broker client transport and the connectivity dial, gated on the
credential-provider work this backlog item already covers.

## Promotion impact

`postgresql`, `mysql`, `mongodb`, and `elasticsearch` **cannot be
promoted under the credential-reference model** until a URI-shaped credential
provider (or a redaction-aware `StaticProvider` extension) exists. `redis` and
`kafka` are blocked only on a provider decision (likely the existing
`StaticProvider` token shape covers their discrete credential fields), not on URI
redaction — and kafka additionally needs its parsed SASL credentials actually
applied at connect (see the parse-but-never-apply finding above).

## Datastore secrets pass the credential-less-at-registration guard (VERIFIED)

The unwired datastore types — `mongodb`, `postgresql`, `mysql`, `redis`,
`elasticsearch`, `kafka` — accept **credential-embedding config fields at
registration** (a `uri` carrying `user:password`, a discrete `password`, an
`api_key`) because the registration guard does not know those field names.

`RejectCredentialFields` (`internal/componentgov/credentials.go:92`) is a
**name-based** rejection: it iterates `credentialBearingFields`
(`credentials.go:52`), which is single-sourced from
`credential.CredentialBearingFields()`. That set is derived by reflection over
the four credential provider structs only —
`discriminator`/`staticConfig`/`staticBearerConfig`/`entraExchangeConfig`
(`internal/credential/fields.go:42-79`) — yielding `credential_provider`,
`value`, `env_var`, `in_cluster`, `tenant_id`, `client_id`,
`client_secret_env_var`, `federated_token_file` (`audience` is explicitly
excluded as a descriptor, `fields.go:28-30`). **None of the datastore secret
field names** (`uri`, `password`, `api_key`) appear in any of those structs, so
the guard does not cover them and a datastore config carrying them registers
`201` and stores the secret in the component `config` blob.

The gap is reachable on **both** registration paths, which share this one guard:

- the admin HTTP create path — `RejectCredentialFields(req.Config)` at
  `internal/api/components.go:247`;
- the `register_component` LLM tool path — `RejectCredentialFields(configBytes)`
  at `internal/coreagent/agent.go:477`.

**`mongodb` requires `uri`**, so simply *rejecting* a URI at registration is not
an available fix today (a URI-less mongodb cannot be described at all). The fix
is therefore **gated on the URI-shaped credential provider this backlog item
covers**: once a URI-aware provider owns the secret (parsing routing out of the
credential, with the redaction boundary the sections above require), the
registration guard and the provider can share one authoritative view of which
datastore fields are credential-bearing, closing this registration hole and the
URI-leak sites together rather than piecemeal.

**Exposure is bounded, not nil.** A secret that lands in `config` this way is
stored **encrypted at rest** (the component `config` blob is encrypted by the
repository) and cannot be read back out through the component read model: every
component serialization path — list, get, **and the create echo** — projects
through `componentView`, which structurally omits the `Config` blob and every
credential locator (A002 read-model closure). So this is a **secret-at-rest
handling gap at the registration boundary**, not a read-side leak.

The tracking GitHub issue for the original D-0026 read-model finding **is being
deleted**; this backlog entry is now the **sole home** of the datastore
secret-into-`config` registration residual.

## Why deferred

A003 is the promotion-boundary work. Modeling a credential-bearing URI — and the
redaction boundary it implies — is a distinct design exercise that would
front-run its own decisions if attempted inside A003.

Reference: `docs/project/adr/D-0026-credential-provider-abstraction.md`.
