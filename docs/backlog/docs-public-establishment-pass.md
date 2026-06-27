# Backlog — Establish docs/public from the operator-facing root docs

Status: open

Deferred out of the `docs-tree-restructure` session, which reorganized the docs
tree into `docs/project` (build-meta plus the ADR annex), `docs/reference` (system
truth), and `docs/backlog/investigations` (live findings), but deliberately left
the operator-facing how-to docs at the `docs/` root as the agreed handoff to a
separate, later pass.

## Progress

- **`docs-public-establishment-pass-01` (done).** Established `docs/public/` as the
  sole published documentation surface for joeagent.dev and laid down the nine-section
  taxonomy (decision **D-0052**). Fully wrote **Overview** and **Concepts**; created
  under-construction placeholders for the other seven sections. `docs/reference` stays
  internal-only and is not published. The thread is **not finished** — seven sections
  remain to write across slices -02..-05 below.
- **`docs-public-establishment-pass-02` (done).** Filled **Install and Build** (how-to:
  build-from-source-only posture, `make build`, running the daemon, the full
  authentication posture — service-account bearer keys, OIDC, and the OIDC-only admin
  bootstrap, plus observation mode) and **Quickstart** (tutorial: the minimal
  boot-to-first-answer path — `JOE_API_KEY` identity + `ANTHROPIC_API_KEY` + observation
  mode — driving the real `POST /api/v1/tasks` interaction surface). The other five
  placeholders were left untouched. The thread is **not finished** — five sections remain
  across slices -03..-05 below.
- **`docs-public-establishment-pass-03` (done).** Filled **Configuration** (reference: the
  full YAML and environment surface with code-true defaults, corrected against the live
  tree — `auth.session_ttl` is `12h` not `24h`, `server.rate_limit_burst` has no default
  and clamps to `1` when limiting is enabled, and the inert/reserved keys are flagged
  honestly: the non-USD `llm.currency`/`usd_to_configured_rate` conversion, and the whole
  `refresh.*` block — `interval_minutes` plus `llm_budget.*` — which is parsed but has no
  consumer) and **Integrations** (how-to: the register→promote→arm spine, then per-type
  guidance for only the gate-passing documentable set, with each type's credential
  mechanism). The gate is recorded as decision **D-0055**. The remaining three placeholders
  were left untouched. The thread is **not finished** — two slices remain (-05, -06) below.
- **`docs-public-establishment-pass-04` (done — corrective).** Corrected the **Integrations**
  page written in -03. The page had presented one uniform register→promote→arm→test runtime
  spine for all 18 documentable types, but re-derivation of the live tree showed the runtime
  type→adapter constructor (`newAdapterForType`) has no case for `github`, `gitlab`, `splunk`,
  `dynatrace`, `newrelic`: these five are armable but have no runtime construction path, so
  the connectivity test cannot build them and they come live only at a daemon restart via the
  boot connect pass (`connectSourcesDefault`). The page now routes per-type by activation
  path — **runtime-registerable (13)** via `/test`, **boot-config-only (5)** via
  register+promote+env-var+restart — with an explicit activation column on every table. The
  MCP mention was trimmed to a one-line pointer to Guides (MCP is documented in slice -05).
  The routing rule is recorded as decision **D-0056** (a refinement of the D-0055 gate; the
  documentable set is unchanged). No code or invariant changed. The thread is **not
  finished** — two slices remain (-05, -06) below.
- **`docs-public-establishment-pass-05` (done).** Filled **Guides** (how-to: one page per
  feature area — the web UI and human OIDC login, chat sessions, the incident regime,
  skills, the MCP server, the Slack bot, the knowledge graph, and documentation proposals
  — plus the Guides landing). Each page was re-derived from the live tree against the
  shipped parser/route surface, honoring the known accuracy traps: documentation publish
  is presented as a step distinct from approval and gated by the write floor (it will not
  publish in observation mode); the web UI lands on chat with no dashboard and a flat nav
  plus an `is_admin` admin subgroup; the incident page documents the captain gate and
  banner but not the deferred captain UI/read surface, and flags `joe incident list` as a
  stub; skills are documented at the real `joe skills` shapes (no `status` subcommand);
  and the Integrations MCP pointer now resolves to the new MCP guide. A straight content
  fill — no decision was forced, no code or invariant changed. Because Guides and
  Operations were split into separate slices, the remaining plan was renumbered:
  Operations (with the break-glass fold) is **-06** and API Reference is **-07**. The
  thread is **not finished** — two slices remain (-06, -07) below.
- **`docs-public-establishment-pass-06` (done).** Filled **Operations** (how-to: running the
  daemon, TLS at a how-to level, the embedded SQLite store at `~/.joe/joe.db` and the
  session archive at `~/.joe/session-archive`, process health, observability, recovering
  from safe mode, and the break-glass-access fold). Re-derived against the live tree,
  honoring the known accuracy traps: the Prometheus metrics endpoint is on a **separate
  port** (`9090`/`/metrics`) from the API (`7777`); the traces exporter **defaults to
  `none`** so spans are instrumented but not exported until `OTEL_TRACES_EXPORTER` is set
  to `stdout`/`otlp`; **no** dedicated `/livez`/`/readyz`/`/healthz` endpoint exists (only
  `GET /api/v1/status` and `GET /api/v1/version` are served); and there is **no API unlock**
  — `POST /api/v1/unlock` does not exist, the write floor is sealed at boot, and recovery is
  the local `joe unlock` CLI (clears the `cluster_panic_state` row, never signals a live
  process) plus a restart, with arming writes and acknowledging the panic as distinct steps.
  Re-derivation corrected two of the prompt's inventory-era leads against the live tree: the
  per-LLM-call metrics are **now wired and emitted** (`llm_requests_total` etc.) and the
  cache metrics were **removed entirely** — both by **D-0051** (committed after the inventory
  was written), so the page documents the LLM metrics as available and does not mention cache
  metrics at all. A straight content fill — no decision was forced, no code or invariant
  changed. The thread is **not finished** — one slice remains (-07) below.

## What stays at `docs/` root (the inputs to this pass)

These five operator-facing documents were **not** moved by `docs-tree-restructure`.
They remain directly under `docs/` as the agreed operator-facing basis, and they are
the inputs to this `docs/public` establishment pass:

- `docs/configuration.md`
- `docs/integrations.md`
- `docs/operations.md`
- `docs/web-ui.md`
- `docs/break-glass-access.md`

## Agreed nine-section nav (D-0052)

One section per nav entry, ordered by `weight` ascending (increments of ten):

1. Overview
2. Quickstart
3. Concepts
4. Install and Build
5. Configuration
6. Integrations
7. Guides
8. Operations
9. API Reference

Each section is a directory under `docs/public/` with an `_index.md` carrying
`title` + `weight` front-matter. Concepts holds one explanation page per concept.
Forward links use directory-style relative paths so placeholders fill in place.

## Per-section source mapping and disposition

**Four operator docs reworked** (each becomes/feeds a section, rewritten — not copied —
for a public audience):

| Root doc | Target section | Disposition |
|---|---|---|
| `docs/configuration.md` | Configuration | reworked into the section |
| `docs/integrations.md` | Integrations | reworked into the section |
| `docs/operations.md` | Operations | reworked into the section |
| `docs/web-ui.md` | Guides | reworked into task-focused how-to |

**Break-glass folded into Operations:**

| Root doc | Target section | Disposition |
|---|---|---|
| `docs/break-glass-access.md` | Operations | folded in, not its own section |

**Five sections net-new** (no single operator-doc source; derived/rewritten from the
internal reference tree + DECISIONS for a public audience, gated by the feature
inventory):

- Overview — *written in -01*
- Concepts — *written in -01*
- Quickstart — net-new
- Install and Build — net-new
- API Reference — net-new

## Remaining slice plan

- **-02** — Install and Build + Quickstart. *(done)*
- **-03** — Configuration + Integrations. *(done)*
- **-04** — Integrations corrective: runtime-registerable vs boot-config-only activation routing (D-0056). *(done)*
- **-05** — Guides. *(done)*
- **-06** — Operations (including the break-glass fold into Operations). *(done)*
- **-07** — API Reference.

Every claim in every slice is gated by `docs/backlog/public-docs-feature-inventory.md`
(shipped-truth). No PARTIAL or PRESENT-BUT-UNWIRED feature is documented as fully
realized. Internal `file:line` citations never appear on a public page; that
verification discipline stays in session reports.

## Deferred

Doc-version stamping is deferred to a footer commit-and-`ui_digest` stamp applied at
release-cadence time, rather than a versioned doc tree (D-0052).
