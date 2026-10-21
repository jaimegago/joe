Agent tool reference — every tool the agent loop can call, with its action class
Status: open
Blocked-by: tool-class-break-tests

The public docs explain the Read/Mutate model (`docs/public/concepts/action-model.md`)
but nowhere list the tools the agent can call or which class each one carries. An
operator deciding what to register, or what a zone permits, has to read
`internal/safety/tier.go` to find out. Raised in the maintainer's notes (joe-pm
notes entry `pnnno.02`).

The work: a reference page under `docs/public` listing each tool registered on the
agent loop with its action class, derived from `internal/safety/tier.go` rather than
written by hand, so the page cannot drift from the classification.

**Why it is blocked.** `tool-class-break-tests` found 58 Read rows with no
break-test pinning their class. Publishing the table before they are pinned would
put an unpinned claim on the public surface — the defect `site-claims-ship-trigger`
exists to catch. Once the classes are pinned, the reference inherits that pin.

Related: `advertised-dead-tools` (tools advertised that no registrable component can
serve — the reference should say which those are, or follow that item's outcome),
`capability-map` (the Concepts page this would sit beside).

Priority is left untriaged for the maintainer.
