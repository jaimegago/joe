Structural AST guards walk only a handler's direct body — one is already vacuous

Status: open
Priority: next

The project pins structural and governance invariants with break-tests rather
than review, and a family of those tests walks a single handler's AST looking for
forbidden calls. Three of them carry the same sentence: "A reviewer can be fooled;
the AST cannot."

For at least one, that sentence is now false. The guard is green, and the
invariant its failure message asserts does not hold.

The pattern is the defect. `ast.Inspect(handler.Body, ...)` pins a property of
**that function's text**, while the failure message asserts a property of the
**behavior reachable from it**. The two coincide only until someone moves the
forbidden call one level down into a helper. Nothing then goes red; the guard just
stops guarding.

## Confirmed vacuous — `TestPromote_NoResolution`

`internal/api/components_promote_governance_test.go:391-431`. It parses
`components.go`, finds `handlePromoteComponent`, walks its body, and fails if it
finds any of `Connect`, `Resolve`, `Probe`, `Select`, `newAdapterForType`
(`:408`). Its message: "promotion must perform NO credential resolution (no
Connect/Resolve/Probe/Select/adapter build). It writes a reference; connectivity
is a separate explicit admin Probe."

Promotion does all five, one level of indirection away:

- `handlePromoteComponent` calls `s.connectAndRegisterAdapter` —
  `internal/api/components.go:883`.
- `connectAndRegisterAdapter` calls `newAdapterForType` (`:200`) and
  `adapter.Connect` (`:204`).
- `Connect` resolves the credential — `credential.Select` and `provider.Resolve`
  at `internal/adapters/github/adapter.go:86-97`, and the same shape in the peer
  adapters.

The walk only examines `handler.Body`, and `s.connectAndRegisterAdapter` is a
selector whose name is not in the forbidden set, so the test passes.

**This is not a request to restore the invariant.** D-0119 introduced the eager
connect-and-register at promotion deliberately, and the maintainer has ruled that
it supersedes the no-resolution invariant: promote-time connect is intended
behavior. The defect is that the guard was left asserting the superseded rule and
kept passing, so nothing recorded the supersession. Reverting D-0119 is explicitly
out of scope.

The consequence is live rather than hypothetical.
`docs/backlog/adapter-dispatch-consolidation.md` widens promote-time credential
resolution from 14 registrable types to all 19, and this guard is the test a
reader would consult to find out whether that is allowed. It would tell them it is
forbidden, and it would be wrong.

## Confirmed defeatable, not yet defeated

Same pattern, same copied sentence, currently accurate only by luck:

- `TestCreateComponent_NoConnectProbe` —
  `internal/api/components_governance_test.go:194-236`. Forbids `Connect` and
  `newAdapterForType` in `handleCreateComponent`'s body. The invariant it protects
  is real and load-bearing: a credential-less record cannot authenticate, so
  connecting at registration is described in the test itself as the
  attacker-controllable network-call / env-dereference vector.
- `TestPromotionCandidates_SeamHeldNoEnvInHandler` —
  `internal/api/components_promotion_candidates_test.go:210-246`. Forbids
  `os.LookupEnv`, `os.Environ` and `os.Getenv` in
  `handleComponentPromotionCandidates`' body, requiring enumeration to be
  delegated through `credential.Provider.AvailableReferences`.

Either goes quiet if the forbidden call is moved into a helper in the same
package — the exact refactor that made the promote guard vacuous, and an
unremarkable thing for a future change to do.

## Untriaged — the rest of the family

`ast.Inspect(<fn>.Body, ...)` appears at 15 call sites across 11 test files. The
three above are triaged; the remaining sites are **not**, and this item does not
claim they are broken:

`internal/access/guard_test.go:107`, `internal/adapters/k8s/transport_break_test.go:186`,
`internal/api/admin_gate_guard_test.go:89` and `:127`,
`internal/api/admin_audit_guard_test.go:177`,
`internal/api/access_phaseb_test.go:160` and `:173`,
`internal/api/llm_admin_guard_test.go:187`, `:272` and `:297`,
`internal/api/skills_admin_test.go:318` and `:388`.

Triage them on one axis, because the failure directions are not symmetric:

- **Absence assertions** ("this body must not call X") fail **quiet** when the
  call moves into a helper. Silent hole. This is the dangerous direction and all
  three confirmed instances are of this kind.
- **Presence assertions** ("this body must call X") fail **loud** when the call
  moves into a helper. A false alarm is noisy and annoying, but it does not hide
  anything, and a guard that cries wolf is not the same defect.

Note that `internal/access/**` and `internal/rbac/**` are governance-class paths
under `docs/verification.md` and are held for maintainer approval by the
`Held Paths` check. Any change to guards living there is governance-floor work.

## Design questions to settle before a build order

This item is **not execution-ready**. What should replace the vacuous guard is a
genuine choice, and the options differ in cost and in what they can promise:

1. **Retire it.** Delete `TestPromote_NoResolution` and record the supersession in
   the D-0119 lineage. Cheapest, honest, and leaves promotion's credential
   behavior pinned by nothing structural.
2. **Rewrite it against the real invariant.** Decide what is actually true of
   promotion now — a candidate is that no resolution happens *before* the arm is
   committed, which the current ordering does satisfy (`components.go:863-887`) —
   and pin that instead. Requires agreeing the invariant before writing the
   assertion.
3. **Make the walk transitive.** Resolve intra-package calls and walk callees, so
   moving a call into a helper no longer hides it. Fixes the whole family at once
   rather than one test, but needs a call-graph helper and a decision on where to
   stop (package boundary? std lib?), and it will surface other already-vacuous
   guards — which is the point, and is also unbounded work until the triage above
   is done.
4. **Change the seam instead of the test.** Where an absence assertion is what
   matters, a type-level or interface-level constraint that makes the forbidden
   call unreachable from the handler is stronger than any AST walk. Not always
   available; worth asking per guard.

Whether the answer must be uniform across the family, or may differ per guard, is
itself part of the question.

## Acceptance criteria

- `TestPromote_NoResolution` no longer asserts an invariant that does not hold —
  by whichever of the routes above is chosen.
- The supersession of the no-resolution invariant by D-0119 is recorded somewhere
  a reader of the promote path will find it, not only in a deleted test.
- The 12 untriaged call sites are classified absence vs presence, with the
  absence-side ones either fixed or filed with their exposure stated.
- Any guard whose pattern is kept unchanged carries a comment saying what it does
  **not** cover, so the next reader is not told the AST cannot be fooled when it
  can.
- The three confirmed guards' shared "A reviewer can be fooled; the AST cannot"
  sentence is corrected or removed wherever it survives, since it is the claim
  that misleads.
- Out of scope: reverting D-0119; the adapter-dispatch consolidation itself; any
  change to promote's runtime behavior.
