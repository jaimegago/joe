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

- Camera-neutrality landed: the `orders` workload no longer narrates its own
  staging in any surface an agent reads. A live run had Joe quote the container
  args' design comments (excursion cadence, the OOM intent, the restart-count
  narration) and the "holding steady near the memory limit" log line, and
  correctly infer the memory pressure was intentional demo logic — the fixture
  was handing the copilot its answer instead of making it diagnose the cluster.
  The script's memory arithmetic and two-phase logic are unchanged (100Mi
  baseline, 45Mi burst, three fast starts, ~13-min steady cadence); what changed
  is that the `args` block carries no comments and the log lines now read like an
  ordinary order service (a startup line, then uniform `processing batch N`
  lines — the bursting cycle logs exactly what every other cycle logs, so nothing
  marks the fatal one). The design comments moved out of `args` into a YAML
  comment, which the API server strips on apply, plus a maintainer note warning
  against reintroducing them. `payments`, `checkout`, and the namespace needed no
  change: their explanatory comments already sat outside `args` and were already
  stripped, and their labels/probe paths already read as a plausible app — the
  believable-labels criterion from D-0076 held. Re-validated live on kind: three
  OOMKilled restarts (exit 137) within 97s of apply, then a held `Running` 1/1
  Ready steady window, with `kubectl get -o yaml` and `kubectl logs` grepping
  clean of every self-describing string.

## Remaining

- Record the three clips against the staged world. **Recording requires
  re-staging**, so the live specs match the neutralized manifests — a cluster
  staged before this change still serves the old self-describing `args` (and a
  second copy of them in the `last-applied-configuration` annotation) out of the
  API, which is exactly what the camera would catch.
- When staging to record, name the kind cluster something neutral. The cluster
  name is visible from inside the cluster as `pod.spec.nodeName` (today:
  `joe-demo-world-control-plane`), so the README's suggested
  `--name joe-demo-world` is the one remaining string that tells a reading agent
  it is looking at a demo. It is outside the manifests — an operator naming
  choice, not a fixture defect — but it defeats the same goal on camera.
- Encode the clips and commit them into the **joeagent.dev** repository (the
  landing-page site is a separate repo from this one); wire them into the landing
  page.
