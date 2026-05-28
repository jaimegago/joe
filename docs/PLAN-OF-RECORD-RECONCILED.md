# Joe Agentic-Runtime Refactor — Plan of Record (Reconciled)

Status: RECONCILED against code-truth. **Phase 2 is COMPLETE (2026-05-28)** —
the single agentic runtime exists; see the Phase 2 completion record below and
docs/DECISIONS.md D-0003. This supersedes the prior
"sessions → migrate REPL → collapse loopback" three-phase sequencing.
Supersession reason and the deleted phase are recorded in §5 Reconciliation
Record below; the change is auditable, not silent. Indexed in
docs/DECISIONS.md as D-0001.

Scope: this is the plan for establishing the single agentic runtime
(single LLM contact point, single agentic loop) that Phase 0's
PHASE-0-SESSION-MODEL.md §D depends on as a precondition. It is the
implementation sequencing, not the session-model design — that is closed
in PHASE-0-SESSION-MODEL.md and is not reopened here.

This is a planning document. No code changes are made by this document.

---

## 1. The code-truth that forced reconciliation

Verified against the Joe repository (Security & LLM-Execution Architecture
Findings, section 4; PHASE-0-SESSION-MODEL.md §G1/§G2):

- **Two LLM adapters exist.** `cmd/joe-core/main.go` instantiates one via
  `deps.newLLMAdapter`; `cmd/joe/main.go` instantiates another via
  `deps.newAdapter` (plus a second for model hot-swap).
- **Two agentic loops exist.** joe-core's Core Agent
  (`internal/coreagent/agent.go`) calls `a.llm.Chat()` directly and loops;
  the joe CLI's `useragent.Agent` REPL (`internal/useragent/agent.go`)
  calls `a.llm.Chat()`, executes tools, and loops independently in the CLI
  process.
- **No loopback-HTTP-to-self exists.** joe-core's Core Agent uses its own
  `*tools.Registry` directly (`internal/coreagent/agent.go`). It does
  **not** use `internal/client.Client` to reach joe-core's own HTTP API.
  The CLI connects to joe-core over HTTP for *core tools*, but that is
  CLI→core, not core→core. There is no core→core HTTP call to remove.

The prior plan of record's Phase 3 was scoped as "collapse the /tasks
loopback HTTP-to-self." That artifact does not exist in code. The phase
was scoped against a structure the codebase never had.

The single-LLM-contact-point / single-agentic-loop end state the refactor
exists to produce is real and not yet achieved — but it is achieved by
removing the **CLI's own loop + adapter**, which is Phase 2 work, not by
collapsing a loopback.

---

## 2. Reconciliation determination

**Does Phase 3 have any remaining content once Phase 2 removes the CLI
loop?**
No. Phase 3's entire scope was collapsing a core→core HTTP loopback. That
loopback does not exist, so there is no work item under it. Once Phase 2
removes the CLI's loop + adapter, the single-agentic-runtime end state is
reached as a consequence of Phase 2 completing, with no further step
required.

**Delete, merge, or rescope Phase 3?**
**Delete.** Merge implies residual Phase 3 content to fold into Phase 2,
and there is none. Rescope invents new work to justify keeping a phase
number — the "consistency for its own sake" failure mode the project flags
elsewhere (cf. PHASE-0-SESSION-MODEL.md R5). The honest action is to delete
the phase and record why.

**Does downstream sequencing/numbering need adjustment?**
Yes, minimally. The refactor collapses from three phases to two. The
end-state acceptance criteria nominally "Phase 3 done" are reattached to
Phase 2 as its completion gate (they were always the real test of the
refactor; they were mis-located under a phantom phase). No work is lost. No
new work is created. Nothing is sequenced after the deleted phase, so there
is no renumbering.

---

## 3. Reconciled plan of record

Two phases. Phase 1 unchanged. Phase 2 absorbs the end-state acceptance
gate. No Phase 3.

### Phase 1 — Session/run durable state (unchanged)

Establish the session and run model from PHASE-0-SESSION-MODEL.md as
durable, team-scoped state behind the persistence interface.

Scope is exactly as closed in PHASE-0-SESSION-MODEL.md (§5b; §D as
design-for-post-refactor-state; the incremental-autonomy seams). Phase 1
does not by itself collapse the two loops; it builds the substrate the
collapsed runtime will own. The §D run state machine is built as design
for the post-refactor state — this remains correct and is not affected by
the reconciliation.

Sequencing dependency: Phase 1 does not require the single-loop end state
to exist. It is built against the persistence interface and the session
model, both of which are loop-count-independent.

### Phase 2 — Collapse to a single agentic runtime

**Status: COMPLETE (2026-05-28).** Implemented per
docs/PHASE-2-IMPLEMENTATION-NOTES.md; protocol/boundary decisions recorded in
docs/DECISIONS.md D-0003. Completion evidence is summarized after the gate
below.

The substantive refactor. Remove the CLI's own agentic loop and its own
LLM adapter so that exactly one agentic loop and one LLM contact point
exist.

Work items:

- Remove the CLI's `useragent.Agent` agentic loop
  (`internal/useragent/agent.go`) as an independent loop. The CLI becomes
  a thin client that drives the single runtime in joe-core rather than
  running its own LLM-call/tool-execute/loop cycle.
- Remove the CLI's own LLM adapter instantiation
  (`cmd/joe/main.go` `deps.newAdapter`, including the hot-swap second
  adapter). After this, joe-core's adapter is the only LLM contact point.
- Re-route what the CLI loop did (interactive REPL behavior, model
  selection UX) onto the single joe-core runtime, exposed to the CLI over
  the existing CLI→core HTTP channel. Model hot-swap becomes an operation
  against the single runtime, not a CLI-local adapter swap.
- Ensure the joe-core Core Agent remains the single loop and continues to
  use its own `*tools.Registry` directly. No core→core HTTP is introduced
  while doing this (the absence of a loopback is a property to preserve,
  not a thing to build and then collapse).

Phase 2 completion gate (formerly the "Phase 3 done" criteria; they are
the real acceptance test of the refactor and now correctly sit here):

- Exactly one LLM adapter is instantiated process-wide. No client
  instantiates its own LLM adapter. (Invariant 6,
  PHASE-0-SESSION-MODEL.md.)
- Exactly one agentic loop implementation is reachable at runtime. A
  second concurrent loop activity is a regression. (Invariant 1,
  PHASE-0-SESSION-MODEL.md.)
- The CLI performs no LLM calls and runs no agentic loop of its own; it
  drives the single runtime.
- PHASE-0-SESSION-MODEL.md §D's "one agentic runtime, one loop"
  precondition is satisfied — §D is no longer design-for-a-future-state
  with respect to loop count; the substrate it assumes now exists.

There is no Phase 3. The refactor is complete when Phase 2's gate passes.

#### Phase 2 completion record (how the gate was met)

- **One LLM adapter process-wide.** The CLI no longer instantiates an adapter
  (`cmd/joe/main.go` removed `deps.newAdapter`, the instrumented adapter, and
  the hot-swap factory). joe-core's `services.LLM` (a `SwappableAdapter`) is the
  only LLM contact point. A guard test (`cmd/joe/guard_test.go`) asserts the
  CLI build closure links no adapter-factory or provider package.
- **One agentic loop reachable at runtime.** The loop implementation moved from
  `internal/useragent` to `internal/agentloop` (joe-core-owned) and is reached
  only via joe-core's task handlers. `internal/useragent` no longer exists; the
  CLI links no loop package.
- **The CLI is a thin client.** `internal/repl` streams joe-core's loop over SSE
  (`POST /api/v1/tasks/stream`), renders it, and services local-tool callbacks
  on the operator's machine (`/tasks/stream/{id}/tool-results`). It performs no
  LLM calls and needs no provider API keys.
- **`/model` works** end-to-end as an operation on the single runtime
  (`GET/POST /api/v1/models[/current]`, hot-swapping `services.LLM`).
- **Existing surface preserved.** `POST /api/v1/tasks` is unchanged (oasisctl),
  and `joe mcp serve` is unaffected (it uses category endpoints, not the loop).
- **Tested.** An end-to-end test (`internal/repl/repl_e2e_test.go`) drives the
  real thin REPL against a real joe-core server through a delegated local tool;
  the full suite passes.
- **§D precondition satisfied.** PHASE-0-SESSION-MODEL.md §D's "one agentic
  runtime, one loop" precondition now holds in code.

---

## 4. Sequencing relationship to PHASE-0-SESSION-MODEL.md

- Phase 1 (session/run durable state) builds the substrate. It does not
  require single-loop to exist and does not by itself produce it.
- Phase 2 (collapse to single runtime) produces the single-loop /
  single-contact-point end state.
- PHASE-0-SESSION-MODEL.md §D is correct as written: it is design for the
  post-Phase-2 state. The reconciliation does not change §D. It changes
  only which phase, and how many phases, deliver the precondition §D
  names — two phases, not three, with the end state delivered by Phase 2.

PHASE-0-SESSION-MODEL.md §G2's reconciliation instruction is hereby
discharged: verification was performed, Phase 3 has no remaining content,
and the disposition is deletion (not fold-in, per §2). §G2 is satisfied by
this document.

---

## 5. Reconciliation Record (audit trail)

| Item | Prior plan of record | Reconciled |
|---|---|---|
| Phase count (refactor) | 3 | 2 |
| Phase 1 | Session/run durable state | Unchanged |
| Phase 2 | Migrate/remove CLI REPL loop + adapter | Unchanged in intent; now carries the end-state acceptance gate |
| Phase 3 | "Collapse the /tasks loopback HTTP-to-self" | **Deleted** — scoped against a structure absent from code (no core→core loopback exists) |
| End-state acceptance criteria | Nominally under Phase 3 | Reattached to Phase 2 completion gate |
| Downstream numbering | n/a (Phase 3 was last) | No renumbering needed; nothing sequenced after the deleted phase |

Supersession basis: Security & LLM-Execution Architecture Findings
section 4; PHASE-0-SESSION-MODEL.md §G1/§G2. The deleted phase is recorded
here rather than removed without trace so a future reader does not
rediscover the phantom loopback and re-add a Phase 3 to "complete" the
sequence.

Repo-level index: docs/DECISIONS.md entry D-0001.
