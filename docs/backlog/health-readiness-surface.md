# Health and readiness probe surface (/livez, /readyz)

Status: open

Deferred out of the build-version-instrumentation thread to keep that thread
scoped to the build, version, and metrics surface. Flagged here so it is not
lost: the comment on `registerStatusRoutes` (`internal/api/server.go`, currently
~line 170) reads "registers status and health check routes", but no `/health`,
`/healthz`, `/livez`, or `/readyz` endpoint actually exists — the comment
overpromises.

## Goal

A **liveness** endpoint and a **readiness** endpoint, served at the **root level**
(`/livez`, `/readyz`) per ops convention rather than under `/api/v1`, and reachable
without authentication (probes carry no credential). Liveness means "the process
is up and the HTTP listener answers." Readiness means "boot is complete and the
server is serving." The primary consumer is **external harnesses that poll
readiness instead of sleeping** (the local verify/run flows, CI, container
orchestrators) — today they race against an arbitrary sleep.

## Hard non-goal

Readiness must **not** gate on per-component health — adapter connectivity, LLM
reachability, graph warmth. That coarse "is each dependency reachable" signal
belongs to [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md),
not here. `/readyz` reflects **boot-completion only**, never downstream
dependency health, so a probe never flaps because (say) a single adapter's
backend is briefly down.

## Open question for that future thread — is readiness even distinct from liveness?

Re-derive the boot sequence before designing; do not assume. As built today
(`cmd/joe/server.go`): migrations run on boot (`deps.migrateStore` /
`store.Migrate()`), then services are wired, then `coreAgent.Start(...)` launches
a **background refresh loop asynchronously**, then the sweeper starts, then
`deps.startServer(...)` binds the listener. So:

- If the listener binds only after everything synchronous (migrations included)
  and nothing meaningful warms up after the listener is up, then readiness
  **collapses into liveness** — once the socket answers, the server is ready, and
  a single endpoint suffices.
- But `coreAgent.Start` kicks off the refresh cycle in a goroutine and returns
  *before the first refresh pass completes*, and the listener binds after that
  return. So the first graph-refresh pass can still be in flight when the socket
  starts answering. If "ready" is meant to imply "first refresh pass complete,"
  then readiness **does** gate on async first-pass-complete and is genuinely
  distinct from liveness.

Decide deliberately which definition `/readyz` carries (boot-listener-bound vs
first-refresh-complete), and whether both endpoints are warranted or one is
enough, when this thread is picked up.
