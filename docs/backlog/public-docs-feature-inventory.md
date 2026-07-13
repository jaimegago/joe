# Public Docs Feature Inventory

Status: open

The authoritative, verified spec of what a built-from-source `joe` binary actually
ships **right now** — written so the public documentation on joeagent.dev describes
only features an operator or end user can reach. Every claim is re-derived from the
live tree and tagged with `file:line`; docs, DECISIONS.md, and prior summaries were
treated as leads only.

**Classification key**
- **SHIPPED** — constructed on a path reachable from `cmd/joe` or the running
  binary's wiring/factory, and reachable by an operator or end user.
- **PARTIAL** — wired but incomplete/skeleton/non-functional in a way that affects
  the user. The specific gap is named with `file:line`.
- **PRESENT-BUT-UNWIRED** — exists in the tree but is constructed on no
  `cmd/joe`-reachable path (referenced only by tests, or dead). **For documentation
  purposes this is NOT shipped.**

The single most load-bearing section for doc authors is
[§9 Do-Not-Document](#9-do-not-document--every-partial-and-present-but-unwired-finding),
which collects every PARTIAL and PRESENT-BUT-UNWIRED finding in one place.

---

## 1. CLI surface

Dispatch happens in `runWithDeps` (`cmd/joe/main.go:587`): a non-flag first arg
selects a subcommand; a leading flag (`--config`) or no arg runs the server. The
top-level usage banner lists the subcommands (`cmd/joe/main.go:620-632`).

### SHIPPED

- **`joe` (bare, or `joe --config <path>`)** — boots the HTTP API daemon, Joe's
  default behavior. `runWithDeps` falls through to `deps.runServer` (`cmd/joe/main.go:616`);
  entrypoint `runServer` → `runServerWithDeps` (`cmd/joe/server.go:177-206`).
- **`joe mcp`** — MCP stdio server. `runMCPCommand` (`cmd/joe/main.go:216`). No flags;
  reads `JOE_SERVER` (default `http://localhost:7777`) and `JOE_API_KEY` from env
  (`main.go:217-221`). It is a **client of a running daemon**, not part of server boot.
- **`joe slack`** — Slack Socket-Mode bot. `runSlackCommand` (`cmd/joe/main.go:247`).
  No flags; requires `SLACK_BOT_TOKEN` and `SLACK_APP_TOKEN` (exits 1 if missing,
  `main.go:248-257`), reads `JOE_SERVER`/`JOE_API_KEY`. Also a client of a running daemon.
- **`joe skills <install|list|remove|update|approve|reject|reload>`** —
  `runSkillsCommand` (`cmd/joe/main.go:294`). Invocation shapes as the parser reads them:
  - `install <repo-url> [--ref <branch|tag|commit>] [--subdir <path>]` — flags may
    appear before or after the positional (`reorderFlagsFirst`, `main.go:349,564`);
    requires exactly one positional (`main.go:352`).
  - `list` — no args.
  - `remove <skill-name> [--force]` — exactly one positional (`main.go:411`).
  - `update [<skill-name>]` — at most one positional (`main.go:437`).
  - `approve <skill-name>` / `reject <skill-name>` — exactly one positional
    (`main.go:466,488`).
  - `reload` — no positionals; calls the running server (`c.ReloadSkills`, `main.go:526`).
  install/list/remove/update/approve/reject operate on `~/.joe/skills/` on the local
  filesystem; only `reload` contacts the daemon.
- **`joe incident <status|declare|resolve|list>`** — `runIncidentCommand`
  (`cmd/joe/incident.go:31`; dispatched `main.go:603`). Shapes:
  - `status` — no flags; prints regime state (`incident.go:84`).
  - `declare --session <id> [--kind <kind>] [--reason <reason>]` — `--session`
    **required** (`incident.go:118`); `--kind` defaults `"human"` (`incident.go:111`).
  - `resolve [--reason <reason>]` (`incident.go:143`).
  - `list` — **honest stub**: prints that history lives in the audit log and exits 2;
    there is no `/regime/history` endpoint (`incident.go:49-51`). (Counted SHIPPED as a
    command that runs, but it performs no query — see §9.)
- **`joe panic [--config <path>] [--reason <text>]`** — `runPanicCommand`
  (`cmd/joe/main.go:121`). Sends `POST /api/v1/panic` over the loopback-keyed client;
  `--reason` defaults `"operator triggered via CLI"` (`main.go:125`).
- **`joe unlock [--reason <text>]`** — `runUnlockCommand` (`cmd/joe/main.go:169`).
  Opens the SQLite DB **directly** (never contacts the daemon), clears the
  `cluster_panic_state` row if set; idempotent; takes effect on restart (`main.go:177-208`).

There is **no** `joe serve`, `joe rbac`, or `joe admin` subcommand — RBAC/zone
provisioning is the admin REST surface, not a CLI (`cmd/joe/main.go:611-616`).

---

## 2. Components and adapters

A managed system is a **component** (`store.Component`, `components` table). There is
**no single type→adapter factory** — two reachable construction sites exist and they
disagree on coverage:

1. **`connectSourcesDefault`** (`cmd/joe/server.go:1056`) — boot-time; constructs +
   `Connect`s + `Register`s adapters for exactly **11 types**: kubernetes, git, aws,
   azure, falco, datadog, splunk, dynatrace, newrelic, github, gitlab
   (server.go:1062–1204).
2. **`newAdapterForType`** (`internal/api/components.go:131`) — runtime; the type→adapter
   switch behind `POST /components/{id}/test` and the only post-boot (re)register path.
   Covers 23 types (components.go:133-178), but **omits** datadog/splunk/dynatrace/newrelic
   (boot-only) and every registry/cloud-metrics type.

A type is constructible-and-registerable iff it appears in at least one site.

**Credential seam.** Only **two** credential provider Kinds ship: `KindStatic`
(env-var indirection) and `KindKubeconfigExec` (`internal/credential/provider.go:95-104`).
The governed credential-entry path is promotion (`buildArmedConfig`,
`internal/api/components.go:758`), which writes only `env_var`/kubeconfig references and
**rejects inline secret `value`** (components.go:780). The wired-type registry —
`internal/credential/wiring.go:43` — lists **16 type-strings** as having a governed
credential path. Types that read inline config credentials (datastores, datadog, git,
aws, registry) have **no governed credential path**.

### SHIPPED (real `Connect` + reachable construction + governed credential path)

All resolve credentials through `credential.Select` and have a real `Connect` that
establishes/probes a live client.

| Type | Connect verdict (file:line) | Construction | Wired Kind |
|---|---|---|---|
| kubernetes | REAL, probes ServerVersion (`adapters/.../k8s.go:55,131`) | both sites | KubeconfigExec (`wiring.go:46`) |
| github | REAL (`github/adapter.go:72,86`) | both sites | Static (`wiring.go:44`) |
| gitlab | REAL (`gitlab/adapter.go:70,84`) | both sites | Static (`wiring.go:45`) |
| prometheus | REAL, probes `/api/v1/status/buildinfo` (`prometheus.go:99,121`) | newAdapterForType:141 | Static (`wiring.go:47`) |
| mimir | REAL (shares prometheus adapter) | newAdapterForType:141 | Static (`wiring.go:48`) |
| loki | REAL, probes `/loki/api/v1/labels` (`loki.go:85,107`) | newAdapterForType:143 | Static (`wiring.go:49`) |
| tempo | REAL, probes `/api/status` (`tempo.go:92,114`) | newAdapterForType:145 | Static (`wiring.go:50`) |
| jaeger | REAL, probes `/api/services` (`jaeger.go:84,106`) | newAdapterForType:147 | Static (`wiring.go:51`) |
| alertmanager | REAL, probes `/api/v2/status` (`alertmanager.go:86,108`) | both sites | Static (`wiring.go:55`) |
| pagerduty | REAL, probes `/abilities` (`pagerduty.go:91,113`) | newAdapterForType:151 | Static (`wiring.go:56`) |
| grafana | REAL, probes `/api/health` (`grafana.go:112,134`) | newAdapterForType:153 | Static (`wiring.go:57`) |
| argocd | REAL, probes `/api/version` (`argocd.go:119,132`) | newAdapterForType:167 | Static (`wiring.go:59`) |
| falco | REAL, probes `/api/v1/events` (`falco.go:90,112`) | both sites | Static (`wiring.go:58`) |
| splunk | REAL, probes `/services/server/info` (`splunk.go:83,105`) | server.go:1147 (boot only) | Static (`wiring.go:52`) |
| dynatrace | REAL, probes `/api/v2/metrics` (`dynatrace.go:104,126`) | server.go:1161 (boot only) | Static (`wiring.go:53`) |
| newrelic | REAL, probes NerdGraph (`newrelic.go:81,104`) | server.go:1175 (boot only) | Static (`wiring.go:54`) |

**Credential-less but fully functional (treat as SHIPPED if "no credential needed"
counts):** `terraform` (REAL, reads local state, `terraform.go:114`,
newAdapterForType:169) and `envoy` (REAL, probes `/server_info`, `envoy.go:74`,
newAdapterForType:175). Both are credential-less by design (`wiring.go:39`).

### PARTIAL

- **azure** — **skeleton Connect**: parses config, sets `connected=true`, returns nil,
  never contacts Azure (`adapters/azure/azure.go:90-110`). Boot-wired (server.go:1104)
  and in newAdapterForType:135, but non-functional. Not in the credential seam. This is
  the clearest "wired but broken" type.
- **helm** — skeleton Connect (parses config, real k8s client built lazily,
  `helm.go:119`); newAdapterForType:171; no credential wiring.
- **nginx-ingress** — skeleton Connect (lazy k8s init, `nginx.go:125`);
  newAdapterForType:173; no credential wiring.
- **git** — REAL Connect (`git.go:69`), both sites, but **no governed credential path**
  (ssh-key/http-token discriminated by `auth_type`, absent from `wiring.go:35`);
  credentials only via inline config.
- **aws** — REAL Connect (STS GetCallerIdentity probe, `aws.go:185`), both sites, but
  uses the AWS SDK chain, **not** the Joe credential seam; no governed credential path.
- **datadog** — REAL Connect (probes `/api/v1/validate`, `datadog.go:107`), but
  **boot-wired only** (server.go:1133) and **absent from newAdapterForType** — no
  post-boot test/re-register path; reads inline `api_key`+`app_key`; no governed
  credential path.
- **postgresql / mysql / redis / mongodb / kafka / elasticsearch** — REAL Connect with
  real ping (`postgres.go:127`, `mysql.go:121`, `redis.go:108`, `mongodb.go:78`,
  `kafka.go:137`, `elasticsearch.go:83`), all in newAdapterForType:155-166, but **none**
  in the credential seam — credentials read inline from config; promotion cannot supply
  them and registration does not block `password`/`api_key` fields.

### PRESENT-BUT-UNWIRED (adapter code exists; never constructed on a reachable path)

`resolveAdapter` returns `ErrAdapterNotFound` for these because no construction site
ever builds + registers them (absent from both newAdapterForType and
connectSourcesDefault). Their refresh/access/tool/API code type-asserts an adapter that
is never created.

- **oci_registry** — adapter real (`oci.go:68,83`), refresh/access/API routes present,
  never constructed.
- **dockerhub** — shares the OCI adapter; never constructed.
- **artifactory** — adapter real (`artifactory.go:62,77`); never constructed.
- **ecr** — adapter real (`ecr.go:70,85`); never constructed.
- **cloudwatch** — type constant only (`internal/constants/constants.go:18`); **no
  adapter package exists**; never constructed.
- **azuremonitor** — type constant only (`internal/constants/constants.go:19`); **no
  adapter package exists**; never constructed.

---

## 3. Core capabilities

### SHIPPED

- **Interactive agentic loop** (`internal/agentloop`) — constructed per request
  (`agentloop.NewAgent`, `internal/api/tasks.go:403`), reachable via `POST /api/v1/tasks`
  and `POST /api/v1/tasks/stream` (`tasks.go:901,904`). This is the chat/agent surface.
- **Background refresh loop (Core Agent)** — constructed at boot
  (`deps.newCoreAgent` → `coreagent.New`, `cmd/joe/server.go:661`), started
  (`coreAgent.Start`, `server.go:746`). Runs per-component-type refreshers that apply
  graph deltas under the `agent:core` guarded read accessor (`server.go:719-727`). See
  PARTIAL for the autonomy caveat.
- **Chat sessions** — first-class owned/shareable/incident-linked sessions. Repo wired
  `server.go:380`, set on services `server.go:482`. CRUD at `/api/v1/sessions`
  (`internal/api/webui.go`, registered `server.go:140`). Archive provider wired at boot
  (`sessionarchive.New(...)`, `server.go:498`); sweeper started at boot
  (`sessionsweeper`, `server.go:777,786`). Live model is `internal/sessionmodel`.
- **Incident regime** — CLI `joe incident` (§1) + HTTP `/api/v1/regime*`
  (`internal/api/regime.go`, registered when `SessionModel != nil`). Gates the executor:
  the captaingate wrapper reads the regime (`internal/captaingate/captaingate.go:224`)
  and refuses non-captain mutations in incident mode (`captaingate.go:228`); installed on
  both agentic paths (Core Agent `server.go:685`, user-task loop `tasks.go:334`).
- **Observation mode & write floor** — floor resolved once at boot
  (`safety.ResolveWriteFloor(dbPanicked, observationMode)`, `cmd/joe/server.go:416`),
  where `observationMode = JOE_MODE == "observation"` (`server.go:415`). Enforced first
  among denials in the executor: `if e.floor.Up() && class == ActionMutate` →
  `WriteFloorError` (`internal/tools/executor.go:215`). Runtime-immutable; recovery is
  restart.
- **RBAC, zones, read posture, promotion** — transport policy engine
  `rbac.NewPolicyEngineWithGovernance(...)` (`server.go:856`); refresh engine
  `NewPolicyEngineWithPromote(...)` with **no** posture seam (`server.go:722`) — the two
  axes are separated at construction. Boot refuses to start without identity config
  (`requireIdentityConfigured`, `server.go:829`). Admin REST: zones, component-zones,
  policies, principals, read-promotions, read-posture (`internal/api/admin.go`,
  registered `server.go:129`). Launch default read posture is `team_flat`.
- **Skills (in-server)** — registry loaded at boot (`skills.LoadDir(~/.joe/skills)`,
  `server.go:512`); hot-reload watcher started unless disabled (`server.go:538-552`).
- **MCP** — `joe mcp`; 8 tools registered in `internal/mcp/server.go:31-45`:
  `joe_graph_query`, `joe_graph_related`, `joe_k8s`, `joe_metrics`, `joe_logs`,
  `joe_traces`, `joe_alerts`, `joe_knowledge_search` (defs `internal/mcp/tools.go:10-82`).
- **Slack** — `joe slack`; `jslack.NewAgent` + `jslack.NewServer` over Socket Mode
  (`cmd/joe/main.go:273-274`).
- **Knowledge graph** — SQLite-backed (`internal/graph/sqlite.go:30`), wired into
  `services.Graph` (`internal/core/services.go:45`), shares `joe.db`. Query routes
  `GET /api/v1/graph/query|related|summary` (`internal/api/server.go:178-180`).
- **Doc-drift / proposal / publish** — services constructed in
  `internal/core/services.go:192-204` (`proposals.NewService`, `drift.New`) and
  `services.DocDrafter` (`drafts.New`, `server.go:645`). Two reach paths: HTTP drift +
  proposals routes (`internal/api/drift.go:13-14`, `internal/api/proposals.go:15-19`),
  and the full drift→draft→publish LLM-tool chain (`detect_doc_drift`,
  `generate_doc_draft`, `publish_doc_update` registered `internal/tools/default.go:136-138`),
  where `publish_doc_update` performs the live external write
  (`internal/tools/core/publish_doc_update.go:51` → `internal/api/publish.go:19`). See
  PARTIAL for the approve-vs-publish asymmetry.

### PARTIAL

- **Autonomy "Needs-Human / clarification queue" from the autonomous refresh** — the
  top-level `refresh()` is an explicit MVP stub: the deterministic graph-delta apply
  ships, but the "queue ambiguous findings for clarification" branch is **not built**
  (`internal/coreagent/refresh.go:168-192`). Clarifications as a subsystem exist and are
  served (`/api/v1/clarifications`) but are populated by onboarding/discovery, not the
  periodic loop.
- **Refresh interval is hardcoded 5 minutes** (`internal/coreagent/refresh.go:88,145`),
  **not** config-driven; `cfg.Refresh.IntervalMinutes` is logged at boot
  (`server.go:249`) but does not change the loop cadence. Docs must not present the
  refresh interval as configurable.
- **Doc-proposal HTTP approve does not publish** — `POST
  /api/v1/knowledge/proposals/{id}/approve` only flips status
  (`internal/api/proposals.go:110`); the external publish happens **only** via the
  `publish_doc_update` agent tool / in-proc `PublishProposal`. There is no standalone
  REST `/publish` route.

### PRESENT-BUT-UNWIRED

- **`internal/session` (singular package)** — dead code; **zero non-test importers**
  (verified). The live session model is `internal/sessionmodel`. Do not document it.

---

## 4. HTTP API surface

Routes mount on one `http.NewServeMux()` (`cmd/joe/server.go:799`) via
`apiServer.RegisterRoutes(mux)` (`server.go:803` → dispatch hub
`internal/api/server.go:110`). The OIDC triplet is registered **only when OIDC is
configured** (`server.go:890-902`). All paths use prefix `/api/v1`.

### SHIPPED — build / identity / status

| Method Path | Returns | Registration |
|---|---|---|
| `GET /api/v1/status` | `{status:"ok", version, time}` — version only, no ui_digest | `internal/api/server.go:166` |
| `GET /api/v1/version` | full `buildinfo.Info` `{version, commit, build_time, ui_digest}` | server.go:170 |
| `GET /api/v1/mutate-status` | `{can_mutate, reason}`, reason ∈ full/observation/safe_mode | `internal/api/mutatestatus.go:44` |
| `POST /api/v1/panic` | triggers shutdown (process exits 2); `{acknowledged, message}` | `internal/api/panic.go:38` |
| `GET /api/v1/panic/status` | `{safe_mode, triggered_at?, trigger_source?, trigger_reason?}` | panic.go:39 |

There is **no `POST /api/v1/unlock`** route and **no `POST /api/v1/chat`** route
(verified absent). Recovery from panic is the `joe unlock` CLI + restart; chat is the
task endpoints.

### SHIPPED — observe (category-based), all 5 mounted

`POST /api/v1/observe/{metrics,logs,traces,alerts,k8s}`, body `{service, question}`,
registered `internal/api/observe.go:17-21`.

### SHIPPED — components

| Method Path | Does | Reg |
|---|---|---|
| `GET /api/v1/component-types` | list the type enum | `internal/api/server.go:189` |
| `GET /api/v1/components` | list | server.go:190 |
| `POST /api/v1/components` | register | server.go:191 |
| `GET /api/v1/components/{id}` | get | server.go:192 |
| `DELETE /api/v1/components/{id}` | delete | server.go:193 |
| `GET /api/v1/components/{id}/promotion-requirements` | promotion contract (admin) | server.go:199 |
| `GET /api/v1/components/{id}/promotion-candidates` | candidate set (admin) | server.go:206 |
| `POST /api/v1/components/{id}/promote` | read-only→armed promotion (admin) | server.go:211 |
| `POST /api/v1/components/{id}/test` | connectivity test → `{ok, message}` | `internal/api/webui.go:944` |

### SHIPPED — sessions / chat / tasks

`GET/POST /api/v1/sessions`, `/sessions/trash`, `/sessions/{id}` (GET/PATCH/DELETE),
`/sessions/{id}/messages`, `/sessions/{id}/restore`, `/sessions/{id}/link-incident`
(`internal/api/webui.go:928-938`). Agentic turns: `POST /api/v1/tasks` (non-streaming)
and `POST /api/v1/tasks/stream` (SSE) (`internal/api/tasks.go:901,904`).

### SHIPPED — admin surface (`internal/api/admin.go`, all admin-gated + audited)

Zones (`GET/POST/PATCH/DELETE /api/v1/admin/zones`, admin.go:103-106); component-zones
(admin.go:108-110); policies (admin.go:112-115); unassigned (admin.go:117); admins
(admin.go:119-121); principals (admin.go:123-125); read-promotions
(`GET/POST /api/v1/admin/read-promotions`, admin.go:131-132); **read-posture**
(`GET/POST /api/v1/admin/read-posture`, admin.go:137-138); credential-status + probe
(admin.go:145-147). Admin session governance — list/get/messages/purge/archive/
restore-archive/trash/retention-policy (`internal/api/adminsessions.go:74-89`).

### SHIPPED — incident / regime / captain / runs

Regime: `GET /api/v1/regime`, `POST /api/v1/regime/declare`, `POST /api/v1/regime/resolve`,
`POST /api/v1/sessions/{id}/promote-incident`, `POST /api/v1/sessions/{id}/incident-state`
(`internal/api/regime.go:54-92`, registered when `SessionModel != nil`). Captain state
machine (`internal/api/captain.go:55-59`); runs (`internal/api/runs.go:62-81`); findings
(`internal/api/findings.go:31-32`); warnings (`internal/api/warnings.go:26`).

### SHIPPED — auth / current-user / LLM control plane / graph / knowledge / control

- Auth: `GET /api/v1/auth/login`, `/auth/callback`, `POST /auth/logout`
  (`internal/auth/handlers.go:102-104`, **OIDC-gated**); `GET /api/v1/auth/config`
  (public `oidc_enabled`, `internal/api/authconfig.go:40`); `GET /api/v1/me`
  (`internal/api/currentuser.go:135`).
- LLM control plane: `/api/v1/models` (+`/models/current`), `/api/v1/llm/settings*`,
  `/api/v1/llm/usage/*`, `/api/v1/llm/providers` (`internal/api/models.go`,
  `llmsettings.go`, `llmusageapi.go`, `llmproviders.go`).
- Graph (web UI): `GET /api/v1/graph`, `/graph/node/{id}`, `/graph/node/{id}/related`
  (`webui.go:921-923`); alerts `GET /api/v1/alerts` (webui.go:941).
- Knowledge: entries CRUD + `POST /api/v1/knowledge/search`, sources (+`/sync`),
  proposals (+`/approve`,`/reject`), drift (`internal/api/knowledge.go`, `proposals.go`,
  `drift.go`).
- Control: `POST /api/v1/onboarding` (server.go:269), `POST /api/v1/refresh`
  (server.go:270), clarifications (server.go:262-264), skills (`skills.go:35-38`).
- Per-componentID observability/adapter routes (`/api/v1/{adapter}/{componentID}/...`)
  are where RBAC enforcement middleware fires; they exist for the alerting/datastore/
  networking/security/registry families (registry routes mount but resolve to no adapter
  — see §2).

### PRESENT-BUT-UNWIRED

None at the route level — every `register*Routes` function is invoked from
`RegisterRoutes`. The only conditional surface is the OIDC auth triplet (registered only
when an issuer is configured). Two endpoints commonly assumed to exist do **not**:
`POST /api/v1/unlock` and `POST /api/v1/chat`.

---

## 5. Configuration surface

Config is YAML (`yaml:` tags only), parsed via `gopkg.in/yaml.v3`
(`internal/config/config.go:488`). Defaults applied in `defaultConfig()`
(`config.go:427-477`); a field absent there defaults to its Go zero value.

### SHIPPED config keys (selected defaults, all `file:line`)

- `llm.current` = `"claude-sonnet"` (config.go:430); `llm.available` seeded with one
  entry `claude-sonnet → {provider: claude, model: claude-sonnet-4-20250514}`
  (config.go:431-433); per-model `base_url` (`config.go:271`, **required** for
  `openai-compat`, ignored otherwise). `llm.currency` = `"USD"` (config.go:434).
- `server.address` = `"localhost:7777"` (`constants.go:5` → `config.go:437`);
  `server.service_accounts` `[]{name,key}` (config.go:160); `server.tls_cert_file` /
  `tls_key_file` (server TLS gated on both via `TLSConfigured()`, config.go:183);
  `server.tls_enabled` is a **client-side** connect flag, not a server TLS enabler;
  `server.rate_limit_rps` = 0 (disabled); `server.session_archive_dir` ""→
  `~/.joe/session-archive`.
- `refresh.interval_minutes` = 5 (config.go:440) — **logged but not applied to the
  refresh loop**, which is hardcoded 5m (see §3 PARTIAL); `refresh.llm_budget.*`
  defaults 100 / 10 / 30 (config.go:442-444).
- `notifications.desktop` (off; threshold `medium`), `notifications.slack` (off;
  threshold `high`), `notifications.quiet_hours` (off; 22:00–08:00 Local)
  (config.go:449-461).
- `logging.level` = `"info"`, `logging.file` = "" (config.go:464-465).
- `knowledge.semantic_top_k` = 5, `derived_min_confidence` = 0.0, `sync_enabled` = false
  (config.go:468-470); `embedding_model` ""→ falls back to `llm.current`.
- `database.driver` ""→ `sqlite`, `database.dsn` ""→ default SQLite path
  (config.go:104-112).
- `skills.trusted_sources` (empty = allowlist off), `skills.hot_reload_disabled` = false
  (config.go:42-47).
- `auth.oidc.{issuer,client_id,client_secret,redirect_url}` (all "" = OIDC disabled),
  `auth.admin_email` "" (bootstrap disabled), `auth.session_ttl` = 12h (config.go:473),
  `auth.post_login_redirect` = `"/"` (config.go:474).

### SHIPPED environment variables

- Config overrides (`applyEnvOverrides`, config.go:515-574): `JOE_LLM_PROVIDER`,
  `JOE_LLM_MODEL`, `JOE_LOG_LEVEL`, `JOE_SERVER_ADDRESS`, `JOE_API_KEY` (sets the
  reserved `svc:server` service-account key), `JOE_DATABASE_DSN`.
- Boot/process: `JOE_CONFIG` (config path, below `--config`, `server.go:200`); `JOE_MODE`
  (`observation` raises the write floor at boot, `server.go:415`).
- LLM API keys (`internal/env/keys.go`): `ANTHROPIC_API_KEY` (required for `claude`,
  fatal if claude selected without it), `GEMINI_API_KEY` / `GOOGLE_API_KEY` (either for
  `gemini`), `OPENAI_API_KEY` (**optional** for `openai-compat` — keyless local endpoints
  are valid; validation gates on `base_url`, not this key, `validation.go:106-112`).
- `joe mcp`: `JOE_SERVER` (default `http://localhost:7777`), `JOE_API_KEY` (optional).
- `joe slack`: `SLACK_BOT_TOKEN` (required), `SLACK_APP_TOKEN` (required), `JOE_SERVER`,
  `JOE_API_KEY`.
- OpenTelemetry (`internal/observability/otel.go:50-56`): `OTEL_ENABLED` (true),
  `OTEL_TRACES_ENABLED` (true), `OTEL_TRACES_EXPORTER` (**`none`** — see §7),
  `OTEL_EXPORTER_OTLP_ENDPOINT` (`localhost:4317`), `OTEL_METRICS_ENABLED` (true),
  `OTEL_METRICS_EXPORTER` (`prometheus`), `OTEL_METRICS_PORT` (9090).
- Static credential references: operator-defined `JOE_<SEGMENT>_<LABEL>` for the 15
  KindStatic component types (`internal/credential/references.go:41-57`), e.g.
  `JOE_GITHUB_PROD`.

### Provider allow-list (authoritative pair)

Exactly three providers are accepted: `claude`, `gemini`, `openai-compat`. Enforced by
the validation switch (`internal/config/validation.go:95-115`; explicit
`supportedProviders` list at validation.go:161) and the factory switch
(`internal/llmfactory/factory.go:27-36`, gemini is the default arm), sharing the
constants at `internal/config/constants.go:33-35`.

### PARTIAL (parsed/validated but effect not wired)

- `llm.usd_to_configured_rate` and the non-USD `llm.currency` conversion path are
  validated at load (`ValidateCostCurrency`, config.go:409; validation.go:136-155) but
  **no recorder/cost-gate consumes them** (in-code comment: "no caller multiplies by this
  value yet", config.go:251-261). Do not document non-USD cost reporting as working.
- `server.rate_limit_burst` documents a "default 10" in a comment, but `defaultConfig()`
  sets no value (Go zero `0`); whether 10 is applied at the limiter is UNVERIFIED.

---

## 6. Install, build, and auth posture

### SHIPPED — build / install

- **Build-from-source only; nothing published yet.** The release pipeline is
  armed to publish on a `v`-prefixed tag push (D-0091, `.goreleaser.yaml`'s
  `release` block plus the tag-triggered `.github/workflows/release.yml`); no
  tag has been pushed, so no release exists yet. See
  `docs/backlog/release-pipeline.md`.
- **`make build`** runs `build-ui` (`npm ci && npm run build`, copies `ui/dist` into the
  `//go:embed` staging dir `internal/webui/dist`, `Makefile:51-55`) then compiles
  `./cmd/joe` with ldflags. The `-X` injection targets are exactly
  `github.com/jaimegago/joe/internal/buildinfo` `.Version`/`.Commit`/`.BuildTime`
  (`Makefile:13,17-19`), mirrored in goreleaser (`.goreleaser.yaml:39-41`). `ui_digest`
  is **not** injected — computed at boot from the embedded UI bytes
  (`cmd/joe/server.go:309-318`). Plain `go build ./...` reports unset `dev`/`none`
  defaults.
- **CI** (`.github/workflows/tests.yml`) runs unit, integration, e2e (`make build` +
  `make test-e2e`), lint, and a `goreleaser build --snapshot --clean` job that stages
  the real UI via the same `before.hooks` the release path uses and verifies the
  built snapshot's `ui_digest` against the staged UI — proven but never publishes.
  A separate tag-triggered `.github/workflows/release.yml` is the only path that
  runs `goreleaser release --clean` and publishes.
- **Running Joe:** bare `joe` (or `joe --config`) boots the daemon; listen address from
  `cfg.Server.Address` (default `localhost:7777`). Config path precedence: `--config`
  flag → `JOE_CONFIG` env → `~/.joe/config.yaml` (`cmd/joe/server.go:188-217`); a missing
  config file is non-fatal (hardcoded defaults).

### SHIPPED — human auth (OIDC)

Real authorization-code + PKCE flow, library-backed (`github.com/coreos/go-oidc/v3` +
`golang.org/x/oauth2`, `internal/auth/oidc.go:9-130`): real discovery, S256 PKCE
challenge + nonce, token exchange, JWKS verification, `email_verified` enforcement.
Endpoints `GET /auth/login`, `GET /auth/callback`, `POST /auth/logout`
(`internal/auth/handlers.go:101-297`). Wired into the live mux + edge-auth chain
(`auth.NewHandlers` + `RegisterRoutes`, `cmd/joe/server.go:890-904`; `auth.EdgeAuth`,
server.go:923-929). Active iff **all three** of issuer/client_id/redirect_url are set
(`config.go:99-101`); when unset the auth endpoints are simply not registered. Discovery
is lazy, so an IdP outage is not a boot failure — only new logins fail (503
`oidc_unavailable`).

### SHIPPED — non-human auth (service-account bearer)

`Authorization: Bearer <key>` parsed in `bearerToken()`
(`internal/auth/middleware.go:225-234`); resolved to a `svc:<name>` principal via an
exact-match map built from `cfg.Server.ServiceAccounts`
(`internal/auth/serviceaccount.go:21-69`; duplicate/empty keys are fatal at boot).
`JOE_API_KEY` folds into the reserved `"server"` account → principal `svc:server`
(`config.go:129-137,562-588`). Enforced by `auth.EdgeAuth` (session cookie first, then
bearer; unknown key on a protected path → 401; `middleware.go:162-187`), with
`rbac.EnforcementMiddleware` downstream (`server.go:934`). **Boot refuses to start
without at least one service account OR a complete OIDC issuer**
(`requireIdentityConfigured`, `server.go:829`); there is no auth-disabled runtime mode.

### SHIPPED — admin bootstrap

`auth.admin_email` is the sole bootstrap path (empty disables it, `config.go:65-69`). In
the OIDC callback, a verified email matching `admin_email` triggers
`provisioner.GrantAdmin` (`internal/auth/handlers.go:243-256` →
`internal/auth/provision.go:76-100`), idempotent and audited once on first escalation.
Bootstrap is **OIDC-only**: a service-account-only install has no self-escalation path;
further admins come through `POST /api/v1/admin/admins` (which itself requires an
existing admin).

---

## 7. Observability surface

### SHIPPED

- **OTel → Prometheus metrics pipeline** is initialized at boot and the **`/metrics`
  endpoint is served on port `9090`** (default `OTEL_METRICS_PORT`), not on the API's
  `:7777` (`internal/observability/otel.go:50-56`).
- **`joe_build_info` gauge** (constant 1, build identity in labels incl. `ui_digest`)
  registered in the metrics-setup layer beside the business gauges.
- Business gauges + domain recorder metric families: tools, adapters, graph, db,
  coreagent refresh/discovery, useragent, session — all wired into the running binary.
- **HTTP server request metrics** are emitted via middleware.

### PARTIAL

- **HTTP request tracing** is instrumented in middleware, but the default
  `OTEL_TRACES_EXPORTER=none` (otel.go:52) means spans are **not exported** unless an
  operator opts in. Docs should present tracing as opt-in, not on-by-default.

### PRESENT-BUT-UNWIRED

- **Cache metrics** — `Metrics.RecordCacheLookup` (`internal/observability/metrics.go:380`)
  is defined but has **no non-test caller** (verified). No cache metrics are emitted.
- **Redundant LLM instrumentation** — both `observability.LLMMiddleware` and
  `llm.InstrumentedAdapter` exist but **neither is on the live adapter path**, so the
  running binary emits **no LLM-level metrics or spans**. Do not document per-LLM-call
  metrics as a shipped feature.

---

## 8. Cross-cutting facts for doc authors

- **Three surfaces, not one process:** the server daemon (bare `joe`), plus `joe mcp`
  and `joe slack`, which are **clients of a running daemon over HTTP** — not part of
  server boot.
- **Two divergent adapter construction sites** (§2): datadog/splunk/dynatrace/newrelic
  are boot-only (no post-boot connectivity test/re-register); registry + cloud-metrics
  types are in neither.
- **The refresh loop cadence is fixed at 5 minutes** and not config-driven (§3/§5).
- **`/metrics` is on :9090, not :7777** (§7).
- **No `POST /api/v1/unlock` and no `POST /api/v1/chat`** (§4).

---

## 9. Do-Not-Document — every PARTIAL and PRESENT-BUT-UNWIRED finding

The public docs must not describe any of these as a working feature.

### PARTIAL (wired but with a user-affecting gap)

| Item | Gap | Proof |
|---|---|---|
| azure adapter | `Connect` is a skeleton — never contacts Azure | `adapters/azure/azure.go:90-110` |
| helm adapter | skeleton `Connect`; no credential wiring | `helm.go:119` |
| nginx-ingress adapter | skeleton `Connect`; no credential wiring | `nginx.go:125` |
| git adapter | REAL but no governed credential path (inline only) | `git.go:69`; `wiring.go:35` |
| aws adapter | REAL but SDK-chain creds, not the Joe credential seam | `aws.go:185` |
| datadog adapter | REAL but boot-only (absent from `newAdapterForType`); inline creds | server.go:1133; components.go:131-178 |
| postgres/mysql/redis/mongodb/kafka/elasticsearch | REAL but no governed credential path (inline only) | components.go:155-166; wiring.go:43 |
| autonomous "Needs-Human" clarification queue | refresh stub never queues clarifications | `coreagent/refresh.go:168-192` |
| configurable refresh interval | hardcoded 5m; config value logged but unused | `coreagent/refresh.go:88,145`; server.go:249 |
| doc-proposal HTTP approve | does not publish to external target | `internal/api/proposals.go:110` |
| non-USD cost reporting | rate parsed/validated but no consumer | config.go:251-261; validation.go:136-155 |
| HTTP tracing | instrumented but exporter defaults to `none` | otel.go:52 |
| `joe incident list` | honest stub; no history query | `cmd/joe/incident.go:49-51` |

### PRESENT-BUT-UNWIRED (exists in tree, constructed on no reachable path)

| Item | Why it's not shipped | Proof |
|---|---|---|
| oci_registry adapter | never constructed (absent from both sites) | components.go:131-178; server.go:1056 |
| dockerhub adapter | shares OCI; never constructed | components.go:131-178 |
| artifactory adapter | never constructed | components.go:131-178 |
| ecr adapter | never constructed | components.go:131-178 |
| cloudwatch | type constant only; **no adapter package** | `internal/constants/constants.go:18` |
| azuremonitor | type constant only; **no adapter package** | `internal/constants/constants.go:19` |
| `internal/session` package | dead code; zero non-test importers | (grep, verified) |
| cache metrics | `RecordCacheLookup` has no non-test caller | `observability/metrics.go:380` |
| LLM instrumentation | neither `LLMMiddleware` nor `InstrumentedAdapter` on the live path | (verified) |
| `POST /api/v1/unlock` | route does not exist | (grep, verified) |
| `POST /api/v1/chat` | route does not exist | (grep, verified) |

### Most consequential (most likely to be mistaken for shipped)

1. **Container-registry components (oci_registry / dockerhub / artifactory / ecr)** —
   full adapter code, refresh/access/API routes, and component-type enum entries exist,
   so a doc author skimming the tree would assume they work. They are never constructed;
   registering one yields `ErrAdapterNotFound`.
2. **cloudwatch / azuremonitor** — present in the component-type enum with **no adapter
   package at all**. The enum makes them look selectable.
3. **azure** — boot-wired and in the runtime factory, but `Connect` is a pure stub. The
   most dangerous "looks shipped, isn't."
4. **LLM-call metrics** — two instrumentation implementations exist but neither is wired;
   the running binary emits no per-LLM-call telemetry.
