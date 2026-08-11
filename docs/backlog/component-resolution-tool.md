Component resolution tool — expose graph-edge component binding to the agent loop
Status: open
Priority: now

The agent-loop LLM today has no reliable way to map a task phrase like "app XYZ in
prod" to a `component_id`. The binding for telemetry, gitops, and registry
components already exists in the graph — refreshers derive `metrics_in`, `logs_in`,
`traces_in`, `alerts_in`, `paged_via`, `managed_by`, `provisions`, and
`image_stored_in` edges from live evidence such as Prometheus scrape-target job
labels (`internal/coreagent/observability_refresh.go`), each edge carrying the
component id — and the HTTP observe API already walks these edges to resolve a
backend from a service name (`resolveComponentForService` and
`resolveK8sComponentForService`, `internal/api/observe.go`), but that resolver is
unexported, welded to the HTTP handler and request-derived principal context, and
no agent-loop tool wraps it. The system prompt (`internal/prompts/prompts.go`
`TaskSystem`) carries no discovery guidance; only `prometheus_query`'s description
points at `list_components`.

Planned shape: extract the resolution logic into a shared seam both the HTTP
handler and a new loop tool consume, so the two surfaces cannot drift, following
the `componentgov` shared-seam pattern; expose it as a resolve-component tool; add
discovery guidance to the tool descriptions and system prompt.

Five design questions must be settled in chat before any build prompt: tool
contract (single answer vs ranked candidates with evidence edges; ambiguity and
no-match behavior — the HTTP 404 shape is wrong for a loop that should fall back
rather than dead-end); one generic resolver vs per-signal-kind routing (the observe
API keys each category to fixed relation constants with an alerts fallback chain
and a k8s type-walk special case); seam extraction shape preserving
RBAC-through-accessor on both callers; `TaskSystem` guidance scope (minimal
resolve-first rule vs full discovery strategy; posture-prompt history
D-0101/D-0104 applies); and the git non-story (git components have no edges — see
`repo-registration-path`).

Resolution for git-type components is out of scope until `repo-registration-path`
lands.
