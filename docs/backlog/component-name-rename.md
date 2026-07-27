# Component Name rename — no mutation path exists

Status: open

Discovered during the component-id-validation session (D-0147), which made the
registration dialog derive the Component ID from the Name: once registered, a
component's **Name has no mutation path anywhere** — the store's `Update` method
exists with zero production callers, and no HTTP endpoint, agent tool, or CLI
command edits a registered component's name. The ID is immutable **by design**
(it is a load-bearing identifier: URL path segments, RBAC zone-assignment keys,
graph node stamps, audit targets); the Name being immutable is merely a gap.

A future governed name-rename endpoint would need:

- an admin-gated HTTP surface in the shape of the other governed component
  mutations (`mutateWithAudit`, same-transaction audit row);
- **its own audit event** — none of the existing `audit.Action*` values
  describes a rename, and reusing the registration action would misrepresent
  the operation;
- no ID or graph implications — the rename touches the `components.name`
  column only.

Until then, the only way to fix a typo'd Name is delete-and-reregister, which
cascades graph state (D-0117) and costs the component's sync history.
