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

## Remaining

- Record the three clips against the staged world.
- Encode the clips and commit them into the **joeagent.dev** repository (the
  landing-page site is a separate repo from this one); wire them into the landing
  page.
