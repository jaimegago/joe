---
title: Configuration
weight: 50
description: The YAML config and environment surface.
---

# Configuration

This is the reference for Joe's configuration file and environment surface: every
key, every environment variable, and the default each one resolves to. For a guided
first run see the [Quickstart](../quickstart/); for the build-and-run procedure see
[Install and Build](../install-and-build/); for *why* Joe is configured the way it is
see [Concepts](../concepts/).

Joe needs **no configuration file** to run. It boots on built-in defaults; a config
file becomes useful when you want to pin a model, run more than one model, enable OIDC
login, or tune the server. The one thing Joe will not boot without is an identity
configuration — at least one service account or a complete OIDC issuer (see
[Install and Build](../install-and-build/)).

## Config file location

Joe reads its config file from the first of these that is set:

1. the `--config <path>` flag,
2. the `JOE_CONFIG` environment variable,
3. `~/.joe/config.yaml` (the default).

A missing config file is not an error. The file is YAML; provider API keys are never
stored in it — they live only in the environment that runs the daemon.

## Config file reference

The block below shows every key with its real default. Keys whose value is currently
inert are marked and collected under [Inert and reserved keys](#inert-and-reserved-keys).

```yaml
llm:
  current: claude-sonnet              # key into llm.available — the active model
  available:                          # default seeds exactly this one entry
    claude-sonnet:
      provider: claude                # one of: claude | gemini | openai-compat
      model: claude-sonnet-4-20250514
    gemini-flash:
      provider: gemini
      model: gemini-2.5-flash
    local-llama:
      provider: openai-compat         # generic OpenAI Chat Completions endpoint
      model: llama3                   # the model name the endpoint expects
      base_url: http://localhost:11434/v1  # REQUIRED for openai-compat; ignored otherwise
  currency: USD                       # USD | EUR (EUR conversion is inert — see below)
  usd_to_configured_rate: 0           # reserved; validated but unused (see below)

server:
  address: "localhost:7777"           # daemon listen address
  service_accounts:                   # machine identities; each key → principal svc:<name>
    - name: server                    # the "server" account is the key the joe CLI presents
      key: "a-long-random-secret"
    - name: ci
      key: "another-secret"
  tls_cert_file: ""                   # server TLS is enabled only when BOTH cert and key are set
  tls_key_file: ""
  tls_enabled: false                  # CLIENT-side flag: connect over HTTPS (not a server TLS switch)
  rate_limit_rps: 0                   # per-IP requests/sec (0 = rate limiting disabled)
  rate_limit_burst: 0                 # per-IP burst (no default; see below)
  session_archive_dir: ""             # "" → ~/.joe/session-archive

auth:                                 # human login (OIDC); optional
  admin_email: ""                     # bootstrap admin identity ("" = bootstrap disabled)
  session_ttl: 12h
  post_login_redirect: "/"
  oidc:                               # OIDC active only when issuer + client_id + redirect_url are all set
    issuer: ""
    client_id: ""
    client_secret: ""
    redirect_url: ""

refresh:                              # NOTE: this whole block is currently inert (see below)
  interval_minutes: 5
  llm_budget:
    max_calls_per_hour: 100
    batch_threshold: 10
    batch_timeout_sec: 30

logging:
  level: info                         # debug | info | warn | error
  file: ""                            # log file path ("" = stderr only)

database:
  driver: ""                          # "" → sqlite (the only functional driver today); "pgx" is present but not yet operational — see note below
  dsn: ""                             # "" → joe.db in Joe's .joe directory; an explicit value is an absolute path, and ~ is NOT expanded
  encryption_key_path: ""             # "" → encryption.key in Joe's .joe directory; same rules as dsn — move it with the database

skills:
  trusted_sources: []                 # repos auto-trusted for skill install ([] = allowlist off)
  hot_reload_disabled: false

web_search:                           # optional; web search is inert until a provider is set
  provider: ""                        # "" = disabled; the only supported value today is "searxng"
  base_url: ""                        # REQUIRED when provider is set (the search endpoint or a gateway)
  api_key: ""                         # optional; SearXNG normally needs none
```

## Key reference

### `llm`

| Key | Default | Effect |
| --- | --- | --- |
| `llm.current` | `claude-sonnet` | Key into `llm.available` selecting the active model. |
| `llm.available` | one entry: `claude-sonnet → {provider: claude, model: claude-sonnet-4-20250514}` | Map of named model configurations. |
| `llm.available.<name>.provider` | — | One of `claude`, `gemini`, `openai-compat`. |
| `llm.available.<name>.model` | — | Model name the provider expects. |
| `llm.available.<name>.base_url` | — | **Required** for `openai-compat`; ignored by native providers. |
| `llm.currency` | `USD` | Allowed values `USD`, `EUR`. EUR conversion is inert — see [below](#inert-and-reserved-keys). |
| `llm.usd_to_configured_rate` | unset | Validated (must be `> 0` when `currency` is not `USD`) but has no consumer — see [below](#inert-and-reserved-keys). |

### `server`

| Key | Default | Effect |
| --- | --- | --- |
| `server.address` | `localhost:7777` | Daemon HTTP listen address (API and embedded web UI). |
| `server.service_accounts` | empty | List of `{name, key}`; each key maps to principal `svc:<name>`. The account named `server` is the key the `joe` CLI and subcommands present. Duplicate or empty keys are a boot failure. |
| `server.tls_cert_file` / `server.tls_key_file` | `""` | Server TLS is enabled only when **both** are set. |
| `server.tls_enabled` | `false` | A **client-side** flag telling subcommands to connect over HTTPS — not a server TLS switch. |
| `server.rate_limit_rps` | `0` | Per-IP sustained requests/sec. `0` (the default) disables rate limiting entirely. |
| `server.rate_limit_burst` | unset | Per-IP burst size. No default is applied — see [below](#inert-and-reserved-keys). |
| `server.session_archive_dir` | `~/.joe/session-archive` | Where the session archive provider writes artifacts. |

### `auth`

| Key | Default | Effect |
| --- | --- | --- |
| `auth.admin_email` | `""` | Verified email bootstrapped to admin on first OIDC login. `""` disables bootstrap. Bootstrap is OIDC-only. |
| `auth.session_ttl` | `12h` | Human session lifetime. |
| `auth.post_login_redirect` | `/` | Path to redirect to after a successful login. |
| `auth.oidc.issuer` / `client_id` / `client_secret` / `redirect_url` | `""` | OIDC login is active only when `issuer`, `client_id`, and `redirect_url` are all set; the login/callback/logout endpoints are registered only then. |

### `logging`

| Key | Default | Effect |
| --- | --- | --- |
| `logging.level` | `info` | `debug` / `info` / `warn` / `error`. |
| `logging.file` | `""` | Log file path; empty logs to stderr only. |

### `database`

| Key | Default | Effect |
| --- | --- | --- |
| `database.driver` | `sqlite` | **SQLite is the supported database.** `sqlite` (the default) is the only functional value. A `pgx` (PostgreSQL) value is present in the configuration surface but is **not yet operational** — see the note below. |
| `database.dsn` | `joe.db` in Joe's `.joe` directory, resolved under the home directory of the account running `joe` (SQLite) | Database path or DSN. |
| `database.encryption_key_path` | `encryption.key` in Joe's `.joe` directory, resolved the same way | Where the key that encrypts component configuration at rest is read from, and written to on a first run. |

**The defaults above are resolved locations, not literals to copy.** An explicit value of
either must be an **absolute path** (or one relative to the working directory): Joe does
**not** expand a leading `~` in either value, so `~/.joe/joe.db` is taken literally and
creates a directory named `~` in the working directory. The two behave identically by
design.

**Set them together.** The database and the key are one unit of durable state, and
relocating only the database strands the key under the home directory. Joe will not start
if the key is missing or does not match the database it finds, so a half-relocated install
fails at start-up rather than running broken — see [Persistence and
backup](../operations/persistence-and-backup/).

**PostgreSQL is not yet functional.** The `pgx` driver value exists in the configuration surface — the store opens the configured driver, the repositories are dialect-aware, and the migration runner has a PostgreSQL branch — but the embedded migration set is written in SQLite dialect only. Setting `database.driver: "pgx"` today fails at startup during the migration step, before the server begins serving, because those migrations use SQLite-only constructs (`AUTOINCREMENT` and SQLite-specific append-only trigger DDL) that PostgreSQL rejects. Use the default SQLite backend. PostgreSQL support is planned.

### `skills`

| Key | Default | Effect |
| --- | --- | --- |
| `skills.trusted_sources` | `[]` | Repositories auto-trusted for skill install; empty means the allowlist is off. |
| `skills.hot_reload_disabled` | `false` | Disable the skills hot-reload watcher. |

### `web_search`

Web search is a global, boot-resolved capability shared by every chat session. What it
does and how it is classified as a Read on Joe's action surface is explained in
[The action model](../concepts/action-model/); the keys are below.

| Key | Default | Effect |
| --- | --- | --- |
| `web_search.provider` | `""` | Search backend. `""` (the default) leaves web search **disabled**. There is no default provider. The only supported value today is `searxng`. |
| `web_search.base_url` | `""` | **Required** when a provider is set. The search endpoint's base URL. May point either at the search provider directly or at an operator-run egress gateway that fronts it — transparent to Joe. |
| `web_search.api_key` | `""` | Optional key. SearXNG normally needs none; when set it is sent as a bearer token. |

When no provider is configured the `web_search` tool is **still offered to Joe** but every
call returns a `no search backend configured` error — it is never silently hidden. A
misconfigured backend (an unknown provider, or `searxng` without a `base_url`) fails the
daemon at boot. Changing the backend requires a **restart**; there is no runtime switch.

**SearXNG (recommended self-host backend).** [SearXNG](https://docs.searxng.org/) is a
self-hostable, keyless metasearch engine. Point `base_url` at your instance and **enable
JSON output** on it — SearXNG serves JSON only when `json` is listed under `search.formats`
in its `settings.yml` (the default configuration omits it). Joe queries the instance's
`/search` endpoint with `format=json` and reads each result's title, URL, and snippet.

```yaml
web_search:
  provider: searxng
  base_url: https://searxng.internal.example.com
```

**Keyed hosted providers** (e.g. Tavily, Brave) are a designed-for extension behind the
same provider abstraction but are **not implemented yet**. When added, their keys will ride
plain config/env like the LLM provider keys — not the component credential-reference model.

## Environment variables

### Config overrides

| Variable | Effect |
| --- | --- |
| `JOE_LLM_PROVIDER` | Overrides the active model's provider. |
| `JOE_LLM_MODEL` | Overrides the active model's model name. |
| `JOE_LOG_LEVEL` | Overrides `logging.level`. |
| `JOE_SERVER_ADDRESS` | Overrides `server.address`. |
| `JOE_API_KEY` | Sets the key of the reserved `server` service account (principal `svc:server`), creating it if absent. |
| `JOE_DATABASE_DSN` | Overrides `database.dsn`. Same rule as the key: an absolute path, with no `~` expansion. |
| `JOE_WEBSEARCH_PROVIDER` | Overrides `web_search.provider`. |
| `JOE_WEBSEARCH_BASE_URL` | Overrides `web_search.base_url`. |
| `JOE_WEBSEARCH_API_KEY` | Overrides `web_search.api_key` (keep the key out of the config file). |

### Boot and process

| Variable | Effect |
| --- | --- |
| `JOE_CONFIG` | Config file path; below `--config`, above `~/.joe/config.yaml`. |
| `JOE_MODE` | Unset or `observation` raises the write floor at boot (read-only posture — the default). `full` is refused at boot (governed full-capabilities mode is not yet implemented); any unrecognized value is refused fail-closed. |

### Provider API keys

Provider keys live **only** in the environment that runs the daemon; client subcommands
never read them.

| Variable | Effect |
| --- | --- |
| `ANTHROPIC_API_KEY` | Provider key for `claude`. Required when a `claude` model is selected; boot is fatal without it. |
| `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Either supplies the provider key for `gemini`. |
| `OPENAI_API_KEY` | Provider key for `openai-compat`. **Optional** — keyless local endpoints leave it unset; validation gates on `base_url`, not this key. |

### Client subcommands

The `joe mcp` and `joe slack` subcommands are clients of a running daemon and read
their connection settings from the environment.

| Variable | Effect |
| --- | --- |
| `JOE_SERVER` | Base URL the client subcommands connect to (default `http://localhost:7777`). |
| `JOE_API_KEY` | Bearer token the client subcommands present. |
| `SLACK_BOT_TOKEN` | Slack bot token; required by `joe slack`. |
| `SLACK_APP_TOKEN` | Slack app-level token (Socket Mode); required by `joe slack`. |

### OpenTelemetry

| Variable | Default | Effect |
| --- | --- | --- |
| `OTEL_ENABLED` | `true` | Master switch for OpenTelemetry setup. |
| `OTEL_TRACES_ENABLED` | `true` | Enable tracing instrumentation. |
| `OTEL_TRACES_EXPORTER` | `none` | Trace exporter. The default `none` means spans are instrumented but not exported until you opt in. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | `localhost:4317` | OTLP exporter endpoint. |
| `OTEL_METRICS_ENABLED` | `true` | Enable metrics. |
| `OTEL_METRICS_EXPORTER` | `prometheus` | Metrics exporter. |
| `OTEL_METRICS_PORT` | `9090` | Port the Prometheus `/metrics` endpoint is served on — separate from the API port. |

### Static credential references

A static (env-var) credential reference for a component is an environment variable
named `JOE_<SEGMENT>_<LABEL>`, where `<SEGMENT>` is the component type's fixed prefix
segment and `<LABEL>` is your choice (e.g. `PROD`, `STAGING`). For example, a GitHub
component references `JOE_GITHUB_PROD`. The segments are defined for the static
credential types listed in [Components](../components/). The credential value lives
only in that environment variable; promotion stores the variable *name*, never the value.

## Provider configuration

Joe is AI-agnostic. The accepted providers are exactly:

- `claude` — native Anthropic adapter.
- `gemini` — native Google adapter.
- `openai-compat` — generic adapter for any server speaking the OpenAI Chat Completions
  protocol, selected together with a per-model `base_url`.

Any other `provider` value is rejected at config validation. A model using
`openai-compat` must set `base_url`; native providers ignore it.

## Inert and reserved keys

These keys are parsed (and some are validated) but have **no effect** in the running
binary today. They are documented here so they are not mistaken for operative knobs.

| Key / area | Status |
| --- | --- |
| `llm.currency` (non-USD) and `llm.usd_to_configured_rate` | Validated at load — `usd_to_configured_rate` must be `> 0` when `currency` is not `USD` — but no recorder or cost gate consumes the rate, so non-USD cost reporting does not function. `currency` defaults to `USD`; there is no default rate. |
| `server.rate_limit_burst` | No default is set, so the value is `0` unless you set it. Rate limiting is off by default (`rate_limit_rps` is `0`); when you enable it and leave the burst at or below `0`, the limiter clamps the burst to `1`. There is no "default 10". |
| `refresh.interval_minutes` | Parsed and logged at boot, but the background refresh cadence is fixed and does not read this value. The interval is not currently configurable. |
| `refresh.llm_budget.max_calls_per_hour` / `batch_threshold` / `batch_timeout_sec` | Parsed and defaulted (`100` / `10` / `30`) but no component consumes them. The refresh LLM budget is not currently enforced from config. |

## Where to go next

- A guided first run → [Quickstart](../quickstart/)
- Build and run the daemon, and the full authentication posture → [Install and Build](../install-and-build/)
- Connect Joe to your systems → [Components](../components/)
- Why Joe is governed by construction → [Concepts](../concepts/)
