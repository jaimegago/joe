Falco refresher — graph visibility for a fully wired type
Status: open
Priority: later

falco is registrable, credential-wired, and has working loop tools (`falco_alerts`,
`falco_rules`), yet [internal/coreagent/refresh.go](../../internal/coreagent/refresh.go) has no
case for it. A registered falco component is therefore graph-invisible: no anchor node, no
edges, unreachable by graph-based component resolution.

Candidate shape: an anchor node plus `alerts_in`-style edges derived deterministically from
alert payload namespace and pod fields — the same derivation pattern the telemetry refreshers
use for scrape targets.

## Note in passing — the stale `.joe/` discovery comments

While in the area: three refresher comments claim edges are discovered from repo-relative
`.joe/` files, and no such discovery code exists anywhere in the production tree.

- [internal/coreagent/alerting_refresh.go:201](../../internal/coreagent/alerting_refresh.go#L201)
  — "dashboard_in edges are discovered via .joe/ files" (grafana).
- [internal/coreagent/registry_refresh.go:190](../../internal/coreagent/registry_refresh.go#L190)
  — "Explicit edges are expected to come from .joe/ file processing."
- [internal/coreagent/datastore_refresh.go:139](../../internal/coreagent/datastore_refresh.go#L139)
  — same wording.

A repo-wide search for `.joe/` across non-test Go returns only home-directory paths
(`~/.joe/config.yaml`, `~/.joe/skills/`, `~/.joe/joe.db`, and the like); there is no
repo-relative `.joe/` reader. Correct all three comments, or build the discovery — noted here
rather than filed as a separate item.
