# Credential Handling — Current State Investigation

**Date:** 2026-06-09
**Scope:** Read-only. Re-derived against the live tree. No code changes.
**Question:** How do Joe's adapters obtain the credentials they use to reach
their backends today, and how far is that from resolving the credential at the
guarded accessor seam (the seam that decides allow/deny on principal +
componentID + action)?

---

## TL;DR / Core finding

- **Every** infrastructure adapter obtains its credential the **same** way: from
  `store.Component.Config` (a freeform `json.RawMessage` blob), handed to the
  adapter through the `Adapter.Connect(ctx, source store.Component)` interface
  method. No adapter reads its own backend credential from an env var; only the
  cloud SDKs (AWS, K8s) consult their own env/file chains internally.
- Credential selection is therefore **fully connected to the component model** —
  the credential *is* a sub-field of the component record. (This contradicts any
  assumption that credentials are "disconnected" from the component model.)
- There is **no shared credential provider / resolver / factory / connection-
  profile abstraction**. The only shared seam is (a) the `Connect(ctx, Component)`
  interface plus the `Component.Config` JSON-blob convention, and (b) a transparent
  **encrypt-at-rest decorator** (`encryptedComponentRepository`).
- The credential is **bound into the adapter at `Connect` time — at boot (or at
  component-registration time) — long BEFORE and entirely independent of the
  accessor allow/deny decision.** At request time the accessor only resolves a
  pre-built, already-credentialed adapter by `sourceID`.
- At the allow/deny decision point the accessor holds **bare values**
  (`principals`, `sourceID` string, `action`) and **no** resolved component
  record and **no** store handle. Reaching the credential there today would
  require a **new store lookup** (the `Accessor` struct has no component
  repository), or exposing adapter internals (the `Adapter` interface exposes
  only `Connect`/`Disconnect`/`Status`).

**Refactor distance to "resolution-at-the-seam":** today binding happens
*eagerly at boot, upstream of the seam*; the seam is a pure
allow/deny + resolve-by-ID gate with no component record in hand. Moving credential
resolution to the seam means giving the seam the component record (a store lookup
it does not currently make) and deferring/centralising connection construction to
the decision point.

---

## 1. Adapter credential inventory

The adapter contract is `Connect(ctx context.Context, source store.Component) error`
(`internal/adapters/adapter.go:10-20`). Credentials flow in through the `source`
argument. `store.Component.Config` is `json.RawMessage` (`internal/store/models.go:13`).

**Universal pattern:** every adapter's `Connect` calls its own package-local
`ParseConfig(source.Config)`, unmarshals the JSON into a per-adapter `Config`
struct, and binds the result into the adapter's private `a.config`. None read a
backend credential from an env var directly.

### Kubernetes — `internal/adapters/k8s/`
- `Connect`: `internal/adapters/k8s/k8s.go:53`; parses config at `k8s.go:57`
  (`ParseConfig(source.Config)`), `config.go:16-24`.
- Config fields: `Kubeconfig`, `Context`, `InCluster` (`k8s/config.go`).
- Connection built at `k8s.go:112-132` (`buildRESTConfig`):
  - `rest.InClusterConfig()` when `InCluster` (`k8s.go:113-114`) — uses the
    pod service-account token/file via the K8s SDK.
  - else `clientcmd.NewDefaultClientConfigLoadingRules()` with
    `rules.ExplicitPath = expanded` from `cfg.Kubeconfig` (`k8s.go:117-123`) —
    a **kubeconfig file path** carried in `Component.Config`. If empty, the SDK's
    default loading rules apply (which themselves consult `$KUBECONFIG`).
- Direct env read by Joe code: **none**; SDK default chain may read env/file.

### AWS — `internal/adapters/aws/`
- `Connect`: `internal/adapters/aws/aws.go:185`; JSON extracted `aws.go:190-198`;
  `ParseConfig` `aws/config.go:19-39`.
- Config fields: `Region` (required), `Profile`, `AccessKey`, `SecretKey`,
  `RoleARN`.
- Credential chain in `buildAWSConfig` (`aws.go:260-290`):
  - `config.LoadDefaultConfig(ctx, config.WithRegion(...))` — **AWS SDK v2 default
    credential chain** (`aws.go:262`); reads `AWS_*` env, shared config, IAM role,
    etc. internally.
  - profile override `config.WithSharedConfigProfile(cfg.Profile)` (`aws.go:268-276`).
  - static creds `credentials.NewStaticCredentialsProvider(cfg.AccessKey,
    cfg.SecretKey, "")` from `Component.Config` (`aws.go:279-281`).
  - assume-role `stscreds.NewAssumeRoleProvider(stsClient, cfg.RoleARN)` (`aws.go:284-287`).

### Azure — `internal/adapters/azure/`
- `Connect`: `internal/adapters/azure/azure.go:90`; `ParseConfig` `azure/config.go:20-37`.
- Config fields: `SubscriptionID` (required), `TenantID`, `ClientID`,
  `ClientSecret`, `ResourceGroup`, `Environment`.
- **Skeleton**: `Connect` only stores `cfg` and sets `connected = true`
  (`azure.go:107-108`); no Azure SDK client is built yet. Credential is read from
  `Component.Config` only.

### Git provider adapters
- **git** (`internal/adapters/git/`): `Connect` `git/git.go:69`; `ParseConfig`
  `git/config.go:18-36`. Fields `URL`, `Branch`, `AuthType`, `SSHKeyPath`,
  `HTTPToken` (`git/config.go:9-15`). `buildAuth(cfg)` uses `cfg.SSHKeyPath`
  (an **SSH key file path**) or `cfg.HTTPToken` (`git/git.go:152-176`).
- **github** (`internal/adapters/github/`): `Connect` `github/adapter.go:71`;
  `ParseConfig` `github/config.go:18-33` (requires `Token`). Fields `BaseURL`,
  `Token`, `WebhookSecret` (`github/config.go:9-15`). Token used as
  `"Bearer "+cfg.Token` in request headers (`github/adapter.go:158,254,288`).
- **gitlab** (`internal/adapters/gitlab/`): `Connect` `gitlab/adapter.go:69`;
  `ParseConfig` `gitlab/config.go:17-32` (requires `Token`). Fields `BaseURL`,
  `Token`, `WebhookSecret`. Token used as `PRIVATE-TOKEN` header
  (`gitlab/adapter.go:225,259`).
- **gitops/argocd** (`internal/adapters/gitops/argocd/`): `Connect`
  `argocd/argocd.go:118`; `ParseConfig` `argocd/config.go:16-30` (requires `URL`
  and `Token`). Fields `URL`, `Token`, `InsecureTLS`. Token used as
  `"Bearer "+a.config.Token` (`argocd/argocd.go:324`).

### Observability backends behind the category observe API
All read `Component.Config` → `ParseConfig` → headers/connection. Token/key
fields per adapter:

| Adapter | Connect (file:line) | Config fields | Auth applied (file:line) |
|---|---|---|---|
| prometheus | `observability/prometheus/prometheus.go:103-111` | `URL`(req), `OrgID`, `APIKey` (`prometheus/config.go:10-14`) | Bearer `APIKey` + `X-Scope-OrgID` (`prometheus.go:367-369`) |
| loki | `observability/loki/loki.go:89-97` | `URL`(req), `OrgID`, `APIKey` (`loki/config.go:10-14`) | Bearer + OrgID (`loki.go:311-316`) |
| jaeger | `observability/jaeger/jaeger.go:88-96` | `URL`(req), `APIKey` (`jaeger/config.go:10-13`) | Bearer (`jaeger.go:316-318`) |
| tempo | `observability/tempo/tempo.go:96-104` | `URL`(req), `APIKey`, `OrgID` (`tempo/config.go:10-14`) | Bearer + OrgID (`tempo.go:342-347`) |
| splunk | `observability/splunk/splunk.go:87-95` | `URL`(req), `Token`(req), `Index` (`splunk/config.go:10-14`) | Bearer `Token` (`splunk.go:107`) |
| datadog | `observability/datadog/datadog.go:112-120` | `Site`, `APIKey`(req), `AppKey`(req) (`datadog/config.go:13-16`) | `DD-API-KEY`/`DD-APPLICATION-KEY` (`datadog.go:337-340`) |
| newrelic | `observability/newrelic/newrelic.go:85-93` | `APIKey`(req), `AccountID`(req), `Region` (`newrelic/config.go:11-15`) | `Api-Key` header (`newrelic.go:212`) |
| dynatrace | `observability/dynatrace/dynatrace.go:108-116` | `URL`(req), `Token`(req) (`dynatrace/config.go:14-15`) | `Api-Token` header (`dynatrace.go:340`) |

Alerting backends (the `alerts` category): **alertmanager**
(`alerting/alertmanager/alertmanager.go:90-98`, `config.go:11-12`: `URL`(req),
`APIKey`; Bearer at `alertmanager.go:234-236`), **grafana**
(`alerting/grafana/`, `config.go` has `URL`+`APIKey`), **pagerduty**
(`alerting/pagerduty/`, `config.go` has `APIKey`).

### Other infrastructure adapters (same pattern, for completeness)
- **datastore**: redis (`datastore/redis/redis.go:113-121`; `Host`,`Port`,
  `Password`,`DB`,`TLSEnabled`), postgres (`postgres.go:132-147`; builds DSN with
  `Password`), mysql (`mysql.go:126-141`; builds DSN with `Password`), mongodb
  (`mongodb.go:83-97`; `URI` connection string), kafka (`kafka.go:142-150`;
  `Brokers`,`Username`,`Password`,`SASLMechanism`), elasticsearch
  (`elasticsearch.go:88-96`; `URL`,`Username`,`Password`,`APIKey`).
- **registry**: ecr (`registry/ecr/ecr.go:92-106`; AWS-style creds + `buildAWSConfig`),
  oci (`registry/oci/oci.go:87`; `RegistryURL`,`Username`,`Password`), artifactory
  (`registry/artifactory/artifactory.go:81`; `BaseURL`,`Username`,`APIKey`).
- **iac/terraform** (`iac/terraform/terraform.go:118`; `StatePath` — a **local
  file path**, no credential).
- **networking**: envoy (`networking/envoy/envoy.go:75`; `URL` only), nginx
  (`networking/nginx/nginx.go:126`; `KubeconfigPath`,`Context` — K8s client built
  lazily).
- **security/falco** (`security/falco/falco.go:94-102`; `URL`,`APIKey`).
- **packaging/helm** (`packaging/helm/helm.go:123`; `KubeconfigPath`,`Context`;
  REST config built lazily).

---

## 2. Shared abstraction vs. ad-hoc

**No** shared credential provider, resolver, factory, or connection-profile type
exists. Search confirms each adapter ships its own `config.go` + `ParseConfig`,
and each builds its own client.

What *is* shared is weaker than a credential abstraction:

1. **The interface convention** — `Adapter.Connect(ctx, store.Component)`
   (`internal/adapters/adapter.go:10-20`) plus the freeform
   `Component.Config json.RawMessage` blob (`internal/store/models.go:13`). Every
   adapter agrees to receive its credential inside that blob, but each defines its
   own private schema for it.
2. **Encrypt-at-rest decorator** — `encryptedComponentRepository`
   (`internal/store/encrypted_components.go:14-127`) transparently encrypts
   `source.Config` on write (`encrypted_components.go:82`) and decrypts on read
   (`encrypted_components.go:108`) with AES-256-GCM (`internal/crypto/crypto.go`).
   This is a storage concern, not a resolution seam — it hands back a fully
   decrypted `Component.Config` to whoever reads the component.
3. **Per-type adapter constructor switch** — `newAdapterForType(req.Type)`
   (`internal/api/components.go:53+`) and the per-type loops in
   `connectSourcesDefault` (`cmd/joe/server.go:837-993`). This selects which
   adapter type to build; it is not a credential resolver.

There is no function anywhere that, given `(principal, componentID, action)` or
even just `componentID`, returns a credential or connection profile.

---

## 3. Configuration surface (config package)

Credential-relevant configuration in `internal/config/`:

- **OIDC** (`config.go:85-96`): `Issuer` (88), `ClientID` (90), `ClientSecret`
  (92), `RedirectURL` (95).
- **ServiceAccount** (`config.go:145-151`): `Name` (148), `Key` (150) — plaintext
  bearer token for machine auth. Held as `ServerConfig.ServiceAccounts []ServiceAccount`
  (`config.go:160`).
- **TLS** (`ServerConfig`): `TLSCertFile` (161), `TLSKeyFile` (162).
- **Database** (`config.go:105-113`): `DSN` (112) — may embed DB credentials.
- **LLM** (`config.go:219-238`): `Current`, `Available map[string]ModelConfig` —
  **no API-key field**; provider keys come from env (see §7).

**Loading:** `Load(configPath)` `config.go:310-396`; reads YAML via `loadFromFile`
`config.go:454-476` (`os.ReadFile` 455, `yaml.Unmarshal` 460); env overrides
`applyEnvOverrides` (called 352).
**Defaults:** `defaultConfig()` `config.go:399-449`; constants `config/constants.go`.
**Validation:** `ValidateAPIKeys` `validation.go:92-110`; `AutoSelectProvider`
`validation.go:38-70` (called `cmd/joe/server.go:200`); `ValidateCostCurrency`
`validation.go:129-148` (called `config.go:381`).

**Env-sourced credentials/connection in the config path:**
- `JOE_API_KEY` → service-account key (`config.go:534`).
- `JOE_DATABASE_DSN` → DB connection string (`config.go:540`).
- LLM provider keys checked (not stored): `ANTHROPIC_API_KEY`,
  `GEMINI_API_KEY`/`GOOGLE_API_KEY` (`validation.go:43-44,97-103`; names in
  `internal/env/keys.go:7-9`).
- CLI/Slack modes read `SLACK_BOT_TOKEN`, `SLACK_APP_TOKEN`, `JOE_API_KEY`,
  `JOE_SERVER` (`cmd/joe/main.go:217-263`).

**Important:** adapter backend credentials are **not** in the config package at
all — they live exclusively in `Component.Config` rows in the DB.

---

## 4. Component-record linkage

**Type** — `store.Component` (`internal/store/models.go:9-19`): fields `ID`,
`Type`, `Name`, `Config json.RawMessage` (line 13), `Status`, `LastSyncAt`,
`LastError`, `CreatedAt`, `UpdatedAt`.

**Schema** — table created as `sources` in `001_initial.up.sql:2-12` with column
`config TEXT NOT NULL` (line 6), renamed to `components` in
`023_source_to_component.up.sql` (`ALTER TABLE sources RENAME TO components`).
There is **no** dedicated credential / secret / connection-profile / backend-
identity column — the credential is embedded inside the freeform `config` blob.

**Registration path** — `POST /api/v1/components` handler
(`internal/api/components.go`): builds `store.Component{ID,Type,Name,Config}`
(`components.go:170-175`), connects the adapter with that component
(`adapter.Connect(ctx, *source)` `components.go:181`), registers it
(`components.go:185`), then persists via `Components.Create`
(`components.go:188`) — which, because the store is wrapped, encrypts `Config`.

**Verdict:** credential selection is **entirely connected to the component model
today** — the credential is literally a sub-field (`Config`) of the component
record, encrypted at rest. It is *not* disconnected. What is missing is not the
linkage but a *resolver seam*: nothing turns a component into a credential except
each adapter's own `Connect`.

---

## 5. The accessor seam contents (what's in hand at allow/deny)

**Location:** `internal/access/access.go`. The single enforcement chokepoint is
`Accessor.permit` (`access.go:120-172`), reached via `permitForPrincipal`
(`access.go:180-182`) and the generic `guard[T]` (`access.go:194-218`).

**Input signature at the decision point:**
```go
func (a *Accessor) permit(ctx context.Context, principals rbac.PrincipalSet,
    sourceID string, action rbac.Action) error               // access.go:120
func guard[T any](a *Accessor, ctx, principal rbac.Principal,
    sourceID string, action rbac.Action, typeName string) (T, error) // access.go:194
```

At decision time the seam has, as **bare values**:
- `principals rbac.PrincipalSet` (a size-1 set built from one `rbac.Principal`,
  `access.go:174-182`),
- `sourceID string` (the componentID),
- `action rbac.Action`.

The decision itself is `a.engine.Decide(ctx, principals, sourceID, action)`
(`access.go:133`). The audit row written at the seam (`access.go:151-159`) also
carries only `Principal`, `Action`, `Zone`, `ComponentID`, `Reason`.

**There is NO resolved `store.Component` record in hand**, and the `Accessor`
struct itself holds **no store / component repository** — its fields are
`registry *adapters.Registry`, `graph graph.GraphStore`, `engine
*rbac.PolicyEngine`, `auditRepo audit.Repository` (`access.go:67-83`).

**Could a credential/connection profile be reached here without a new store
lookup?** Not as a value:
- The raw `Component.Config` (decrypted credential) requires reading the component
  — and the accessor has no store handle, so that is a **new store lookup**.
- After the decision, `guard` calls `a.registry.Get(sourceID)` (`access.go:206`),
  which returns the **already-connected adapter** holding the credential in its
  private `a.config` — reachable without a store lookup, **but** the `Adapter`
  interface exposes only `Connect`/`Disconnect`/`Status`
  (`internal/adapters/adapter.go:10-20`), so the credential is encapsulated and
  not retrievable through the seam.

So today the credential is *near* (inside the resolved adapter) but *opaque* at
the seam; the component record carrying it is *absent* and would need a fresh
lookup.

---

## 6. Binding-point trace (the relative ordering — core finding)

Path from "tool call resolved to a target component" to "adapter invoked against
its backend," and where the credential binds:

1. **Boot, BEFORE any request:** `runServerWithDeps` wires the encrypted store
   (`cmd/joe/server.go:377-382`), then calls `deps.connectComponents(...)`
   (`server.go:387`), which is `connectSourcesDefault` (`server.go:120`,
   defined `server.go:837-993`).
2. **`connectSourcesDefault`** loads each component type from the store
   (`sqlStore.Components.ListByType(...)`, e.g. `server.go:838`) — the read goes
   through the encrypted repository, so `Config` is **decrypted here**
   (`encrypted_components.go:108`). For each component it does:
   - `adapter := <type>.New()`
   - **`adapter.Connect(ctx, *src)`** ← **CREDENTIAL BOUND HERE**: the adapter
     parses `src.Config` and stores it in its private `a.config`
     (e.g. `server.go:844`, `:858`, `:872`…).
   - `registry.Register(src.ID, adapter)` (e.g. `server.go:848`).
   (Runtime registration via `POST /api/v1/components` follows the same
   Connect-then-Register order: `components.go:181` then `:185`.)
3. **Request time:** a typed accessor method (e.g.
   `Accessor.K8sListResources`, `internal/access/k8s.go:12`) calls
   `guard[...](a, ctx, principal, sourceID, action, ...)` →
   **`permit` decides allow/deny** (`access.go:203` → `access.go:120-172`) →
   only then `a.registry.Get(sourceID)` (`access.go:206`) returns the
   **pre-connected, pre-credentialed** adapter → the typed adapter method runs
   against the backend (e.g. `ad.ListResources(...)` `k8s.go:17`).

**Ordering verdict:** the credential is bound **before** the accessor decision
point — eagerly, at process boot (or at registration), into a long-lived
per-component adapter instance. The accessor decision happens **after** binding
and merely *selects an already-credentialed adapter by ID*. The decision point
never touches the credential.

**Refactor distance to resolution-at-the-seam:** large in ordering, modest in
plumbing. To resolve the credential at the seam you would (a) give the `Accessor`
a component/credential lookup it does not currently hold, (b) move connection
construction from eager-boot `Connect` to the post-decision moment inside
`guard`, so the *same* code that decides allow/deny on `(principal, componentID,
action)` also resolves the component → credential → connection. The plumbing is
contained because the credential already lives on the component record and the
seam is already singular and already keyed by `componentID`; what is missing is
the store handle on the accessor and a deferred (per-decision) connection step
instead of a boot-time one.

---

## 7. LLM provider key path — REFERENCE ONLY (not an infra-component credential)

Distinct from items 1–6: this is the one existing **in-process** secret path.

- The key is **never** stored in `Component.Config` or the DB. It is read from the
  process environment at client-construction time:
  `apiKey := os.Getenv(env.AnthropicAPIKey)` in `claude.NewClient`
  (`internal/llm/claude/claude.go:50`), errored if empty (`claude.go:51-52`).
- It is handed to the vendor SDK: `anthropic.NewClient(option.WithAPIKey(apiKey))`
  (`claude.go:55`) and **held inside the SDK client struct**, referenced by the
  adapter as `Client{client, model}` (`claude.go:61-64`). The raw key is not kept
  in a Joe-owned field beyond the SDK client.
- Construction goes through `llmfactory.NewAdapter` (`internal/llmfactory/factory.go:20-32`),
  which validates the key env var via `config.ValidateAPIKeys` (`factory.go:22`)
  before building the client. `HasProviderAPIKey` (`factory.go:48-57`) reports
  presence **without returning key material**.
- Env var names: `internal/env/keys.go:7-9` (`ANTHROPIC_API_KEY`,
  `GEMINI_API_KEY`, `GOOGLE_API_KEY`).

**Pattern relevance:** unlike infra credentials, the LLM key is process-scoped
(one per deployment), env-sourced, and held only in the vendor SDK client — there
is no per-component selection and no accessor involvement.

---

## 8. Leakage surface (located only — no fixes proposed)

### A. API responses returning the full `Component` (incl. decrypted `Config`)
Because reads go through the encrypting repository, `Config` is **decrypted**
before these handlers serialize it:
- `GET /api/v1/components` → `writeJSON(..., {"components": components, ...})`
  serializes full `[]*store.Component` incl. `Config`
  (`internal/api/components.go:47-48`).
- `GET /api/v1/components/{id}` → `writeJSON(w, http.StatusOK, source)`
  (`internal/api/components.go:217`).
- `POST /api/v1/components` → echoes the created component incl. `Config`
  (`internal/api/components.go:193`).

### B. Connection strings / URIs in error messages
- **mongodb**: `fmt.Errorf("ping MongoDB at %s: %w", cfg.URI, err)`
  (`internal/adapters/datastore/mongodb/mongodb.go:106`) — `cfg.URI` may embed
  `user:password@host`.
- postgres builds a `password=...` DSN (`postgres.go:146-147`) and mysql builds a
  `user:password@tcp(...)` DSN (`mysql.go:140-141`); these DSN strings are held in
  memory (connect errors at `postgres.go` / `mysql.go` log only host:port, not the
  DSN — but the value exists and could surface via panics/wrapping).

### C. Backend URLs in `Connect` error messages (lower sensitivity; URLs, not secrets)
All wrap `cfg.URL`/`BaseURL`/`RegistryURL` in connect/ping errors:
`alertmanager.go:114`, `grafana.go:140`, `loki.go:113`, `prometheus.go:127`,
`tempo.go:120`, `jaeger.go:112`, `splunk.go:111`, `dynatrace.go:132`,
`elasticsearch.go:112`, `falco.go:118`, `argocd/argocd.go:136`,
`oci/oci.go:113`, `artifactory/artifactory.go:96`, `datadog.go:136`. These errors
reach operator logs via `writeInternalError`/`writeBadRequest`
(`internal/api/helpers.go`).

### D. LLM prompt-assembly payload — NOT leaking `Config` (verified)
- The LLM-facing list-components tool returns only `{id, type, name}` and
  **omits `Config`** (`internal/tools/core/listcomponents.go:55-84`). Safe.
- System prompts in `internal/prompts/` contain no credentials.
- No direct `slog`/`log` call serializing `store.Component.Config` was found in
  the adapter packages.
- Residual risk (not a confirmed leak): `internal/tools/core/graphquery.go`
  returns `graph.Node` objects whose `Metadata` map is arbitrary; if any path ever
  writes credential material into node metadata it would reach the LLM. No such
  writer was identified in this pass.

### E. Audit rows
- The accessor audit row (`internal/access/access.go:151-159`) records
  `Principal`, `Action`, `Zone`, `ComponentID`, `Decision`, `Reason` — **no
  credential**. No path was found writing `Component.Config` into an audit row.

### F. Defense-in-depth already present
- `safety.RedactSecretsFromResponse` (`internal/safety/secrets.go`) scrubs known
  secret values (raw + base64) from text before it reaches the LLM context — a
  backstop, not a guarantee, and only for secret values it is told about.

---

## Appendix — files cited
- `internal/adapters/adapter.go`, `internal/adapters/registry.go`
- `internal/adapters/{k8s,aws,azure,git,github,gitlab}/…`,
  `internal/adapters/gitops/argocd/…`, `internal/adapters/observability/*`,
  `internal/adapters/alerting/*`, `internal/adapters/datastore/*`,
  `internal/adapters/registry/*`, `internal/adapters/iac/terraform/…`,
  `internal/adapters/networking/*`, `internal/adapters/security/falco/…`,
  `internal/adapters/packaging/helm/…`
- `internal/access/access.go`, `internal/access/k8s.go`
- `internal/store/models.go`, `internal/store/encrypted_components.go`,
  `internal/store/migrations/001_initial.up.sql`,
  `internal/store/migrations/023_source_to_component.up.sql`
- `internal/crypto/crypto.go`
- `internal/config/config.go`, `internal/config/validation.go`,
  `internal/config/constants.go`, `internal/env/keys.go`
- `internal/llmfactory/factory.go`, `internal/llm/claude/claude.go`
- `internal/tools/core/listcomponents.go`, `internal/tools/core/graphquery.go`
- `internal/api/components.go`, `internal/api/helpers.go`
- `internal/safety/secrets.go`
- `cmd/joe/server.go`, `cmd/joe/main.go`
