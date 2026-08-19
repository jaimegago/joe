# Zone scope with no assigned components tells the model it cannot act, and enforces nothing

Status: open
Priority: next

When a task request carries `allowed_zones` but **no component is assigned to any
of those zones**, the system prompt asserts a total restriction that no code
implements.

## What happens

`resolveZoneScope` in `internal/api/tasks.go` builds its allowed-component list by
walking zone assignments and keeping the ones whose zone is in the request's
`allowed_zones`. When none match, the list is a nil slice. Two things then diverge:

- **The prompt claims a total block.** `BuildZoneScopePrompt`
  (`internal/prompts/zones.go`) takes its `else` branch and appends: *"No components
  are assigned to your authorized zones. You cannot execute any component-scoped
  operations."*
- **The executor installs no restriction.** `buildTaskRun` appends
  `tools.WithAllowedComponents` only `if zoneScope.allowedComponentIDs != nil`, so
  on the nil slice the option is never applied. `internal/tools/executor.go`'s
  zone-scope check is guarded by `if e.allowedComponents != nil` and is therefore
  skipped entirely — every component-scoped tool call passes that check.

The divergence is safe in the enforcement direction: per-component RBAC still gates
each read, and the write floor still gates each mutate. What is wrong is the
**claim**. The prompt states a capability boundary that nothing behind it maintains.

## Why it is worth fixing rather than tolerating

The clause is a stopping instruction, and a stopping instruction with no enforcement
behind it is the exact failure shape D-0101 was a live incident about: wording that a
model followed literally, deferring reads it was fully permitted to perform. That
lesson is pinned twice in the prompt package —
`internal/prompts/posture_test.go` forbids the observation posture from reading as a
reason to stop, and `internal/prompts/tasksystem_resolve_test.go` forbids the
resolve-before-acting rule from reading as a wall. The zone-scope `else` branch is
stronger than either: it is a flat statement that **no** component-scoped operation
can be executed.

It also cuts directly across the resolve-before-acting rule sitting above it in the
same assembled prompt. `TaskSystem` tells the model to call `resolve_component` on a
phrase before using it as a `component_id`, and that an empty result is an answer
rather than a wall. The zone-scope section appended after it can tell the same model,
in the same breath, that it cannot execute any component-scoped operation at all.

The branch is not exotic. A newly registered component lands in the default
`unassigned` zone, so any task naming some other zone resolves to zero components and
takes this path.

## What is open

The wording fix and the enforcement fix are different changes and only the first is
settled as desirable:

- **State the fact without the capability claim** — say that no component in the
  authorized zones is known, and leave what the model may attempt to the tools and
  their errors. Prompt-only.
- **Or install the empty allow-list** so enforcement matches the claim — drop the
  `!= nil` guard so an empty resolved scope denies every component-scoped call. That
  is a change to permission logic and should be taken deliberately rather than as a
  side effect of a wording pass.

Doing the second without the first leaves the stopping instruction in place; doing
the first without the second leaves a scope the operator asked for unenforced. Pick
one on purpose.

## What is not established

Whether any deployment or evaluation run has actually reached the branch. It requires
`allowed_zones` naming zones that hold no assignments, and nothing asserts on the
assembled prompt, so the case would pass unobserved.
