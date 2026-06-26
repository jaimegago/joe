# Backlog — The credential-rejection field list is not single-sourced with provider credential fields

Status: done — already closed in the live tree by D-0030; filed as a resolved/archived record, not open work.

## Why this is archived, not open

This item was queued (session `credential-reject-single-source`) to track a
correctness residual that D-0029 flagged as "single-source seam (flagged, not
closed)" (`docs/DECISIONS.md:538`). Phase-1 verification against the live tree
found the seam is **already closed** — both the create-time reject path and the
promote path now draw the credential-bearing-field set from one authoritative,
reflection-derived accessor. The work this item describes is therefore done, so
it is filed directly under `done/` rather than as an active row in
[`../INDEX.md`](../INDEX.md). No code change was made by this session; it records
the verified state.

## The risk (as originally framed)

At component creation, a registration must be credential-less by construction —
both the HTTP create path and the `register_component` LLM tool reject any
credential-bearing config field rather than silently stripping it, so an
agent/LLM-created component lands inert (no credential, read-only floor,
unassigned zone). That guarantee depends on the create-time reject set staying in
agreement with the set of fields the credential providers actually parse as
authentication. If the reject set were a separately maintained copy of the
provider field definitions, adding a new credential-bearing field to a provider
without also updating the reject set would let that field pass at creation —
silently re-opening a credential hole and undermining the credential-less-at-
creation guarantee that keeps agent/LLM-created components inert.

## Current state (as found in Phase 1) — single-sourced (closed)

The two paths are **fully single-sourced**, both consuming one authoritative
declaration:

- Authoritative source: `credential.CredentialBearingFields()`
  (`internal/credential/fields.go:54`) derives the set **by reflection** over the
  provider config structs (`discriminator`, `staticConfig`,
  `kubeconfigExecConfig`; `internal/credential/fields.go:39`), with an explicit
  `nonCredentialConfigFields` opt-out (`internal/credential/fields.go:28`). Any
  field added to a provider config struct that is not opted out is treated as
  credential-bearing by default — the safe direction.
- Create-time reject path: `componentgov.RejectCredentialFields`
  (`internal/componentgov/credentials.go:61`) checks against
  `credentialBearingFields` (`internal/componentgov/credentials.go:50`), which is
  initialized as `credential.CredentialBearingFields()` — no hand-copied literal.
  This is the one rule both the HTTP create path and the `register_component` LLM
  tool call, so those two surfaces cannot drift from each other either.
- Promote path: draws from the same accessor —
  `credential.CredentialBearingFields()` is referenced in `armedState`
  (`internal/api/components.go:736`) and `buildArmedConfig`
  (`internal/api/components.go:767`).
- Divergence guards: `credential.TestCredentialBearingFields_ExactSet`
  (`internal/credential/fields_test.go:16`) pins the derived set, and
  `componentgov.TestCredentialBearingFields_MatchCredentialPackage`
  (`internal/componentgov/credentials_test.go:18`) asserts the reject set matches
  the credential package.

## Decision trail

- **D-0029** (`docs/DECISIONS.md:477`, Status IMPLEMENTED 2026-06-16) built the
  credential-less registration boundary and recorded the single-source seam as
  *flagged, not closed* (`docs/DECISIONS.md:538`): at that point the reject list
  was duplicated from provider json tags on unexported structs, and D-0029
  prescribed the fix — export the field set from `internal/credential` (a
  `CredentialBearingFields()` accessor) and have `componentgov` consume it.
- **D-0030** (`docs/DECISIONS.md:381`, Status IMPLEMENTED 2026-06-16) closed the
  seam as its "commit-one prerequisite" (`docs/DECISIONS.md:447`): `componentgov`
  no longer hand-maintains its denylist and consumes
  `credential.CredentialBearingFields()`.

This backlog item is the tracked follow-up to D-0029's flagged residual. The
append-only log is internally consistent — D-0029 flagged the residual and D-0030
recorded closing it; the live tree matches D-0030. No DECISIONS change is
warranted, and none was made.

## Shape of the fix (implemented)

Requirements-level: both the create-time reject path and the promote-path
credential handling must derive from one authoritative declaration of
credential-bearing provider fields, so the two cannot drift and a new provider
credential field flows into both automatically (or must be consciously excluded
at the single source). This is exactly what the live tree implements via
`credential.CredentialBearingFields()` plus the two divergence guard tests.

## Posture

Non-blocking, not launch-gating — a correctness-hardening item, now resolved.
