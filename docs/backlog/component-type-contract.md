Per-type component contract — one deterministic package per component type
Status: open
Priority: later

Design frame: each component type (gitlab, kubernetes, grafana, postgresql,
kafka, and so on) gets its own deterministic code package satisfying a uniform
contract with five elements.

One, type-specific config granularity declared at registration (for example
postgresql: database, table, query classes; kubernetes: API resource groups such
as deployments or policies) with Read or Mutate exposed per granular level to
RBAC for the human operator.

Two, a matching tool surface the LLM must use to interact with the component,
aware of each granular object — the per-type tool families in
`internal/tools/default.go` are the informal precursor; formalizing them is the
ambitious version of `adapter-dispatch-consolidation`.

Three, a placement in the knowledge graph committed at registration, with a
temporary placement permitted during discovery — interacts with the
inert-registration posture (D-0029) and needs its own design pass.

Four, a type-owned usefulness hint available to the agent loop — the
declared-semantics counterpart of the derived hints `component-resolution-tool`
exposes; `component-description-field` is blocked on this frame so a free-text
field is not locked in where structured type-owned content should live.

Five, a component-addressed interaction invariant: every managed-system
interaction goes through the guarded accessor bound to a registered component; the
unbound shared diagnostics tools (`tcp_connect`, `port_scan`, `dns_lookup`,
`http_request`, `traceroute`) are the named exception and will need their own
security layer — component-derived scoping and always-ask-permission are both
open options; see `governed-connectivity-check-surface`.

Constraints the contract must respect: the safety layer is deliberately
type-blind (tier by tool name, floor by boot booleans, per D-0021) — granular RBAC
refines the RBAC key, never makes the floor or tier classification type-aware; and
per-granularity RBAC belongs with the `full-mode-rbac-track` design work. The
guard seam is one-adapter-per-component by Go type assertion
(`internal/access/access.go` guard), so a type owning multiple tool surfaces (see
`repo-registration-path` option two) is a structural question the contract must
answer.

This frame is deliberately later: per the vertical-slices principle it should be
designed against operational evidence from `component-resolution-tool` and
`repo-registration-path`, not speculatively.
