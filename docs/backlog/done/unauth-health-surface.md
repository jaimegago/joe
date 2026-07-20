# Unauthenticated health surface — auth-posture and information-exposure analysis

Status: done — merged into healthz-endpoint-surface (backlog-priority-triage, D-0127)

An investigation (session `unauth-health-surface`) into whether Joe should expose
an unauthenticated, deployment-grade health surface — an unauthenticated
`GET /api/v1/status`, or a dedicated unauthenticated `/healthz`/`/livez` and/or
`/readyz` — for Kubernetes probes, load-balancer health checks, and container
healthchecks. This is the **auth-posture and information-exposure** analysis that
the build-side item [`health-readiness-surface`](health-readiness-surface.md)
depends on; it does not itself build anything. The recommendation at the end is
**for ratification**, not a decision — changing Joe's unauthenticated surface is a
design choice, not a doc fix, and no `DECISIONS.md` entry is filed here.

Every claim below is tagged VERIFIED with `file:line` (re-derived from the live
tree on this session) or UNVERIFIED.

---

## 1. Current posture, re-derived

**The edge gate and its public-prefix config.** Authentication is the
`EdgeAuth` middleware. Its only unauthenticated bypass is the configured
`PublicPrefixes`, which defaults to the single prefix `/api/v1/auth/` when the
field is left nil (VERIFIED `internal/auth/middleware.go:23` `defaultPublicPrefix`;
`internal/auth/middleware.go:135-138` the nil→default fallback;
`internal/auth/middleware.go:148-151` the public-path bypass). The production
wiring constructs `EdgeConfig` **without setting `PublicPrefixes`**, so the default
applies — `/api/v1/auth/` is the sole unauthenticated prefix on the chain
(VERIFIED `cmd/joe/server.go:923-929`, no `PublicPrefixes` field). Any other path
on the chain with no valid session cookie and no valid service-account bearer is
rejected `401` (VERIFIED `internal/auth/middleware.go:184-186`).

**Route registration for status and version.** Both are registered under the
`/api/v1` prefix, i.e. *inside* the authenticated chain, **not** under the public
`/api/v1/auth/` prefix (VERIFIED `internal/api/server.go:166` `GET .../status`,
`internal/api/server.go:170` `GET .../version`). So today **`/api/v1/status` and
`/api/v1/version` both require authentication.** The comment on
`registerStatusRoutes` claims it "registers status and health check routes"
(VERIFIED `internal/api/server.go:164`) but registers only `status` and `version`
— the "health check" half is the same overpromise the
[`health-readiness-surface`](health-readiness-surface.md) item already flags.

**The boot guard that makes auth always-enforced.** Joe refuses to boot without a
usable identity configuration: `requireIdentityConfigured` returns an error unless
`cfg.RBACEnabled()` (VERIFIED `cmd/joe/server.go:167-172`), and the server exits
non-zero on that error before building the engine (VERIFIED
`cmd/joe/server.go:829-832`). `RBACEnabled()` is true iff a service account OR an
OIDC issuer is configured (VERIFIED `internal/config/config.go:208-213`). So the
auth-disabled branch inside `EdgeAuth` (VERIFIED `internal/auth/middleware.go:139`,
`157-160`) is **unreachable for the real server** — a booted Joe always enforces
auth. "Running implies governed" is enforced at boot, not merely hoped for.

**No health route exists.** A tree-wide search for `/healthz`, `/livez`,
`/readyz`, `/health`, `/livez` finds matches **only in comments, tests, and this
backlog area** — no route registration anywhere (VERIFIED: grep over
`internal/ cmd/` `.go` returns zero `mux.HandleFunc`/route hits for any of these).

**A second, already-open unauthenticated surface — the static UI.** The embedded
web UI is mounted **outside** the middleware chain: any path that is **not** under
`/api/v1` is served by the static handler with "no edge auth, rate limit, metrics,
or body cap" (VERIFIED `internal/webui/embed.go:69-94`; wired at
`cmd/joe/server.go:963`). The static handler serves real files when they exist and
otherwise falls through to the SPA shell `index.html` for any other path (VERIFIED
`internal/webui/static.go:89-98`). **Consequence:** a *root-level* `/healthz` (or
`/livez`/`/readyz`) today is **not** a 404 — it is served `200` with the
`index.html` HTML shell, unauthenticated, because it is neither an asset nor a real
file and falls through to `serveIndex` (VERIFIED `internal/webui/static.go:95-97`,
`121-125`). This matters two ways: a root-level health route would be claimed by
the static handler, not the API chain (so it would have to be registered there or
the chain would never see it); and "an unauthenticated 200 at root" is already the
status quo, just not a probe contract.

**The metrics port.** When metrics are enabled (default on: `OTEL_METRICS_ENABLED`
defaults `true`, exporter `prometheus`, port `9090` — VERIFIED
`internal/observability/otel.go:54-56`), a **separate** `http.Server` serves only
`/metrics` from a bare mux with **no middleware and no auth** (VERIFIED
`cmd/joe/server.go:980-991`). Any other path on that port is a mux `404`. The
Prometheus exposition includes the `joe_build_info` gauge whose **labels carry full
build identity: `version`, `commit`, `build_time`, `ui_digest`** (VERIFIED
`internal/observability/metrics.go:555-561`; label constants
`internal/observability/metric_names.go:67-71`).

**Summary of what an unauthenticated caller can reach today:**

| Surface | Port | Unauthenticated? | What it returns |
|---|---|---|---|
| `/api/v1/auth/*` (login, callback, logout, config) | API (:7777) | Yes (public prefix) | OIDC flow + `oidc_enabled` config |
| `/api/v1/status`, `/api/v1/version` | API (:7777) | **No — 401** | (see §2 for shape when authenticated) |
| any other `/api/v1/*` | API (:7777) | No — 401 | — |
| any non-`/api/v1` path (`/`, `/graph`, `/healthz`, …) | API (:7777) | Yes (static, off-chain) | SPA `index.html` shell `200` |
| `/metrics` | metrics (:9090, default on) | **Yes (no auth at all)** | Prometheus exposition incl. `joe_build_info{version,commit,build_time,ui_digest}` |
| any other path on metrics port | metrics (:9090) | Yes | `404` |

---

## 2. The governed-safety implication — what each candidate would expose

Joe's wall is "running implies governed; the only unauthenticated API-port surface
is the auth flow itself." Each health candidate punches a differently-sized hole.

**What `/status` returns today (authenticated).** `{ "status": "ok", "version":
<buildinfo.Version>, "time": <RFC3339> }` — version string only, no commit/digest
(VERIFIED `internal/api/server.go:399-405`).

**What `/version` returns today (authenticated).** The full `buildinfo.Info`:
`version`, `commit`, `build_time`, `ui_digest` (VERIFIED
`internal/api/server.go:411-413`; struct `internal/buildinfo/buildinfo.go:53-57`).

**Per-candidate information exposure if made unauthenticated:**

- **Bare liveness** ("process is up"): reveals only that the HTTP listener answers.
  No version, no dependency state. This is the *minimum* — and notably it reveals
  nothing an attacker cannot already learn by getting a `401` from any `/api/v1`
  path or a `200` HTML shell from `/`.
- **`status` as-is, unauthenticated**: leaks the `version` string and a server
  clock (`time`). Version alone is a coarse fingerprint.
- **`version` as-is, unauthenticated**: leaks `version` + `commit` + `build_time` +
  `ui_digest`. The `commit` pins the exact source tree; combined with a public repo
  (Joe is Apache-2.0, and its source is public whether an operator downloaded a
  published release binary or built that commit themselves) this is a precise map
  from a running instance to its exact code, i.e. to any known issue at that commit.
  Published releases sharpen this rather than blunting it: a leaked `version` that
  matches a release tag also identifies a widely distributed, byte-identical binary,
  so the `ui_digest` and `commit` are known-in-advance constants for every operator
  running that release rather than per-install values. This is the strongest
  reconnaissance signal of the options.
- **Readiness reflecting dependency health**: by construction reveals *which*
  dependencies are healthy/unhealthy (DB up/down; if it ever included adapters,
  which backends are reachable). That is operationally useful and also the most
  information-bearing — an unauthenticated readiness probe that distinguishes
  failure modes hands an attacker a live dependency map.

**Fingerprinting caveat that defuses part of the concern.** Build identity
(`version`/`commit`/`build_time`/`ui_digest`) is **already exposed
unauthenticated** on the default-on metrics port via `joe_build_info` (VERIFIED
`internal/observability/metrics.go:555-561` + `cmd/joe/server.go:980-991`). So an
unauthenticated `/version` or version-bearing `/status` on the API port reveals
**nothing the metrics port does not already reveal by default**. The
not-equivalent part: operators routinely firewall the metrics port to scrape-only
while the API port is the internet-facing one, so the *reachability* of the two
differs even though the *content* does not. The honest framing for ratification:
the marginal fingerprinting cost of an unauthenticated version surface is small
*given the current metrics default*, but it is not zero, because it moves build
identity onto the port most likely to be exposed.

---

## 3. Liveness versus readiness, distinguished — against Joe's live wiring

**Boot sequence (re-derived).** In order: migrations run **synchronously**
(VERIFIED `cmd/joe/server.go:303` → `store.Migrate()` at
`cmd/joe/server.go:115-116`); services are wired; `coreAgent.Start` is called
(VERIFIED `cmd/joe/server.go:746`); the session retention sweeper starts (VERIFIED
`cmd/joe/server.go:786`); the metrics server starts (VERIFIED
`cmd/joe/server.go:992`); and **last**, the main listener binds via
`deps.startServer` (VERIFIED `cmd/joe/server.go:1008`; binds in
`server.ListenAndServe[TLS]` `cmd/joe/server.go:1265-1281`).

**`coreAgent.Start` is asynchronous *and ticker-gated*.** `Agent.Start` calls
`Refresher.Start`, which launches `go r.refreshLoop(...)` and returns immediately
(VERIFIED `internal/coreagent/agent.go:107-117`;
`internal/coreagent/refresh.go:106-118`). The loop's first refresh does not run at
boot — it waits for the first `ticker.C` tick, one `interval` later (VERIFIED
`internal/coreagent/refresh.go:145-151`). **Correction to the existing item's
framing:** the [`health-readiness-surface`](health-readiness-surface.md) note says
the first refresh pass "can still be in flight when the socket starts answering."
Re-derived, it is stronger: the first pass has **not even started** when the socket
binds, and will not for one full interval. Either way, defining `/readyz` as
"first-refresh-complete" would keep an instance "not ready" for an arbitrary
interval after it is fully serving — a bad contract. Graph warmth is **not** a
readiness signal.

**What each probe would actually check in Joe:**

- **Liveness** — cheap, dependency-free: the handler returns `200` if it runs at
  all. It must **not** touch the DB (`sqlStore.DB()`), the embedded store, or any
  adapter. Because the listener binds last, "the socket answers" already implies
  migrations ran and services are wired — liveness is honest the instant it can be
  reached.
- **Readiness** — "ready to take traffic." Given the wiring, by the time the
  listener answers at all, the only synchronous prerequisites (migrations,
  service wiring) are already done. The one non-degenerate readiness signal that
  remains is a **cheap DB ping** (catch a DB handle that died *after* boot —
  SQLite file gone, disk full). Adapter/LLM/graph reachability is explicitly a
  **non-goal** for readiness per the existing item and belongs to
  [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md);
  folding it in would make readiness flap on a single backend blip.

**Recommendation on shape:** liveness and readiness are *nearly* degenerate in
Joe because the listener binds after all synchronous boot work. A single liveness
endpoint covers the common probe need. A separate readiness endpoint is justified
**only** if a post-boot DB-died signal is wanted; if so it should ping the DB and
nothing else, and reveal only ready/not-ready (a bare status code), never *which*
dependency failed.

---

## 4. Reconcile against existing backlog — relationship to `health-readiness-surface`

The open item [`health-readiness-surface`](health-readiness-surface.md) is the
**build** item: it names `/livez` and `/readyz` at the root level, unauthenticated,
as not-yet-built; it already fixes the definition (boot-completion, not dependency
health) and lists the primary consumer (external harnesses polling readiness
instead of sleeping).

**This item does not duplicate or supersede it.** This is the
**auth-posture-and-information-exposure analysis that the build item depends on** —
it answers the questions the build item defers: *what may an unauthenticated probe
reveal, and what does opening the surface cost the governed-safety posture.* The
right relationship:

- Keep both items. `health-readiness-surface` stays the build/implementation item.
- This item (`unauth-health-surface`) is the **prerequisite design analysis**; its
  ratified recommendation (§5) should be folded **into** the build item as its
  decided contract when that thread is picked up.
- **Do not modify** `health-readiness-surface` from this investigation (read-only
  constraint). Two re-derived facts this analysis contributes to it, to apply when
  it is picked up: (a) the listener binds **last**, so readiness collapses toward
  liveness; (b) the refresh loop is **ticker-gated** and its first pass has not
  even started at bind time, so "first-refresh-complete" is an unworkable readiness
  definition — confirming, more strongly, that item's boot-completion-only stance.

---

## 5. Options and recommendation

| # | Option | Cost to governed-safety posture | Operational payoff |
|---|---|---|---|
| A | **Leave `status`/`version` authenticated; rely on the metrics port for a process-up signal** | None — no new unauthenticated API-port hole | Weak: `/metrics` proves the *metrics* server is up, on a different port, and is a heavy Prometheus payload — a poor K8s/LB probe target; nothing proves the *API* listener is serving |
| B | **Make `/api/v1/status` itself unauthenticated** | Moderate: punches a version-bearing hole in the wall; leaks `version` + clock to anyone; ties the probe contract to a business endpoint inside `/api/v1` | Cheap to ship (move it to a public prefix); gives a real API-port up-signal — but reveals more than a probe needs |
| C | **Add a dedicated unauthenticated minimal `/healthz` (liveness): up-or-not, no version, no dependency detail** | Small and bounded: one new unauthenticated route revealing strictly less than a `401` or the existing `200` HTML shell already do | Clean probe contract for K8s liveness / LB / container healthcheck; cannot be used for fingerprinting; decoupled from build identity |
| D | **Add a separate `/readyz` (readiness)** with a deliberate auth tier and a DB-ping-only check, revealing only ready/not-ready | Small if it reveals only a status code and never *which* dependency failed; larger if it ever distinguishes failure modes (a live dependency map) | Lets harnesses poll readiness instead of sleeping (the consumer the build item names); marginal over C given the listener-binds-last wiring |

**Framed recommendation (for you to ratify):**

1. **Adopt Option C** — a dedicated, unauthenticated, **minimal `/healthz`**
   liveness endpoint that returns only up-or-not (e.g. `200` + `{"status":"ok"}`),
   with **no `version`, no `commit`, no `ui_digest`, and no dependency detail**.
   This is the smallest possible hole: it reveals strictly less than the `401` an
   attacker already gets from any `/api/v1` path and less than the `200` HTML shell
   the static handler already serves at `/`. It does not move build identity onto
   the API port.
2. **Decide its placement deliberately.** Because non-`/api/v1` paths are served by
   the off-chain static handler (§1), a root-level `/healthz` must be registered in
   that handler (or the static mount must special-case it) — it will **not** reach
   the API chain on its own. Alternatively register it under `/api/v1/` and add it
   to `EdgeAuth`'s `PublicPrefixes`. The root-level placement matches ops
   convention and the existing build item; the trade-off is that it lives in the
   static path, not the API chain.
3. **Reject Option B** (unauthenticated `status`/`version`): it reveals more than a
   probe needs, and although build identity is already on the default-on metrics
   port, B moves it onto the internet-facing API port for no probe benefit C does
   not already give.
4. **Treat `/readyz` (Option D) as optional and deferred to the build item.** Given
   that the listener binds last, readiness collapses toward liveness; the only
   non-degenerate signal is a cheap DB ping. Ship `/readyz` **only** if the
   sleep-replacement consumer in `health-readiness-surface` is judged worth it, and
   if so make it DB-ping-only and reveal a bare status code, never a per-dependency
   breakdown. Adapter/LLM reachability stays out — that is
   [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md).
5. **No `DECISIONS.md` entry until you ratify.** This is an investigation; the
   above is a proposal, not a posture change.
