# Joe — Decisions

Append-only project decision log. Newest entries at the top. Each entry
records what was decided, the basis (verifiable source, not assertion), and
what it supersedes. This file is normative: where a decision here conflicts
with prose elsewhere, this file states the project's position and the
conflicting prose is stale.

Format per entry: ID, date, decision, basis, supersedes, status.

---

## D-0002 — Phase 0 session model is the normative session-model design

- Date: 2026-02-16
- Decision: `docs/PHASE-0-SESSION-MODEL.md` is the normative output of the
  refactor's Phase 0. Phase 1 implementation is built from it. Its
  accompanying state diagram is a comprehension aid; where the document and
  the diagram disagree, the document governs.
- Scope closed by it: the original six refactor open questions; incident mode
  (emerged during design, load-bearing); the authority-layer integration
  verified against current code (§G of that document).
- Basis: PHASE-0-SESSION-MODEL.md (closed); Security & LLM-Execution
  Architecture Findings (code verification).
- Supersedes: nothing — first normative session-model artifact.
- Status: active. Phase 1 references this document, not chat history.

---

## D-0001 — Refactor Phase 3 deleted; refactor is two phases

- Date: 2026-02-16
- Decision: The agentic-runtime refactor collapses from three phases to two.
  Phase 3 ("collapse the /tasks loopback HTTP-to-self") is **deleted**, not
  merged or rescoped. The end-state acceptance criteria formerly nominal
  under Phase 3 are reattached to Phase 2 as Phase 2's completion gate. No
  work is lost; no work is created.
- Why delete (not merge/rescope): Phase 3's entire scope was collapsing a
  core→core HTTP loopback. Code verification found that loopback does not
  exist (joe-core's Core Agent uses its own `*tools.Registry` directly and
  does not call joe-core's own HTTP API). Merge implies residual Phase 3
  content to fold in — there is none. Rescope would invent work to preserve a
  phase number — the consistency-for-its-own-sake failure mode the project
  rejects (cf. PHASE-0-SESSION-MODEL.md R5). The single-agentic-runtime end
  state is reached as a consequence of Phase 2 (removing the CLI's own loop +
  LLM adapter), with no further step.
- Basis: Security & LLM-Execution Architecture Findings §4;
  PHASE-0-SESSION-MODEL.md §G1/§G2. The phantom phase is recorded here so a
  future reader does not rediscover the absent loopback and re-add a Phase 3
  to "complete" the sequence.
- Supersedes: the prior plan-of-record's "sessions → migrate REPL → collapse
  loopback" three-phase sequencing. The reconciled plan-of-record
  (`docs/PLAN-OF-RECORD-RECONCILED.md`, §5 Reconciliation Record) is the full
  audit trail; this entry is the index pointer to it.
- Status: active. The original plan-of-record file carries a supersession
  header pointing here and to the reconciled plan.
