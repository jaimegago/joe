An agentic-ask MCP tool — reach the agent loop from a coding agent
Status: open
Priority: next

`joe mcp` today exposes single-hop REST tools only: seven of them
(`internal/mcp/tools.go`), each a thin dispatch to one client call, with **no
access to the agent loop and no session creation**. A coding agent talking to Joe
over MCP can ask for a graph edge or a metrics query, but cannot ask Joe a
question that requires Joe to think.

(Housekeeping for whichever session next touches that file: the comment at
`internal/mcp/tools.go:5` says "all 8 Joe tools" and there are seven — the
knowledge-search tool went with D-0113 and the comment did not.)

The work: an MCP tool that **creates a session** under the resolved service
principal, **drives the same user-task agent loop** a web session drives, and
returns the synthesis. That makes any agentic ask — change-impact analysis
included — reachable from a coding agent without a second execution path.

The central design problem is **long-running call semantics**: MCP calls are
request/response and an agent loop is not. Streaming, polling with a handle, or a
hard timeout with a session link to follow — decide deliberately, this is the item.

Sessions created this way remain **team-public with a real creator principal**,
exactly as on every other surface. The surface does not get its own session
visibility rules.
