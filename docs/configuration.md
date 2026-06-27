# Configuration

Joe runs with **zero config** when you set exactly one provider API key — it
auto-selects that provider and its default model. A config file becomes useful
when you want to pin a model, run both providers, enable authentication, or tune
the server.

Joe reads `~/.joe/config.yaml`. Override the path with `JOE_CONFIG` or
`--config <path>`. Copy the bundled starting point:

```bash
cp config.example.yaml ~/.joe/config.yaml
```

## `~/.joe/config.yaml` reference

```yaml
llm:
  current: claude-sonnet            # key from llm.available — the active model
  available:
    claude-sonnet:
      provider: claude              # native Anthropic / Google adapters, or
      model: claude-sonnet-4-20250514  # openai-compat for any OpenAI-compatible endpoint
    gemini-flash:
      provider: gemini
      model: gemini-2.5-flash
    local-llama:
      provider: openai-compat       # generic OpenAI Chat Completions endpoint
      model: llama3                 # the model name the endpoint expects
      base_url: http://localhost:11434/v1  # REQUIRED for openai-compat; ignored by native providers
  # API keys are NEVER stored in config — set them in joe's environment:
  #   ANTHROPIC_API_KEY               (Claude)
  #   GEMINI_API_KEY / GOOGLE_API_KEY (Gemini)
  #   OPENAI_API_KEY                  (openai-compat; OPTIONAL — omit for keyless local endpoints)
  # The selectable provider set is authoritative in internal/llmfactory/factory.go
  # and internal/config/validation.go.

server:
  address: "localhost:7777"         # joe listen address
  service_accounts:                 # machine identities; each key → principal svc:<name>
    - name: server                  # the "server" account is the key the joe CLI/subcommands present
      key: "my-secret-token"
    - name: ci
      key: "another-token"
  tls_cert_file: ""                 # path to TLS cert (enables HTTPS)
  tls_key_file: ""                  # path to TLS key
  tls_enabled: false                # client side: connect over HTTPS (must match server)
  rate_limit_rps: 0                 # requests/sec per IP (0 = disabled)
  rate_limit_burst: 10

auth:                               # human login (OIDC); optional
  admin_email: ""                   # bootstrap admin identity
  session_ttl: 24h
  post_login_redirect: "/"
  oidc:
    issuer: ""
    client_id: ""
    client_secret: ""
    redirect_url: ""

refresh:
  interval_minutes: 5
  llm_budget:
    max_calls_per_hour: 100
    batch_threshold: 10
    batch_timeout_sec: 30

notifications:
  desktop:
    enabled: false
    priority_threshold: medium      # low | medium | high | urgent
  slack:
    enabled: false
    priority_threshold: high
  quiet_hours:
    enabled: false
    start: "22:00"
    end: "08:00"
    timezone: Local

logging:
  level: info                       # debug | info | warn | error
  file: ""                          # log file path (empty = stderr only)

knowledge:
  embedding_model: ""               # model key for embeddings (defaults to llm.current)
  semantic_top_k: 5
  derived_min_confidence: 0.0       # threshold for surfacing derived knowledge
  sync_enabled: false               # enable Confluence/Notion background sync

database:
  driver: ""                        # defaults to sqlite
  dsn: ""                           # database path/DSN

skills:
  trusted_sources: []               # repos auto-trusted for skill install
  hot_reload_disabled: false
```

### Authentication

There is no single `api_key` field. Machine callers authenticate with a
**service account** bearer token: each `server.service_accounts` entry maps its
`key` to the principal `svc:<name>`. The account named `server` is the key the
`joe` CLI and subcommands (mcp, slack, panic, …) present to the daemon; if it is
absent, the first configured account is used.

When no service accounts and no OIDC issuer are configured, authentication is
disabled and all requests are permitted (single-operator local posture). RBAC is
enforced once a caller principal can be established — see
[operations.md](operations.md).

Human users log in through OIDC (`auth.oidc`). Set `auth.admin_email` to bootstrap
the first admin.

## Environment variables

| Variable                       | Effect                                                            |
| ------------------------------ | ----------------------------------------------------------------- |
| `ANTHROPIC_API_KEY`            | Provider key for Claude                                           |
| `GEMINI_API_KEY` / `GOOGLE_API_KEY` | Provider key for Gemini                                       |
| `OPENAI_API_KEY`               | Provider key for `openai-compat` (OPTIONAL — omit for keyless local endpoints; the endpoint comes from `base_url`) |
| `JOE_CONFIG`                   | Path to the config file (overrides `~/.joe/config.yaml`)          |
| `JOE_SERVER_ADDRESS`           | Overrides `server.address`                                        |
| `JOE_API_KEY`                  | Sets the key of the reserved `server` service account; also the bearer token client subcommands present |
| `JOE_SERVER`                   | Base URL the client subcommands connect to (default `http://localhost:7777`) |
| `JOE_LOG_LEVEL`                | Overrides `logging.level`                                         |
| `JOE_DATABASE_DSN`             | Overrides the database path/DSN                                   |
| `JOE_LLM_PROVIDER`             | Overrides the active LLM provider at runtime                      |
| `JOE_LLM_MODEL`                | Overrides the active LLM model at runtime                         |

Provider keys live **only** in the environment that runs `joe` (the daemon). The
client subcommands never read a provider key.

## Troubleshooting

- **"Failed to load config"** — check the path and that the YAML is valid. YAML is
  indentation-sensitive: use spaces, not tabs, and quote strings with special
  characters.
- **Startup exits asking for a provider key** — set one of `ANTHROPIC_API_KEY` or
  `GEMINI_API_KEY`/`GOOGLE_API_KEY` in the environment that runs `joe`. With both
  present, `joe` defaults to Claude; override with `JOE_LLM_PROVIDER`.
- **No config file** — that's fine. `joe` runs on defaults when one provider key is
  set; create `~/.joe/config.yaml` only to customize.
