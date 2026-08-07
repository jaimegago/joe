Component description field — operator-declared semantics on the component record
Status: open
Priority: later
Blocked-by: component-type-contract

`store.Component` (`internal/store/models.go`) models no Description, annotations,
environment tag, or any operator-authored semantic field — only Name is free
text, unconstrained and unenforced, and `list_components` surfaces only id, type,
and name to the LLM. A declared-semantics field ("Prometheus scraping the prod
EKS cluster") surfaced through `list_components` would convert name-string
guessing into reliable routing for everything graph edges cannot reach.

Blocked on `component-type-contract` element four: the contract may make the
usefulness hint type-owned and structured, and a free-text blob committed now
could lock in the wrong shape.

When unblocked, scope includes the store model, both registration surfaces, the
UI form, and the `list_components` tool surface.
