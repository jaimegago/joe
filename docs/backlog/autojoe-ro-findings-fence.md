# The first live Auto Joe run persisted no findings: the model fenced its block as `json` and typed its findings `refusal`

Status: open

Observed 2026-10-28 on a local joe at `3609225`, Gemini 2.5 Flash, the first
Auto Joe run triggered by an operator over two registered components. The run
completed in 15 s inside both ceilings, and its final turn carried a findings
block — two findings with `operator_action` proposals — that the pipeline
never read. The run's record says `completed`; the Findings page says "No
runs yet".

## What happened

- The output contract asks for one fenced block whose info string is
  `joe-findings`, with `type` one of `issue`, `risk`, `graph_update`
  ([internal/runfindings/schema.go:69](../../internal/runfindings/schema.go#L69)).
  The model opened its block with ` ```json ` and typed both findings
  `refusal`.
- `SplitFindingsBlock` matches the info string exactly and skips every other
  fenced block
  ([schema.go:101](../../internal/runfindings/schema.go#L101)), so it found
  nothing, and the pipeline returned `ErrNoFindingsBlock`
  ([internal/api/autorun_findings.go:53](../../internal/api/autorun_findings.go#L53)).
  No finding set was written; the turn carries "(The findings pipeline
  failed: the pass emitted no joe-findings block.)" under `## Pass
  (unverified)`.
- Had the fence matched, `Establish` would have dropped both findings for the
  type, with the reason recorded
  ([internal/runfindings/establish.go:117](../../internal/runfindings/establish.go#L117)),
  and persisted an empty set.
- `GET /api/v1/run-findings` lists finding **sets**, not runs
  ([internal/api/runfindings.go:80](../../internal/api/runfindings.go#L80)),
  so a run that completed and persisted nothing is invisible to the Findings
  page, which renders the same empty state as if no run had ever happened
  ([ui/src/pages/FindingsPage.tsx:52](../../ui/src/pages/FindingsPage.tsx#L52)).
  The only operator-visible trace is the session the run created.

The run also read nothing: both components had failed to connect at boot and
`svc:agent:core` was denied `no_grant` on the unassigned zone — the
condition `oasis-refresh-principal-no-grant.md` already carries. That is why
the model had only refusals to report, but it is not why nothing was
persisted; a compliant block of two refusals would have been dropped on type
just the same.

## Why the tests did not catch it

They pin this exact outcome. `TestRunFindingsPipeline_NoBlockIsAnErrorAndPersistsNothing`
and `TestAutoRun_FindingsSeam_NoBlockKeepsTheRunAndRecordsNothing`
([internal/api/autorun_findings_seam_test.go:183](../../internal/api/autorun_findings_seam_test.go#L183))
assert that a pass without the block completes, persists nothing, and carries
the error on the turn — which is what happened. Every test that feeds the
parser a block fences it `joe-findings`; none feeds a block with another info
string, and none exercises a real model against the contract, because the
deterministic tiers cannot: the harness's scripted responder answers with
whatever the scenario wrote, and the OASIS evaluation is the only tier that
runs a model and it is not in the gate.

So the mechanism behaved as specified, and the specification was never run
against the thing it specifies. Two things were untested, and only the second
is testable deterministically:

1. Whether the configured model follows the fence instruction and the type
   vocabulary. It did not, on the first try.
2. What the operator sees when it does not. Nothing, on the Findings page.

## Candidate fixes

- **A tolerant extractor.** When no `joe-findings` block is present, take the
  last fenced block, whatever its info string, whose body parses as an object
  with a `findings` array. The contract stays as written; the parser stops
  depending on an info string a model can get wrong while getting the body
  right. Keep "no block at all" an error, as the schema comments require, and
  add the mis-fenced case to `schema_test.go` and the seam tests.
- **Surface the run, not only its set.** A completed run with no finding set
  — pipeline error, or every candidate dropped — should appear on the
  Findings page with its outcome and the reason, so the empty state means
  "no run" and nothing else. `auto_runs` already carries the state and
  the session; the endpoint and the page have to list runs. This is the
  half that owes a deterministic UI spec in joe-oasis-e2e's `ui-suite/`
  (`autojoe-ro-e2e-backfill` covers the happy path only).
- **Treat a dropped type as a signal.** A `refusal`-typed finding is the
  model telling the operator it could not read the component. Whether to
  admit a fourth type for it, or to map it onto the Limits section's "not
  looked at" list, is a design question; dropping it silently is the one
  answer that is wrong.
- **One real-model pass before a contract-bound feature is called working.**
  Not a gate change — a model's compliance is not deterministic — but a
  step in the order of any feature whose output contract a model has to
  honour, recorded in the thread's report with the model and the outcome.
