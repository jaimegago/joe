Discovery-to-clarifications pipeline — unpark and finish the onboarding, facts, and needs-human surfaces

Status: open

## Context

The onboarding, clarifications, and manual-refresh HTTP surfaces were **parked for
launch** in the `discovery-clarifications-pipeline` session (D-0081). The routes are
left unregistered as reachable-but-orphaned code: the register functions, handlers,
`ClarificationService`/`ClarificationRepository`, `discovery.ProcessInput`, the facts
repository, the `save_onboarding_fact` tool, and all tables and migrations are
**retained untouched** — only the two `RegisterRoutes` call sites were removed, so
each parked path now returns `404`.

The surfaces were parked because the pipeline behind them is incomplete: the routes
exposed a half-built discovery/clarifications flow (see the audit below), so shipping
them would advertise HTTP endpoints that do not deliver a working feature. Parking is
route-level only; nothing in the safety, RBAC, or graph model changed, and the
autonomous background `Refresher` (launched from `Agent.Start`, independent of the
`/refresh` route) and the session-scoped findings routes are unaffected.

## Parked routes

- `GET  /api/v1/clarifications`
- `POST /api/v1/clarifications/{id}/answer`
- `POST /api/v1/clarifications/{id}/dismiss`
- `POST /api/v1/onboarding`
- `POST /api/v1/refresh`

## What is missing to finish the feature

Per the audit, the pipeline is incomplete on four fronts:

- **A producer that enqueues clarifications.** Today nothing on the autonomous path
  writes a clarification. The refresh loop's *Needs-Human* branch — the step that
  would queue an ambiguous finding for human resolution — is a stub, so the
  clarifications table is only ever populated by the (also-parked) onboarding/discovery
  entry points. A real producer is required for the feature to have content.
- **A reader for `onboarding_facts`.** The `save_onboarding_fact` tool and the facts
  repository can write facts, but there is no consumer that reads them back into the
  discovery flow or the graph. The facts are write-only dead weight until a reader
  exists.
- **An LLM-backed discovery flow replacing the `ProcessInput` stub.**
  `discovery.ProcessInput` is a placeholder, not a working inference step. Finishing
  the feature means replacing it with an LLM-backed flow that turns onboarding input
  and facts into graph deltas and clarifications.
- **A UI or MCP consumer.** There is no surface that lists pending clarifications,
  answers/dismisses them, or drives onboarding — neither in the web UI nor over MCP.
  Without a consumer the routes have no caller even once re-enabled.

## Re-enabling

Re-enabling is small and mechanical:

- Restore the two `RegisterRoutes` call sites in `internal/api/server.go`
  (`registerClarificationRoutes` and `registerControlRoutes`) — the register
  functions and handlers are retained.
- Revert the parked-contract tests (`routes_test.go`, `control_test.go`,
  `clarifications_test.go`) that currently assert `404` back to asserting live
  behavior.
- Revert the docs that mark the endpoints as parked (`docs/public/api-reference/`
  endpoints block and `docs/public/concepts/agent-loop-and-autonomy.md`).

Re-enabling the routes is necessary but not sufficient — the four missing pieces
above are what actually finish the feature.
