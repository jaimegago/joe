# Joe is near-useless without registered components — make that framing explicit
Status: open

## The framing gap

Joe only becomes useful once it has registered, armed components to read and
reason about. With an empty graph it can boot, authenticate, and run an agentic
turn, but it has nothing to say about any real system. The public docs do not yet
state this plainly up front: a reader can finish Overview without understanding
that registering at least one component is the difference between a governed empty
shell and a working copilot.

## What this asks for

Make "Joe needs registered components to be useful" an explicit, early framing
point — not something the reader infers. It belongs across:

- **Overview** — say it as part of what Joe *is*: a copilot over the systems you
  connect to it, near-useless until you connect one.
- **Quickstart** — already includes a Kubernetes register-and-promote step
  (D-0059); the surrounding narrative should name the principle, not just perform
  the step.

## Status

Stub only — flagged here, not acted on in the `component-registration-guide`
session. Pick up as a deliberate Overview/Quickstart framing pass.
