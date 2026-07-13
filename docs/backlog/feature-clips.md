# Landing-page demo clips — record, encode, and commit to joeagent.dev

Status: in-progress

Three short screen-capture clips for the joeagent.dev landing page, each showing
one Joe surface driven against a live cluster:

- **feature-chat** — the chat surface investigating the `shop` world (e.g. "why is
  orders restarting?" surfacing the OOMKilled evidence).
- **feature-graph** — the infrastructure graph showing the `shop` object graph and
  the checkout → payments / checkout → orders dependency edges.
- **feature-mcp** — Joe's tools driven over MCP (`joe mcp`) against the same world.

## Done

- The demo world is landed: `examples/demo-world/` — a fictional three-service
  `shop` namespace (orders OOMKilling near a 128Mi limit, payments NotReady on a
  real 404 readiness probe, checkout healthy with discoverable dependency env
  vars), plus a README covering stage / reset / verify. Every symptom is real
  cluster state; validated on a local kind cluster including reset-and-restage
  (D-0076).
- Orders two-phase cadence landed: the workload now keys off a start counter
  persisted in an `emptyDir` to run a **fast phase** (first three starts: hold
  near the limit ~15s then burst over it) that seeds real OOMKilled restarts
  within ~5 minutes of apply, then a **steady phase** that bursts only every
  ~13 min. The ~13-min steady interval sits past the kubelet's back-off reset
  window (back-off caps at 300s; reset threshold is 2× that = 600s of clean
  running, verified against the k8s `release-1.31` source), so steady restarts
  reset to the base ~10s delay and the pod holds `Running` 1/1 near the 128Mi
  limit instead of resting in `CrashLoopBackOff`. Every kill is a genuine kernel
  OOM (exit 137). This corrects the earlier single-phase ~13-min tuning, which
  met the steady-state goal but pushed the first demo symptom out to ~13 min.
  README time-to-symptom (~5 min), two-phase behavior, steady state, and verify
  commands updated to match; validated live on a kind cluster (fast phase to
  RESTARTS≥2 with OOMKilled, then a held Running steady window).

## Remaining

- Record the three clips against the staged world.
- Encode the clips and commit them into the **joeagent.dev** repository (the
  landing-page site is a separate repo from this one); wire them into the landing
  page.
