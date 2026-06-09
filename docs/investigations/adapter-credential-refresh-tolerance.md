# Adapter Credential Refresh Tolerance — Investigation

**Date:** 2026-06-09
**Scope:** Read-only. Re-derived against the live tree. No code changes.
**Question (per adapter):** Can its credential become *refreshable* — i.e. can the
adapter hold a credential **provider handle** it calls for a *current* value at
call time (so the value can rotate underneath it: workload identity, short-lived
federated creds) — **without rewriting the adapter**? Or does its construction
bake the credential value in permanently?

> The prior doc `credential-handling-current-state.md` is treated as unverified
> design intent. Every claim below is re-derived with a `file:line` citation.
> Where this investigation contradicts the prior doc, it says so explicitly.

---

## TL;DR / Core findings

- **The contract is uniform and shallow.** Every adapter implements
  `Connect(ctx, store.Component) error` (`internal/adapters/adapter.go:13`) and
  parses its credential out of the freeform `Component.Config` JSON. This part of
  the prior doc holds up exactly.
- **But adapters split on the central axis** (struct-field-client vs.
  resolve-per-call), and that split — *not* the uniform `Connect` contract — is
  what decides refresh tolerance:
  - **Resolve-per-call (the majority).** Every HTTP-backed adapter caches a
    *credential-free* `*http.Client` and re-reads the secret from `a.config`
    **on every request**, applying it as a header at the call site
    (e.g. prometheus `addHeaders` `internal/adapters/observability/prometheus/prometheus.go:366-373`).
    These bake nothing — swapping the per-call read for a provider call is a small,
    localized change. **GREEN.**
  - **Struct-field-client with a *source-shaped* SDK.** Kubernetes (client-go) and
    AWS/ECR (aws-sdk-go-v2) cache a built client, but the SDK client holds a
    **credential source**, not a value, and refreshes natively per request. These
    are **already refresh-capable today** for their workload-identity auth modes
    (in-cluster / exec / assume-role / web-identity). **GREEN.**
  - **Struct-field-client with a *value-baked* SDK.** The connection-pool
    datastores (postgres, mysql, mongodb) and kafka capture the credential into a
    pool/client at `Connect`. Refresh needs reconstructing the pool or wiring an
    SDK callback the adapter doesn't currently use. **YELLOW.**
- **No adapter is RED.** The uniform `Connect`/`Config` pattern keeps coupling
  shallow everywhere; even the hardest cases (DB pools) are reconstruct-per-call,
  not architectural rewrites.
- **Contradiction with the prior doc's framing.** The prior doc says the credential
  is "**bound into the adapter at `Connect` time** … long BEFORE … the accessor
  decision" and that "the credential is *near* (inside the resolved adapter) but
  *opaque*." That is true as a *storage* statement but misleading as a *refresh*
  statement: for the HTTP adapters **no credential is bound into any client at
  all** — only a config struct is stored, and the live value is re-read per call;
  for K8s/AWS the "bound" object is a *provider*, not a captured value. The eager
  binding the prior doc emphasizes is real only for the YELLOW pool adapters.

---

## 1. The Connect contract

**Interface** — `internal/adapters/adapter.go:10-20`:

```go
type Adapter interface {
    Connect(ctx context.Context, source store.Component) error  // :13
    Disconnect() error                                          // :16
    Status() Status                                             // :19
}
```

`Connect` returns only `error`. The credential arrives inside
`source store.Component` — specifically the freeform `Component.Config`
(`json.RawMessage`) blob, which each adapter unmarshals with its own package-local
`ParseConfig`. There is **no** method on the interface that exposes the credential
or the live client; `Adapter` exposes only Connect/Disconnect/Status.

**Central axis — does `Connect` store a live client, or just config?** Two classes:

| Class | What `Connect` stores | Adapters |
|---|---|---|
| **Resolve-per-call** | A credential-*free* `*http.Client` (or none) + the parsed `Config` struct; the secret is re-read from `a.config` and applied at each request | prometheus, loki, jaeger, tempo, splunk, datadog, newrelic, dynatrace, alertmanager, grafana, pagerduty, github, gitlab, argocd, falco, elasticsearch, oci, artifactory (+ envoy/terraform which have no credential) |
| **Struct-field-client** | A built backend SDK client / connection pool cached as a struct field | k8s, nginx, helm (client-go); aws, ecr (aws-sdk-go-v2); postgres, mysql (`*sql.DB`); redis (`*redis.Client`); mongodb (`*mongo.Client`); kafka (`*kafka.Client`); git (`*gogit.Repository`) |

The struct-field-client class subdivides again by whether the SDK client holds a
credential **source** (refreshes itself) or a **value** (baked) — see §2/§3.

---

## 2. Per-adapter client construction

For each adapter: the construction call, and whether the SDK constructor accepts
**(a)** a captured credential VALUE, **(b)** a credential SOURCE/PROVIDER, or
**(c)** unclear.

### Source-shaped SDK clients (b) — refresh native

- **Kubernetes** — `internal/adapters/k8s/k8s.go:69-79`:
  `dynamic.NewForConfig(restConfig)` + `kubernetes.NewForConfig(restConfig)`, where
  `restConfig *rest.Config` is built in `buildRESTConfig` (`k8s.go:112-132`).
  `rest.Config` is **source-shaped**: for `rest.InClusterConfig()` (`k8s.go:114`)
  the client-go transport re-reads the projected service-account token file per
  request; for an exec/auth-provider kubeconfig the transport invokes the plugin
  on expiry. SDK: `k8s.io/client-go v0.35.2`. **(b)** for in-cluster/exec;
  **(a)** only if a static token is embedded directly in the kubeconfig.
- **nginx** — `internal/adapters/networking/nginx/nginx.go:157`
  (`kubernetes.NewForConfig(restCfg)`, built **lazily** in `initDoer`
  `nginx.go:145-168` from `KubeconfigPath`). Same client-go semantics. **(b).**
- **helm** — `internal/adapters/packaging/helm/helm.go:149`
  (`kubernetes.NewForConfig(restConfig)`, built **lazily** in `initLister`
  `helm.go:139-153`). Same. **(b).**
- **AWS** — `internal/adapters/aws/aws.go:213-215`:
  `ec2.NewFromConfig(awsConfig)`, `eks.NewFromConfig(...)`, `rds.NewFromConfig(...)`,
  where `awsConfig aws.Config` is built in `buildAWSConfig` (`aws.go:260-290`).
  `aws.Config.Credentials` is an `aws.CredentialsProvider` (**a source**):
  `config.LoadDefaultConfig` (`aws.go:262`) wraps the resolved chain in an
  `aws.CredentialsCache` that calls `Retrieve` and refreshes on expiry; the
  assume-role path `stscreds.NewAssumeRoleProvider` (`aws.go:286`) refreshes STS
  creds. Only the static path `credentials.NewStaticCredentialsProvider(cfg.AccessKey,
  cfg.SecretKey, "")` (`aws.go:280`) is a fixed value. SDK:
  `aws-sdk-go-v2 v1.41.1` / `config v1.27.27` / `credentials v1.17.27`. **(b)**
  for chain/profile/assume-role; **(a)** only for the static access-key path.
- **ECR** — `internal/adapters/registry/ecr/ecr.go:111`
  (`awsecr.NewFromConfig(awsCfg)`, `awsCfg` from `buildAWSConfig` `ecr.go:252-280`).
  Same provider-chain semantics as AWS. SDK: `service/ecr v1.55.2`. **(b).**

### Per-call HTTP auth (no SDK client; secret applied per request) — refresh trivial

All of these cache a plain `*http.Client` (created in `New()` or, for TLS reasons,
in `Connect`) that carries **no credential**, and set an auth header from
`a.config` at every call. The "construction from the credential" is therefore a
single header assignment, repeated per request:

| Adapter | HTTP client created | Auth applied per-request (file:line) | Header / scheme |
|---|---|---|---|
| prometheus | `New()` `prometheus.go:85` | `prometheus.go:366-373` | `Authorization: Bearer <APIKey>` + `X-Scope-OrgID` |
| loki | `New()` `loki.go:71` | `loki.go:311-316` | Bearer + OrgID |
| jaeger | `New()` `jaeger.go:70` | `jaeger.go:316-318` | Bearer |
| tempo | `New()` `tempo.go:78` | `tempo.go:342-347` | Bearer + OrgID |
| splunk | `New()` `splunk.go:65` | `splunk.go:107` (Connect) / `splunk.go:185` (op) | `Authorization: Bearer <Token>` |
| datadog | `New()` `datadog.go:94` | `datadog.go:337-340` | `DD-API-KEY` / `DD-APPLICATION-KEY` |
| newrelic | `New()` `newrelic.go:67` | `newrelic.go:212` | `Api-Key` |
| dynatrace | `New()` `dynatrace.go:90` | `dynatrace.go:340` | `Authorization: Api-Token <Token>` |
| alertmanager | `New()` `alertmanager.go:72` | `alertmanager.go:234-236` | Bearer |
| grafana | `New()` `grafana.go:98` | `grafana.go:374-376` | Bearer |
| pagerduty | `New()` `pagerduty.go:76` | `pagerduty.go:285` | `Authorization: Token token=<APIKey>` |
| falco | `New()` `falco.go:76` | `falco.go:293` | Bearer |
| elasticsearch | `New()` `elasticsearch.go` (`&http.Client{}`) | `elasticsearch.go:246-249` | `ApiKey` or BasicAuth |
| oci | `Connect` `oci.go` (`&http.Client{Transport…}`) | `oci.go:308-314` (`addAuthHeader`) | `Authorization: Basic <creds>` |
| artifactory | `Connect` | `artifactory.go:275` | `X-JFrog-Art-Api: <APIKey>` |
| github | `New()` `github/adapter.go:55` | `adapter.go:158,254,288` | `Authorization: Bearer <Token>` |
| gitlab | `New()` `gitlab/adapter.go:53` | `gitlab/adapter.go:225,259` | `PRIVATE-TOKEN: <Token>` |
| argocd | `Connect` `argocd.go:132` (`&http.Client{Transport}` for TLS) | `argocd.go:324` | `Authorization: Bearer <Token>` |

All **(a)** in a literal sense (the value is a `string` field), but because it is
re-read at every call from `a.config`, refreshing it means changing **only the
read** — see §4. The `*http.Client` itself bakes nothing.

### Value-baked SDK clients / pools (a) — refresh needs rebuild or callback

- **postgres** — `internal/adapters/datastore/postgres/postgres.go:149`
  (`sql.Open("pgx", dsn)`), where `dsn` embeds `cfg.Password` (`postgres.go:146-147`).
  `database/sql` captures the DSN into the pooled `*sql.DB` (`a.db` `postgres.go:160`).
  SDK: `jackc/pgx/v5 v5.9.2` via `database/sql`. **(a)** — the password is fixed in
  the DSN at open. (pgx natively supports dynamic creds via `pgxpool`
  `BeforeConnect`, but the adapter uses `database/sql`, which does not.)
- **mysql** — `internal/adapters/datastore/mysql/mysql.go:143`
  (`sql.Open("mysql", dsn)`), DSN embeds `cfg.Password` (`mysql.go:140-141`), cached
  as `a.db` (`mysql.go:154`). SDK: `go-sql-driver/mysql v1.9.3`. **(a).**
- **mongodb** — `internal/adapters/datastore/mongodb/mongodb.go:97-98`
  (`mongo.Connect(ctx, options.Client().ApplyURI(cfg.URI))`), cached as `a.runner`
  wrapping `*mongo.Client` (`mongodb.go:103,109`). The credential is embedded in the
  connection-string URI. SDK: `go.mongodb.org/mongo-driver v1.17.9`. **(a)** — though
  the driver *does* support a refresh source (`options.Credential` with an
  `OIDCMachineCallback` for `MONGODB-OIDC`), which the adapter does not wire.
- **redis** — `internal/adapters/datastore/redis/redis.go:136`
  (`goredis.NewClient(opts)`), `opts.Password = cfg.Password` (`redis.go:129`), cached
  as `a.client` (`redis.go:144`). SDK: `redis/go-redis/v9 v9.18.0`. **(a) as written**,
  but the SDK is **source-capable**: `redis.Options.CredentialsProviderContext
  func(ctx) (user, pass, error)` is called per new connection. So the value is baked
  only because the adapter sets `Password` instead of the provider hook — a small
  change makes it **(b)**. (Caveat: like any pool, already-open sockets keep their
  old auth; only new connections pick up rotated creds.)
- **kafka** — `internal/adapters/datastore/kafka/kafka.go:163-166`
  (`&kafkago.Client{Addr: kafkago.TCP(cfg.Brokers...)}`, cached as `a.admin`).
  **No SASL/auth is wired at all** — `cfg.Username`/`Password`/`SASLMechanism` are
  parsed but never applied; the client uses the default transport. SDK:
  `segmentio/kafka-go v0.4.50` (its `Transport.SASL` accepts a `sasl.Mechanism`,
  which can be dynamic, but nothing is set). **(c)/(a)** — no credential path exists
  today; adding one would build a SASL transport.
- **git** — `internal/adapters/git/git.go:79` (`buildAuth(cfg)` →
  `transport.AuthMethod`), passed into `gogit.CloneOptions{Auth: auth}` (`git.go:92-94`)
  / `gogit.PullOptions{Auth: auth}` (`git.go:109`). The cached field is
  `a.repo *gogit.Repository` (`git.go:119`) — a **local** clone; the credential is
  used only for the Connect-time clone/pull, and subsequent read ops are local
  (no auth). SDK: `go-git/go-git/v5 v5.19.1`. **(b)-shaped**: go-git takes the
  `AuthMethod` *per network operation*, not baked into a persistent client — a
  fresh `AuthMethod` from a provider could be passed at each clone/pull.

### No credential

- **envoy** — `internal/adapters/networking/envoy/envoy.go:74-98`: builds an
  `*http.Client` against the Envoy admin API; `cfg` has `URL` only, no auth. **N/A.**
- **terraform** — `internal/adapters/iac/terraform/terraform.go:114`: `StatePath` is a
  local file path; no credential. **N/A.**

### Not-yet-implemented

- **azure** — `internal/adapters/azure/azure.go:90-110`: `Connect` parses `cfg` and
  sets `connected = true` (`azure.go:107-108`); **no Azure SDK client is built**. The
  credential fields (`ClientID`/`ClientSecret`/`TenantID`) are stored but unused. When
  implemented against `azidentity`, that SDK's `TokenCredential` is source-shaped by
  design, so it would land GREEN — but today it is a skeleton.

---

## 3. Refresh-native SDKs already in use (the two the prompt asks about)

**Kubernetes (client-go) — YES, already source-shaped.** The construction path is
`rest.Config` → `kubernetes.NewForConfig` / `dynamic.NewForConfig`
(`k8s.go:63-79`). `buildRESTConfig` (`k8s.go:112-132`) produces the `rest.Config`
two ways:
- `rest.InClusterConfig()` (`k8s.go:114`) — the resulting transport reads the
  projected SA token from disk and **re-reads it as it rotates**; the cached
  `clientset`/`dynClient` pick up new tokens without a rebuild.
- `clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)`
  (`k8s.go:131`) from a kubeconfig path — if that kubeconfig uses an `exec`
  credential plugin or auth-provider, client-go invokes it on token expiry.
What the code **actually does**: it caches the clients at `Connect` and never
rebuilds them. That is *correct and sufficient* for refresh, because refresh lives
in the transport, not the cached client object. The only non-refreshing sub-case is
a static bearer token written literally into the kubeconfig.

**AWS (aws-sdk-go-v2) — YES, the credential provider chain is used.**
`buildAWSConfig` (`aws.go:260-290`) calls `config.LoadDefaultConfig`
(`aws.go:262`, again with profile at `:269`), which resolves the default provider
chain (env → shared config → IMDS/IRSA web-identity → …) and wraps it in an
`aws.CredentialsCache`. The cache calls `Retrieve` and **refreshes on expiry**, so
the cached `ec2`/`eks`/`rds` clients (`aws.go:213-215`) sign each request with a
current credential. The assume-role override (`stscreds.NewAssumeRoleProvider`,
`aws.go:286`) is likewise a refreshing source. What the code **actually does**: it
builds the clients once at `Connect` and relies on the SDK's own per-request
credential retrieval — i.e. it is already a "provider handle" model internally.
The only baked sub-case is the explicit static-key override (`aws.go:279-281`),
which is a fixed value by its nature (nothing to refresh).

> Net: for K8s and AWS the refactor toward a "provider handle" is largely *already
> done by the SDK*. The only thing Joe captures eagerly is the *client object*, and
> for these two SDKs that object delegates credential acquisition to call time.

---

## 4. Per-call vs. cached-client invocation (and the move-to-provider cost)

Representative op traced per adapter, and which of three the move to a provider
handle requires: **(i)** change only what `Connect` stores; **(ii)** change every
operation to pull a current credential; **(iii)** a deeper rewrite.

- **HTTP per-call adapters** (prometheus, loki, jaeger, tempo, splunk, datadog,
  newrelic, dynatrace, alertmanager, grafana, pagerduty, falco, elasticsearch, oci,
  artifactory, github, gitlab, argocd). Trace: e.g. `Accessor` → typed op →
  `Adapter.Query` → builds `http.Request` → `addHeaders(req, a.config)` reads
  `a.config.APIKey` and `a.client.Do(req)` (prometheus `prometheus.go:168-189,366-373`).
  The cached `*http.Client` holds no secret; the secret is read fresh each call.
  **Move cost: (i)+(small ii)** — store a provider handle on the struct, and change
  the one `addHeaders`/header-set site to call it. Localized, one site per adapter.
- **Kubernetes / nginx / helm.** Trace: `K8sListResources` → `guard[...]`
  (`internal/access/k8s.go:12-13`) → `ad.ListResources` →
  `a.dynClient.Resource(...).List(...)` on the cached client. The client refreshes
  credentials internally. **Move cost: (i)** — pass a provider/`ExecConfig`-backed
  `rest.Config` at `Connect`; operations need no change. (Often **zero change**
  if already in-cluster/exec.)
- **AWS / ECR.** Trace: `ListEC2Instances` → cached `a.ec2Client.DescribeInstances`.
  Credentials retrieved per request by the SDK. **Move cost: (i)** — supply the
  desired `aws.CredentialsProvider` at `Connect`; operations unchanged. (Often
  **zero change** with the default chain / assume-role.)
- **redis.** Trace: `a.client.info(ctx, …)` on the cached pool. **Move cost: (i)** —
  set `Options.CredentialsProviderContext` instead of `Options.Password` at
  `Connect`; the SDK invokes it per new connection. Operations unchanged.
- **git.** Network only at `Connect` (clone/pull); read ops are local. **Move cost:
  (i)/(ii-light)** — build the `AuthMethod` from a provider at each clone/pull call.
- **postgres / mysql / mongodb.** Trace: cached pool (`a.db.scan` /
  `a.runner.runCommand`). The credential is fixed in the DSN/URI captured at
  `Connect`. **Move cost: (iii-light)** — `database/sql` / `ApplyURI` cannot rotate
  creds under a live pool; you must rebuild the `*sql.DB`/`*mongo.Client` on rotation
  (reconstruct-per-rotation, or switch to `pgxpool.BeforeConnect` / mongo OIDC
  callback). Not a per-operation change, but more than swapping a read.
- **kafka.** No auth wired today (`kafka.go:163-166`). **Move cost: (iii)** — build a
  SASL transport first, then make its mechanism dynamic.

---

## 5. Registry and lifecycle

**Where connected adapters live.** `adapters.Registry` — a
`map[string]Adapter` keyed by source/component ID (`internal/adapters/registry.go:11-15`).
Populated at boot by `connectSourcesDefault` (`cmd/joe/server.go:837`), which for
each component type does `ListByType` → `adapter.Connect(ctx, *src)` →
`registry.Register(src.ID, adapter)` (e.g. `server.go:838-848` for k8s, repeated
per type through `server.go:993`). Runtime registration via
`POST /api/v1/components` follows the same Connect-then-Register order
(`internal/api/components.go:181` then `:185`).

**How an adapter is retrieved for a call.** Only through `guard[T]`
(`internal/access/access.go:194-218`): it runs the allow/deny decision
(`permitForPrincipal` `:203`), then `a.registry.Get(sourceID)` (`:206`), then
type-asserts to the typed adapter interface (`:213`). `guard` is the *only* caller
of `registry.Get` (comment `access.go:191`).

**Does anything assume a ready, already-authenticated client?** Yes, weakly:
- `guard` itself does **not** check connectivity — it just returns the registered
  adapter (`access.go:206-217`). There is no readiness gate at the seam.
- Each adapter operation self-gates on `checkConnected()` requiring
  `a.connected == true` (e.g. prometheus `prometheus.go:375-380`, aws
  `aws.go:293-298`), a flag set at the **end** of `Connect` after the eager
  connectivity probe (k8s `ServerVersion` `k8s.go:82`; aws `STS GetCallerIdentity`
  `aws.go:218-219`; mongodb `ping` `mongodb.go:104`; prometheus buildinfo health
  check `prometheus.go:117-136`).

**Would a provider-handle model that defers credential acquisition past Connect
violate a lifecycle assumption?** Largely **no**, with one caveat:
- The registry stores an `Adapter` interface value and assumes nothing about its
  internals; deferring credential acquisition does not change the `map[string]Adapter`
  contract. The seam (`guard`) makes **no** authenticated-client assumption.
- For the **GREEN source-shaped** adapters (k8s, aws, ecr, redis-with-provider) and
  **HTTP per-call** adapters, credential acquisition is *already* deferred to
  call/transport time; a provider handle fits with no lifecycle change.
- **Caveat — the eager `Connect`-time connectivity probe.** Each `Connect` performs a
  live authenticated round-trip to set `connected=true`. A provider that genuinely
  cannot mint a credential until *after* boot (e.g. a federated token not yet
  available) would fail that probe and leave the component unregistered/`connected=false`.
  So the lifecycle assumption that *bites* is not "the seam needs a live client" but
  "**`Connect` itself needs a working credential at boot to pass its own health
  check**." A provider-handle model is compatible only if it can produce a credential
  at `Connect` time (or if the probe is relaxed to lazy verification — note nginx/helm
  already build their client lazily on first op, `nginx.go:145`, `helm.go:139`, and
  would be the natural template).

---

## 6. Refactor-distance classification

**GREEN** — SDK accepts a credential source (or the adapter already resolves the
secret per call); converting to a provider handle is a small change at `Connect`
(and at most one header-read site). **YELLOW** — SDK bakes the value at
construction, but the adapter can reconstruct-per-rotation or wrap cheaply.
**RED** — deep coupling requiring significant rework. **N/A** — no credential.

| Adapter | Construction style | SDK accepts source? | Class | Refactor distance |
|---|---|---|---|---|
| **kubernetes** | cached clientset from `rest.Config` (`k8s.go:69-79`) | **Yes** — transport refreshes (in-cluster/exec) | **GREEN** | (i) / often zero — already refresh-native |
| **nginx** | lazy clientset (`nginx.go:157`) | Yes (client-go) | **GREEN** | (i) / often zero |
| **helm** | lazy clientset (`helm.go:149`) | Yes (client-go) | **GREEN** | (i) / often zero |
| **aws** | cached ec2/eks/rds from `aws.Config` (`aws.go:213-215`) | **Yes** — `CredentialsProvider` chain + cache (`aws.go:262,286`) | **GREEN** | (i) / often zero — already refresh-native |
| **ecr** | cached ecr client (`ecr.go:111`) | Yes (same chain) | **GREEN** | (i) / often zero |
| **prometheus** | credential-free `http.Client`; header per call (`prometheus.go:366`) | n/a — secret re-read per call | **GREEN** | (i)+small (ii): one header site |
| **loki** | per-call header (`loki.go:311`) | n/a | **GREEN** | one header site |
| **jaeger** | per-call header (`jaeger.go:316`) | n/a | **GREEN** | one header site |
| **tempo** | per-call header (`tempo.go:342`) | n/a | **GREEN** | one header site |
| **splunk** | per-call header (`splunk.go:185`) | n/a | **GREEN** | one header site |
| **datadog** | per-call headers (`datadog.go:337`) | n/a | **GREEN** | one header site |
| **newrelic** | per-call header (`newrelic.go:212`) | n/a | **GREEN** | one header site |
| **dynatrace** | per-call header (`dynatrace.go:340`) | n/a | **GREEN** | one header site |
| **alertmanager** | per-call header (`alertmanager.go:234`) | n/a | **GREEN** | one header site |
| **grafana** | per-call header (`grafana.go:374`) | n/a | **GREEN** | one header site |
| **pagerduty** | per-call header (`pagerduty.go:285`) | n/a | **GREEN** | one header site |
| **falco** | per-call header (`falco.go:293`) | n/a | **GREEN** | one header site |
| **elasticsearch** | per-call auth (`elasticsearch.go:246`) | n/a | **GREEN** | one auth site |
| **oci** | per-call Basic auth (`oci.go:308`) | n/a | **GREEN** | one auth site |
| **artifactory** | per-call header (`artifactory.go:275`) | n/a | **GREEN** | one header site |
| **github** | per-call Bearer (`adapter.go:158,254,288`) | n/a | **GREEN** | a few header sites (shared `get`/`post`) |
| **gitlab** | per-call PRIVATE-TOKEN (`gitlab/adapter.go:225,259`) | n/a | **GREEN** | two header sites (shared `get`/`post`) |
| **argocd** | per-call Bearer (`argocd.go:324`) | n/a | **GREEN** | one header site |
| **redis** | cached pool, static `Password` (`redis.go:129,136`) | **Yes** — `CredentialsProviderContext` unused | **GREEN** | (i): swap `Password`→provider hook |
| **git** | local repo; `AuthMethod` at clone/pull (`git.go:79,92,109`) | **Yes** — go-git takes auth per network op | **GREEN** | (i)/(ii-light): build auth from provider per pull |
| **azure** | skeleton, no client (`azure.go:107`) | future `azidentity.TokenCredential` (source) | **GREEN** | n/a yet — build it source-shaped from day one |
| **postgres** | cached `*sql.DB`, password in DSN (`postgres.go:147,149`) | **No** (`database/sql`) | **YELLOW** | rebuild pool on rotation, or move to `pgxpool.BeforeConnect` |
| **mysql** | cached `*sql.DB`, password in DSN (`mysql.go:141,143`) | **No** (`database/sql`) | **YELLOW** | rebuild pool on rotation |
| **mongodb** | cached `*mongo.Client`, creds in URI (`mongodb.go:97-98`) | partial — OIDC callback exists, unused | **YELLOW** | wire `options.Credential` OIDC callback, or rebuild |
| **kafka** | cached `*kafka.Client`, **no auth wired** (`kafka.go:163-166`) | transport SASL can be dynamic, unused | **YELLOW** | build SASL transport first, then make it dynamic |
| **envoy** | admin API, no auth (`envoy.go:74-98`) | — | **N/A** | no credential |
| **terraform** | local `StatePath` (`terraform.go:114`) | — | **N/A** | no credential |

**Launch-scope read:** the credential-refresh effort is dominated by GREEN. The 18
HTTP adapters each need the same one-line-ish change (header read → provider call),
and K8s/AWS/ECR are effectively free because their SDKs already operate on a
credential source. The only genuinely awkward cases are the four
connection-oriented datastores (postgres, mysql, mongodb, kafka), and even those are
reconstruct/-wire, not rewrite — **no adapter is RED.** The one cross-cutting design
decision is whether `Connect`'s eager connectivity probe (§5) must succeed at boot;
the lazy-build template already exists in nginx/helm.

---

## Appendix — files cited

- `internal/adapters/adapter.go`, `internal/adapters/registry.go`
- `internal/adapters/k8s/k8s.go`, `internal/adapters/aws/aws.go`,
  `internal/adapters/azure/azure.go`, `internal/adapters/git/git.go`,
  `internal/adapters/github/adapter.go`, `internal/adapters/gitlab/adapter.go`,
  `internal/adapters/gitops/argocd/argocd.go`
- `internal/adapters/observability/{prometheus,loki,jaeger,tempo,splunk,datadog,newrelic,dynatrace}/*.go`
- `internal/adapters/alerting/{alertmanager,grafana,pagerduty}/*.go`
- `internal/adapters/datastore/{postgres,mysql,redis,kafka,mongodb,elasticsearch}/*.go`
- `internal/adapters/registry/{ecr,oci,artifactory}/*.go`
- `internal/adapters/networking/{nginx,envoy}/*.go`,
  `internal/adapters/packaging/helm/helm.go`,
  `internal/adapters/security/falco/falco.go`,
  `internal/adapters/iac/terraform/terraform.go`
- `internal/access/access.go`, `internal/access/k8s.go`
- `cmd/joe/server.go`, `internal/api/components.go`, `internal/store/models.go`
- `go.mod` (SDK versions)
