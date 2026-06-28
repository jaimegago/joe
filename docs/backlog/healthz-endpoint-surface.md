# Unauthenticated health-probe surface — standards-anchored build spec (/healthz, /livez, /readyz)

Status: open

A **decision-ready build specification** for an unauthenticated health-probe
surface on Joe, anchored to established Kubernetes / HTTP-service convention
rather than a bespoke design. The point is explicitly to **not reinvent this**:
liveness / readiness / startup probes are standardized; this item adopts the
standard contract and records only the places where Joe's specifics (auth chain,
off-chain static handler, boot order) force a **Joe-specific placement** decision.

This item **does not ratify a design decision**. The standard naming and
semantics are adopted as given; the only genuinely open choices left for the
maintainer are (a) the Joe-specific placement of the route and (b) whether a
separate readiness endpoint is worth shipping at all, given that readiness
collapses toward liveness here. No `DECISIONS.md` entry is filed by this pass.

Relationship to the two existing items is stated in §5; the survivor
recommendation is at the end of that section. This pass modifies neither of
those files.

---

## 1. The standard contract (anchored to Kubernetes docs)

Sources consulted live this session (web access available):

- Kubernetes, *Configure Liveness, Readiness and Startup Probes* —
  <https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/>
- Kubernetes, *Kubernetes API health endpoints* (`/livez`, `/readyz`, the
  `/healthz` deprecation) —
  <https://kubernetes.io/docs/reference/using-api/health-checks/>

The standard, stated plainly:

- **Liveness** (`/livez`, historically `/healthz`): "the process is up and the
  server loop is responsive." A failure means the container is in a
  non-recoverable state (deadlock, wedged loop) and Kubernetes **restarts** it.
  A liveness probe **must not** touch external dependencies (DB, adapters,
  other services) — a dependency blip would otherwise trigger pointless restart
  storms. It is cheap, conventionally **unauthenticated** (probes carry no
  credential), and reveals nothing beyond up-or-not. HTTP success is any
  `2xx`/`3xx` (`>=200 && <400`); non-2xx/3xx is failure.

- **Readiness** (`/readyz`): "ready to serve traffic." May check critical
  dependencies (e.g. the API server waits on etcd). A failure means Kubernetes
  **removes the Pod from Service endpoints** — no restart, no traffic — until it
  recovers. The contract returns ready / not-ready (a bare status code); it does
  **not** need to leak a per-dependency map to callers. (Kubernetes offers an
  opt-in `?verbose` view, but that is a debugging affordance, not the probe
  contract, and is not what an unauthenticated internet-facing probe should
  default to.)

- **Startup** (`/startz` or a dedicated path): for **slow-starting** apps. It
  gates liveness and readiness until boot completes, so a slow boot is not
  mistaken for a deadlock and killed. Once it passes, liveness/readiness take
  over.

- **Naming.** The `z`-suffixed adjective form (`/livez`, `/readyz`) is the
  Kubernetes API-server convention; `/healthz` is the older liveness name,
  **deprecated since Kubernetes v1.16** in favor of `/livez` + `/readyz`, but
  still ubiquitous as a generic liveness path in application and example YAML
  (the probe example in the tasks doc itself probes `path: /healthz`). This item
  adopts that naming as the standard and does not invent alternatives.

**Bearing on Joe.** A startup probe is **not warranted** here: Joe's listener
binds last (§2), so the socket only answers once synchronous boot is done — there
is no slow-start window to protect. The live decision space is liveness (clearly
worth it) and readiness (marginal — see §3).

---

## 2. Joe-specific constraints (re-derived from the live tree, VERIFIED)

Every fact below was re-derived this session and tagged VERIFIED with `file:line`.

**No health route exists today.** A tree-wide search for `/healthz`, `/livez`,
`/readyz`, `/health` finds matches only in comments, tests, and this backlog
area — no route registration. The only health-shaped routes registered are
`status` and `version` (VERIFIED `internal/api/server.go:166`, `:170`), and the
comment on `registerStatusRoutes` claims it "registers status and health check
routes" (VERIFIED `internal/api/server.go:164`) while registering neither a
health nor a liveness/readiness route — the same overpromise the prior items
flag.

**`/status` and `/version` sit inside the authenticated chain.** Both are
registered under the `/api/v1` prefix, not under the public `/api/v1/auth/`
prefix (VERIFIED `internal/api/server.go:166` `GET .../status`,
`internal/api/server.go:170` `GET .../version`), so both require authentication
today. `/status` returns `{status, version, time}` — version string + clock, no
commit/digest (VERIFIED `internal/api/server.go:399-405`). `/version` returns the
full `buildinfo.Info` — `version` + `commit` + `build_time` + `ui_digest`
(VERIFIED `internal/api/server.go:411-413`).

**The auth-exemption mechanism.** Authentication is the `EdgeAuth` middleware;
its only unauthenticated bypass is the configured `PublicPrefixes`, which
defaults to the single prefix `/api/v1/auth/` when the field is left nil
(VERIFIED `internal/auth/middleware.go:23` `defaultPublicPrefix`; the nil→default
fallback `internal/auth/middleware.go:135-138`; the public-path bypass
`:148-151`). Production wiring constructs `EdgeConfig` **without** setting
`PublicPrefixes` (VERIFIED `cmd/joe/server.go:923-929`, no `PublicPrefixes`
field), so `/api/v1/auth/` is the sole unauthenticated prefix on the chain; any
other path with no valid session cookie and no valid service-account bearer is
rejected `401` (VERIFIED `internal/auth/middleware.go:184-186`).

**The static-handler trap (placement is load-bearing, not cosmetic).** The
embedded UI is mounted **outside** the middleware chain: requests under `/api/v1`
delegate to the auth chain; **every other path** is served by the static handler
with "no edge auth, rate limit, metrics, or body cap" (VERIFIED
`internal/webui/embed.go:69-94`; the `isAPIPath` gate `:80-94`; wired at
`cmd/joe/server.go:963`). The static handler serves a real embedded file when one
exists and otherwise **falls through to the SPA shell `index.html`** for any
other path (VERIFIED `internal/webui/static.go:89-98`, `:121-125`).
**Consequence:** a *root-level* `/healthz` today is **not** a 404 — it is served
`200` with the `index.html` HTML shell, unauthenticated, because it is neither an
asset nor a real file. So a naively root-mounted `/healthz` would be **claimed by
the static handler and never reach the API mux**: it must be registered *in* the
static handler (or the static mount must special-case it), or else live under
`/api/v1/` where the chain can see it.

**Boot order constrains readiness — the listener binds last.** In order:
migrations run synchronously (VERIFIED `cmd/joe/server.go:303` →
`store.Migrate()` at `:115-116`); services are wired; `coreAgent.Start` is called
(VERIFIED `cmd/joe/server.go:746`); the metrics server starts (VERIFIED
`cmd/joe/server.go:992`); and **last**, the main listener binds via
`deps.startServer` (VERIFIED `cmd/joe/server.go:1008`). So "the socket answers"
already implies migrations ran and services are wired.

**The refresh loop is async and ticker-gated — "first-refresh-complete" is
unworkable as readiness.** `coreAgent.Start` launches `go r.refreshLoop(...)` and
returns immediately; the loop's first refresh does **not** run at boot — it waits
for the first `ticker.C` tick, one full `interval` later (VERIFIED
`internal/coreagent/refresh.go:142-151`). At bind time the first refresh pass has
**not even started**. Defining `/readyz` as "first-refresh-complete" would keep a
fully-serving instance "not ready" for an arbitrary interval. Graph warmth is
**not** a readiness signal; readiness collapses toward liveness here.

**Build identity is already exposed unauthenticated on the default-on metrics
port.** When metrics are enabled (default on: `OTEL_METRICS_ENABLED` defaults
`true`, port `9090` — VERIFIED `internal/observability/otel.go:54`, `:56`), a
separate `http.Server` serves only `/metrics` from a bare mux with no middleware
and no auth (VERIFIED `cmd/joe/server.go:980-991`). The exposition includes
`joe_build_info`, whose labels carry full build identity — `version`, `commit`,
`build_time`, `ui_digest` (VERIFIED `internal/observability/metrics.go:555-561`).
**This bounds what an unauth liveness endpoint should add:** an unauthenticated
liveness probe should reveal *strictly less* than what is already public, i.e.
up-or-not and no build identity — there is no probe benefit to re-exposing
version/commit/digest on the internet-facing API port.

---

## 3. Placement options (mutually exclusive) — each with meaning-of-200

The standard contract is fixed; the placement is the open Joe-specific decision.
Each option's row states **what a `200` actually proves under that placement**,
because the static-handler trap makes that non-obvious.

| # | Placement | Meaning of a `200` under this placement | One-line tradeoff |
|---|-----------|------------------------------------------|-------------------|
| **P1** | **`/api/v1/healthz` (or `/api/v1/livez`) registered on the API mux, with `/api/v1/healthz` added to `EdgeAuth.PublicPrefixes`** | A `200` proves **the API mux itself is serving** — the request traversed the same chain real API traffic does, minus the auth gate. This is the *honest* liveness signal: it proves the thing probes actually care about (the API listener loop is responsive). | Lives inside `/api/v1` (slightly off ops convention, which prefers root-level `/livez`); requires widening `PublicPrefixes` from its single-entry default — a deliberate, reviewable one-line posture change. |
| **P2** | **Root-level `/healthz` special-cased *inside* the static handler** (matched before the SPA-shell fallthrough) | A `200` proves only **the static handler is serving** — it does **not** prove the API mux or auth chain is alive, since the static handler is off-chain. It is a weaker liveness signal than it looks: the API surface could be wedged while this still returns `200`. | Matches root-level ops convention and the prior build item's `/livez` naming; but the probe answers from a different server path than the API, so a green `/healthz` does not imply a healthy API. Must be matched ahead of `serveIndex` or it is indistinguishable from the existing `200` HTML shell. |
| **P3** | **Root-level `/healthz` on a dedicated tiny mux fronted ahead of both the API chain and the static handler** in `webui.Mount` (or the equivalent root handler) | A `200` proves **the root HTTP server accepted and dispatched a request** — stronger than P2 (it sits at the very front, before static), weaker than P1 (it still does not traverse the API mux). | Cleanest root-level contract and no `PublicPrefixes` change; but adds a new dispatch branch in the root handler (`webui.Mount`, VERIFIED `internal/webui/embed.go:75-87`) — more surface than P1's one-line prefix add, and still not an API-mux liveness proof. |

The meaningful split is **P1 (proves the API is alive, lives under `/api/v1`)**
versus **P2/P3 (root-level per convention, but proves only that an HTTP server —
not the API — is alive)**. Choosing is a judgment about whether matching the
root-level `/livez` convention is worth a `200` that proves less about the API.
This item does **not** pre-pick.

In all placements the liveness handler must return cheaply with **no DB touch,
no adapter call, no `buildinfo` body** — just a bare `200` (e.g.
`{"status":"ok"}`), consistent with §1 and bounded by §2's "already-public build
identity" note.

---

## 4. Is readiness worth shipping at all?

Open question, deliberately left to the maintainer. Per §2, the listener binds
**last**, so by the time `/readyz` could answer, every synchronous prerequisite
(migrations, service wiring) is already done, and the async refresh loop is
explicitly **not** a readiness signal. That leaves exactly one non-degenerate
readiness check: a **cheap DB ping** to catch a handle that died *after* boot
(SQLite file removed, disk full). Everything richer — adapter/LLM/graph
reachability — is a **non-goal** and belongs to
[`governed-connectivity-check-surface`](governed-connectivity-check-surface.md);
folding it in would make readiness flap on a single backend blip.

So the readiness decision is binary:

- **Ship `/readyz` only if** the sleep-replacement consumer named in the prior
  build item (external harnesses / CI / `verify` and `run` flows polling
  readiness instead of racing an arbitrary sleep) is judged worth a second
  endpoint. If shipped, it must be **DB-ping-only** and reveal a **bare
  ready/not-ready status code** — never a per-dependency breakdown (no
  unauthenticated dependency map).
- **Otherwise skip it.** A single liveness endpoint covers the common probe
  need, and readiness here is nearly degenerate with liveness.

---

## 5. Relationship to the two existing items, and survivor recommendation

Three items now circle this surface:

1. [`health-readiness-surface`](health-readiness-surface.md) — the **prior build
   stub**. Names `/livez` and `/readyz` at the root level, unauthenticated, as
   not-yet-built; fixes the definition to boot-completion (not dependency
   health); names the sleep-replacement consumer. This item **refines** that
   stub: it adds the grounded standard contract, the re-derived Joe constraints
   with `file:line`, and the named placement options the stub left implicit.
2. [`unauth-health-surface`](unauth-health-surface.md) — the **analysis** this
   item operationalizes. It is the auth-posture and information-exposure study
   (what an unauthenticated probe may reveal; the metrics-port fingerprinting
   caveat; the Option-C minimal-`/healthz` recommendation for ratification).
   This item turns that analysis into a buildable, placement-decided spec.
3. **This item** (`healthz-endpoint-surface`) — the **standards-anchored,
   decision-ready build specification** drawing on both.

**Survivor recommendation.** When this surface is actioned, collapse to **a
single tracked build item: this one (`healthz-endpoint-surface`)**. Concretely:

- **Close `health-readiness-surface` in favor of this item** (or merge its one
  unique contribution — the sleep-replacement consumer framing, already captured
  in §4 — and then close it). This item supersedes it as the build spec; keeping
  both is duplication.
- **Keep `unauth-health-surface` until its recommendation is ratified**, then
  archive it as the design record. It is the analysis this spec rests on, not a
  competing build item; once its Option-C recommendation is either ratified into
  this spec's placement choice or set aside, it has served its purpose and can
  move to `done/`.
- Net end-state: **one** open build item (this file) carrying the decided
  contract, with the analysis archived as provenance.

This pass modifies neither `health-readiness-surface.md` nor
`unauth-health-surface.md`.
