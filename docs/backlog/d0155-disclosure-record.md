D-0155 (j) misdescribes which residual bounds reach the caller

Status: open
Priority: next

`docs/project/DECISIONS.md`, in the D-0155 entry, states that the two residual
work bounds "are NOT reported to the caller, and that is the second limb of the
rule rather than an omission", recording both as audit-row fields.

`evidence_bounded` is audit-only and that half is true. `matches_bounded` is not:
the same boolean reaches the caller as `matches_truncated` in
`internal/tools/core/resolvecomponent.go`, and the tool description instructs the
model to act on it.

**The code is right and the record is wrong**, which is the direction that
matters. Disclosing `matches_truncated` is safe — but not for the reason the entry
gives. It is safe because the component registry behind matching is ungoverned:
`ListComponents` returns `c.components.List(ctx)` with no accessor, and `Match` is
deterministic on name and type, so any caller can already compute the overflow
from `list_components`. That argument appears in the Go doc on
`Resolution.MatchesTruncated`. It appears nowhere in D-0155.

So the ratified record both misdescribes the posture and omits the dependency the
posture rests on. That dependency is live rather than theoretical: the ungoverned
registry read is open, and a later change that governs `Store.Components.List`
turns `matches_truncated` into a leak the moment it lands — while D-0155 (j) tells
that reader the flag is not disclosed to the caller at all.

## The work

- Correct D-0155 (j) to state what is true: `evidence_bounded` is audit-only;
  `matches_bounded` is on the audit row **and** returned to the caller as
  `matches_truncated`.
- Record the dependency the disclosure rests on — the ungoverned registry read and
  the determinism of `Match` — so the posture's basis is in the ratified record
  rather than only in a Go doc.
- Name the trigger: governing `Store.Components.List` makes `matches_truncated` a
  leak, so a later reader finds the consequence rather than having to notice it.

## Out of scope

Changing the code. The code is right; a change that "fixes"
`resolvecomponent.go` to match the false entry has inverted the defect. Governing
`Store.Components.List` is also out of scope — naming the trigger is not arming it.
