Component-resolve bound overrides are documented as narrowing-only and are not clamped

Status: open
Priority: next

`internal/access/componentresolve.go:126` documents the two override fields on
`ComponentResolveRequest` as narrowing only:

> a value above the ceiling is clamped to it, so a caller may narrow an answer and never widen one

`perCandidateShare` does not clamp. It computes the derived share and then
replaces it:

```go
if override > 0 {
    share = override
}
```

The only clamp in force is `graph.DefaultComponentBindingLimit` (50), which is not
the derived share. With the default budget and 25 candidates the derived share is
10, so an override of 50 yields 50 and one call can return up to 25 × 50 = 1250
bindings — the unbounded product D-0155 (j) records this mechanism as replacing.
Separately, `budget := req.TotalBindingBudget` carries no upper clamp against
`MaxResolveBindings`.

The result struct documents its three fields as "the bounds actually in force for
this call". With an override set they are not: a call can report
`TotalBindingBudget: 250` while returning up to 1250 bindings, or report 5000
while returning at most 1250.

## Severity — latent, not live

`ComponentResolveRequest` is `internal/`. Production passes zero for both override
fields and all seven test call sites narrow, so nothing a caller receives today is
wrong. What is wrong is that the struct carrying the bounds asserts a safety
property its own function does not enforce.

Filed rather than fixed inline because the mechanism shipped in #44 and the
remedy is small and local, not urgent.

## The work

- Enforce the documented contract: clamp an override to the derived share instead
  of replacing it, keeping the existing `DefaultComponentBindingLimit` clamp.
- Clamp `req.TotalBindingBudget` against `MaxResolveBindings`.
- Make the result struct's three fields true as documented, or correct the doc —
  but do not leave the two disagreeing, which is the defect.
- Pin each with a test verified to fail against the current code: an override
  above the derived share does not widen; a total budget above `MaxResolveBindings`
  does not widen; the reported bounds match what was enforced.

## Out of scope

The ungoverned component-registry read (`Store.Components.List` has no accessor).
It was out of scope for the change that shipped this mechanism and stays out.
