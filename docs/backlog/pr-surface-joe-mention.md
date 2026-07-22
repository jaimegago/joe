PR surface: an @joe mention triggers a change-impact assessment
Status: open
Priority: later
Blocked-by: change-impact-analysis

An `@joe` mention on a pull request or merge request triggers an impact assessment,
posted back as a comment on that PR/MR.

This is a **new inbound surface with its own auth story**, per D-0056. Webhook
signature verification and the mapping from a provider identity to a Joe principal
are external design work belonging to this item — and are **never** to be conflated
with the MCP consumer's auth, which resolves a service principal from a configured
key and answers a different question entirely.

The raw material exists at adapter level: `GetPRDiff` supplies the input, and
`PostComment` / `RequestChanges` supply the reply, on both providers
(`internal/adapters/github/adapter.go:171,200,210`;
`internal/adapters/gitlab/adapter.go:195`). What is missing is the inbound half —
receiving the event, authenticating it, and resolving who is asking.

This surface is the **expected first parsing consumer** of an assessment: a comment
body has to be assembled from the result rather than read by a human. The typed
input contract and structured output schema that D-0140 deliberately deferred
therefore land **here**, driven by a consumer that actually needs them.
