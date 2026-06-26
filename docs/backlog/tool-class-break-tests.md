Break-tests pinning tool action-class for the two currently-unpinned cases
Status: open

Two tools in the hardcoded classification (`internal/safety/tier.go`) carry an
action class that **no test asserts**. The project principle is that
structural/governance invariants are break-tested, not inspected — a class is an
invariant the safety gate depends on, so an unpinned class is a silent-regression
hazard: a refactor (or a careless rename/reorder of the registry) could flip the
class with the full suite still green. This item adds the two missing class
break-tests to `internal/safety/tier_test.go`.

This is **code-lane work**, distinct from any documentation sweep: the fix is new
test assertions in the safety package, not prose. It touches only the test file.

## Gap 1 — `detect_doc_drift` is Read, but nothing pins it

`detect_doc_drift` is classified `ActionRead` at `internal/safety/tier.go:178`.
It does not appear in any assertion in `internal/safety/tier_test.go` — it is
absent from the known-tools class table (`TestClassifyTool_KnownTools`) and from
every other class-pinning test.

Why the absence matters: a refactor could silently flip it Read→Mutate with no
test failing. As a Read it runs unconditionally; promoted to Mutate it would be
denied by default (it has no policy key) and would stop running — a silent
capability regression for the drift-detection path, invisible to the suite.

Proposed fix: add `{"detect_doc_drift", ActionRead}` to the known-tools class
table in `TestClassifyTool_KnownTools`.

## Gap 2 — the `publish_doc_update` family is Mutate, but no test pins the CLASS

The publish family is classified `ActionMutate`:

- `publish_doc_update_confluence` — `internal/safety/tier.go:229`
- `publish_doc_update_notion` — `internal/safety/tier.go:230`
- `publish_doc_update_git` — `internal/safety/tier.go:231`
- `publish_doc_update` (bare) — `internal/safety/tier.go:233`

No test pins their class as Mutate. The only appearances of any of them in the
test file are `publish_doc_update` and `publish_doc_update_git` inside
`TestClassifyTool_IdempotentToolsAreNotDurable` — and that test asserts
`NeedsDurability == false` (durability), not the action class. `_confluence` and
`_notion` are not referenced by any test at all.

Why the absence matters: a refactor could silently flip the family Mutate→Read.
As Mutate they are denied by default and gated by the act policy, with the
before/after human notification on execution; reclassified to Read they would
skip the act-policy gate and the before/after notification entirely — an external
publish (to Confluence/Notion/Git) executing unannounced and ungated, with no
test failing.

Proposed fix: add an `external-publish-is-mutate` break-test mirroring the
existing `TestClassifyTool_ExternalCommentsAreMutate`
(`internal/safety/tier_test.go:131`), asserting each of the four publish tools
classifies as `ActionMutate`.
