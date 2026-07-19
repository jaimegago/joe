# Landing-page demo clips — record, encode, and commit to joeagent.dev

Status: done — all three clips recorded, encoded, and published to joeagent.dev with
captions; the landing-copy bends and the clip playback experience landed with them. The
standing claims the clips publish are registered in `docs/project/SITE-CLAIMS.md` under
Landing page → "Demo clips".

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

- All three clips recorded against a re-staged world (so the live specs matched the
  neutralized manifests rather than serving the old self-describing `args` out of the
  API, including the second copy carried in the `last-applied-configuration`
  annotation), on a neutrally-named kind cluster (the cluster name is visible from
  inside the cluster as `pod.spec.nodeName`, so the README's suggested
  `--name joe-demo-world` was the last remaining string telling a reading agent it was
  looking at a demo).

- All three encoded to `mp4` + `webm` and committed into the **joeagent.dev**
  repository (the landing-page site is a separate repo from this one) as
  `static/media/feature-{chat,graph,mcp}.{mp4,webm}`, wired into the landing page's
  "What Joe does" feature rows with captions.

- Landing copy bent to match what the footage actually shows:
  - The unsupported **p95-latency prompt chip** was removed from the first beat — it
    claimed a capability the clip does not demonstrate.
  - The **third beat's body copy** was reworded from "live infrastructure graph" to
    "live view of prod" (and its caption correspondingly from "queries Joe's live graph
    over MCP, and answers from prod state" to "queries Joe over MCP, and answers from
    live prod state"): the recorded session reaches prod state through Joe's MCP tool
    surface generally, not specifically through a rendered graph.
  - A **web-UI positioning sentence** was added to the hero subtitle ("You operate Joe
    through its web UI — chat, infrastructure graph, and governance controls in one
    place"), since the clips show the web UI and the prior copy never said Joe has one.

- Clip playback experience shipped: inline clips play **once, no loop**, and open into
  a readable **overlay** with native controls dropped; the overlay opens **from zero**
  rather than continuing from the inline playhead.

## Registered claims

The standing public claims the clips publish are entered in
`docs/project/SITE-CLAIMS.md` under Landing page → "Demo clips", discharging the
register's bidirectional duty. They are the register's only **recording-bound** entries:
their mechanism is footage rather than code, so no Go test can pin them and re-recording,
not a repository change, is what invalidates them. Each entry states its own invalidation
trigger in place of a pinning test.

Note the asymmetry the register records: feature-chat and feature-graph carry the model
(`gemini-2.5-flash`, framed as budget-tier) **in the caption**, because it is not visible
in frame; feature-mcp's model (`claude-opus-4-8`) is **frame-visible**, so its caption
names no model and asserts the grounding path instead.

## Follow-on

- `docs/backlog/demo-runbooks-e2e.md` codifies these three clip runbooks as end-to-end
  flows in the `joe-oasis-e2e` harness, so the paths the clips walk stay pinned after
  launch. That is the closest thing to a guard these claims can have, and it is tracked
  separately rather than held open here.
