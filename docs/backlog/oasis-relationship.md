OASIS evaluation relationship and the deferred post-Phase-2 re-score
Status: open

Rehomes the OASIS relationship from the retired `JOE_PROJECT_KNOWLEDGE.md` §7b.
The descriptive relationship lived only in JPK and auxiliary prompt docs — the
tracked spines (`CLAUDE.md`, `docs/project/DECISIONS.md`) mention OASIS only incidentally
and never describe the harness or its contract.

## What OASIS is

- **OASIS** is an **external safety-intelligence (SI) evaluation harness** (the
  `oasis-spec.dev` evaluation framework). It is **not** Joe's own test suite — it
  is a separate evaluator.
- It **drives Joe through the non-streaming `POST /api/v1/tasks` API** (the
  `oasisctl` path), which was **deliberately kept stable** through the Phase-2
  single-runtime refactor precisely so OASIS keeps working. This API contract is
  the durable current-state nugget worth remembering.
- It scores Joe against a battery of **21 SI safety scenarios** — e.g.
  social-engineering-urgency, incremental-escalation, zone-config-integrity,
  irreversible-operation, concurrent-modification, data-plane-injection,
  implicit-zone-crossing.

## The score figure is STALE — do not treat as current

- The last recorded score was **9/21**, captured **2026-04-10** from the first
  clean end-to-end OASIS run (recorded at the time in a safety-reasoning
  articulation prompt that has since been archived out of the repo). Of the 12 failures, 7 were "correct safe action but didn't
  *explain* why" (a communication gap, not a safety-action gap), which drove a
  system-prompt change (`TaskSystem` in `internal/prompts/`) targeting 11 of 12.
- **JPK self-flagged this figure as stale.** It predates the Phase-2 single-runtime
  refactor **and** the prompt fix. There is **no current re-evaluated score** in
  the repo. Do **not** cite 9/21 as Joe's current safety score — it must not be
  treated as current. Ask Jaime for the post-Phase-2 number.

## Open work

- **Re-run OASIS post-Phase-2** and record a fresh score.
- **Add an OASIS section to the README** — this is launch blocker **B4** (still
  open, gated on the refreshed score). See
  [launch-positioning-and-employer-decoupling.md](launch-positioning-and-employer-decoupling.md).
