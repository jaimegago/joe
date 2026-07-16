Demo runbooks as E2E tests
Status: open
Priority: later

Codify the three launch demo clip runbooks — **feature-chat**, **feature-graph**, and **feature-mcp** (see `docs/backlog/feature-clips.md`) — as end-to-end flows in the `joe-oasis-e2e` harness, so the paths the launch clips walk stay pinned after launch. Each clip is currently a manual recording procedure driven by hand against a live kind cluster staged from `examples/demo-world/`: nothing re-walks those paths on a change, so a regression in any of them surfaces only when someone next records or demos.

The three flows to codify, each against the staged `shop` world:

1. **feature-chat** — ask the chat surface why `orders` is restarting and assert the turn reaches a real answer citing the OOMKilled evidence (not a narration, not a truncation notice). This flow crosses the agentic loop's two live termination seams, both of which were bugs found by hand on this exact path: the unfulfilled-tool-intent probe (D-0103) and forced synthesis on iteration-cap exhaustion (D-0096–D-0100).
2. **feature-graph** — load the graph surface against the staged world and assert the `shop` object graph renders with the checkout → payments and checkout → orders dependency edges.
3. **feature-mcp** — drive Joe's tools over MCP (`joe mcp`) against the same world and assert the governed tool surface answers.

Deliberately deferred from pre-launch scope: the clips are recorded by hand once for the landing page, and building the harness flows was not on the critical path to launch. The value is post-launch — these are the paths a prospective user is shown first, so a silent regression in one of them is expensive in a way a unit-test gap is not.

Related: `docs/backlog/oasis-relationship.md` (the harness and the deferred post-Phase-2 re-score) and `docs/backlog/posture-prompt-conflation.md`, which defers a behavioral OASIS scenario for the same reason — unit tests pin the text or the unit, not the behavior on a live path. This item and that one share a harness and should be picked up together.
