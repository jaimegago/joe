# Joe — Phase 0 Session Model Design

Status: CLOSED. This document is the normative output of Phase 0. Phase 1
(Claude Code) implements this text. The state diagram that accompanies this
document is a comprehension aid only — where the diagram and this document
disagree, this document wins.

Scope note: this document specifies the **session model** for the
post-refactor architecture (joe-core as the single agentic runtime). Several
properties here are design-for-the-post-refactor-state, not descriptions of
current code. Where that distinction matters for Phase 1 sequencing it is
called out explicitly (see §G, Reconciliation with current code).

The original six open questions from the refactor plan of record are resolved
here, plus one concept (incident mode) that emerged during design and proved
load-bearing, plus the authority-layer integration verified against current
code.

---

## 0. System regime — the switch everything else is conditioned on

R1. Incident mode is a **system-level state**, not a session attribute. The
model has two regimes:

- **Normal**: no captain; sessions independent; mutation passes through
  ordinary per-session RBAC/safety gates. The simple model — sufficient here.
- **Incident**: captain exists; mutation through Joe is captain-session-only
  (§C); the participation model (§A) and authority model (§B) are in force.

R2. **Declare** incident mode: a human holding `can_declare_incident` at any
time; OR Joe, when it both holds `can_declare_incident` and the specific
signal/page is configured as Joe-actionable. Day-one: the human path is live;
Joe-authorization + per-signal config is a defined-but-inert entry-point seam.
Phase 1 builds the seam, not the judgment.

R3. Joe judges something is an incident but is **not** authorized to declare
→ no incident regime. The finding goes to the **Joe warnings surface** (§E)
for human triage. This is also the answer to noisy alerting: handled by
absence-of-authorization producing a warning, not by Joe modeling noise.

R4. **Resolve** incident mode: a human holding `can_resolve_incident` at any
time; OR Joe *proposes* resolution and a human disposes. Never unilateral-Joe
day one. Exit is never automatic and never by signal/time lapse.

R5. The declare/resolve asymmetry is **deliberate and ICS-correct**: entry may
be automated, exit may not. A future contributor will try to make resolve
mirror declare "for consistency" — that symmetry is the safety regression.
Invariant: *incident-mode entry may be automated; exit requires a human act
(day one) or at most Joe-proposes-human-disposes (later); never
unilateral-Joe, never lapse.*

R6. `can_declare_incident` and `can_resolve_incident` are **distinct
permissions**. Declaration intentionally broad by org policy (hesitating to
declare is a known incident-response failure); resolution intentionally narrow
(premature all-clear is the dangerous act). Both are RBAC policy entries
evaluated by the existing authorization layer (§F).

R7. There is **no "unwatched ambiguous incident" state**. No declaration
authorization ⇒ no incident session, only a warning (R3). Every
incident-regime session is the result of an authorized declaration.

R8. **Boundary:** run-done (§D, pause type 4) ≠ incident-cleared. A run going
quiet is at most Joe *proposing* resolution; it never flips the regime.
Phase 1 must not wire "run completes → incident mode off".

R9. **Non-goal (stated):** Joe's session/incident model does not compensate
for low-quality or noisy alerting; signal quality is an upstream concern. The
warnings surface (§E) is not a replacement alerting pipeline and Phase 1 must
not over-build it into one.

### Captaincy and declaration are one act (resolves open question #1)

R-CAP1. Declaration and captaincy are the same act for human declarers:
whoever declares the incident is captain from the instant of declaration.
There is no captain-less-but-declared state once a human is present, and no
"attached but declined captaincy" option — declining command on a declared
scene is not offered (ICS: arriving at / declaring a scene is assuming
command).

R-CAP2. For autonomous declarations (Joe under R2 authorization, or
configured-page-via-Joe): Joe is **not** itself the captain in v1 (see
captain types, R-CAP4). The session is `pending_captain` — mutation authority
is null (B2) — until the first **RBAC-authorized** human attaches, who
becomes captain on attach, with no separate claim prompt and no refusal
option.

R-CAP3. "First authorized human" means RBAC-authorized to captain that
session type. An unauthorized human attaching to a `pending_captain` incident
does **not** become captain (they cannot hold the authority that gates
mutation); they are an observer, and `pending_captain` persists until an
authorized human arrives. This is an authorization check, not a new state.

R-CAP4. Captain role is **typed**: `human` (v1 default — Joe acts with that
human's RBAC principal) or `joe` (authorization-gated, **inert in v1** — Joe
holds the role and acts with an explicitly provisioned, bounded
autonomous-authority grant for that session type; never ambient authority;
the Safety layer binds underneath and harder). Phase 1 builds the role as
typed and builds `joe` as a defined-but-inert seam. Phase 1 does **not**
build autonomous-captaincy behavior. With the `joe` seam enabled (future), an
autonomous declaration goes straight to `active`/`joe`-captain and the first
authorized human attach is a B-override transfer (R-OVR), not a claim;
`pending_captain` is specifically the seam-off (v1) state.

---

## A. Participation model (read/mutate split) — in force only in incident regime

A1. Reads/discovery require no captain approval. Mutation through Joe routes
through the captain. No separate suggestion object; no second approval
semantic.

A2. Parallel read-only investigations are independent `investigation`-type
sessions, not part of the incident session. Single-session-per-engagement
holds; the v2 Engagement/Session/Run three-level model remains deferred with
a clean migration path. Do not reopen this in v1.

A3. Any SRE may attach read-only (observer) to any session in scope.
Multi-human observer attach is explicitly **cross-session**, not
within-incident-only.

A4. Finding promotion: a human posts an attributed, non-actionable synthesis
message into a target session's timeline, optionally carrying a string
reference to the source investigation session. Reuses the annotation
semantic. No new object, no workflow, no merge. The captain reads it and
decides whether to act — the normal captain-driven path.

A5. Accepted deferred tradeoff: the incident session is not a self-contained
record in v1; the full picture is the incident session plus its linked
findings' source sessions. Auto-enrichment is a clean v2 follow-on.

A6. Finding references are **live attach points** (reachable via the
cross-session observe capability of A3, not merely postmortem breadcrumbs).
Ad-hoc investigation sessions live independently — see §5b lifecycle.

---

## B. Authority & captain transfer — captain exists only in incident regime

Authority is layered. This section distinguishes three things the security
architecture keeps separate and that earlier drafts wrongly conflated:

- **Infra RBAC** — can this principal touch this source with this action.
  *Exists in code* as `PolicyEngine.IsAllowed(ctx, principal, sourceID,
  action)`, per-call, principal+zone+action. The session model **consumes it
  unchanged**.
- **Session-coordination mutation gate** — is this mutation arriving from the
  captain session under current incident-mode rules. *Does not exist
  anywhere.* The session model **builds and owns this** (§C). It is **not** a
  call into the authorization layer.
- **Execution safety** — T1/T2/T3 tiers, panic/safe-mode, unlock. *Exists in
  code*, principal-independent, downstream of and independent from RBAC. The
  session model **wires to it** for autonomous-Joe safety and human override
  (R-OVR).

B1. Joe's effective authority is a pure function of the **current captain's
RBAC principal**, recomputed on every captain change, never inherited, never
compared peer-to-peer. There is no authority-comparison check on transfer.
Concretely: "Joe acts with the captain's authority" means the session layer
passes the **current captain's `Principal`** as the `principal` argument to
`IsAllowed` for mutations in that session. Captain change ⇒ subsequent
`IsAllowed` calls carry the new captain's principal. **The session model is
responsible for establishing and threading the principal-to-captain binding;
the RBAC layer faithfully evaluates whatever principal it is handed and
cannot infer that it came from the captain.** (`Principal` is a bare string
set from the API key at middleware time in current code; the binding is
entirely the session model's job.)

B2. An autonomous unattached incident session has **null mutation authority**
until an authorized human attaches and becomes captain (R-CAP2/3).

B3. Captain transfer with authority contraction while a run is mid-flight:

- Outgoing captain present/responsive → Joe pauses, asks the **outgoing**
  captain "finish under current authority, or cancel clean?", they decide,
  then transfer completes. A "finish" is a bounded, one-time, logged
  authorization of *that specific in-flight plan only*; it cannot seed new
  privileged work.
- Outgoing captain absent/unresponsive → halt at the blocked step, run goes
  to `cancelled` (or is held per the §C redirect, see §D terminals), transfer
  completes clean, new captain notified. The unanswered graceful question
  decays into this via the existing unreachability signal — **not** a new
  timeout (honors plan-of-record no-Joe-side-timeouts; reuses the single
  sanctioned transfer-on-unreachable exception).

B4. Captain is a **coordination role**, not an authority concept and not
session ownership. It is the single mutation-driver that exists *because*
incident regime is active. Outside incident regime there is no captain.

### Captain-transfer state machine (resolves open question #2)

States: `active` → `transfer_requested` → `transfer_confirmed` → `active`.

Dual initiation:
- (a) the **current captain** hands off → proceeds to confirm normally;
- (b) an **authorized incoming human** requests command. Guarded: valid as
  self-assumption **only if the current captain is unreachable**. If the
  current captain is reachable, an incoming request becomes a `decision`
  solicitation to the current captain (approve/decline). Current captain
  approves → `transfer_confirmed`. Present and declines → denied, stays
  `active`. **Does not respond** → decays, via the single sanctioned
  captain-transfer-on-unreachable timeout, to incoming-assumes-command.
  Self-proclamation while the current captain is reachable is never silent.

The B3 contraction/absent branches hang off `transfer_requested`; they are
not separate states.

### Human override of Joe-captain (B-OVR — invariant)

R-OVR. When the current captain is type `joe` (future seam), a
properly-authorized human's command-assumption request results in
**immediate, non-discretionary** `transfer_confirmed`. Joe-captain cannot
decline or delay human takeover. This is compiled in, not configurable, not
subject to Joe's judgment. The transfer state machine is structurally
unchanged; the "reachable captain declines/doesn't-respond" branch is
force-overridden to immediate-yield for `joe`-type captains. Tier: same as
the idempotency-key and one-agentic-loop invariants.

**Seam limitation (stated, from code verification):** the existing override
substrate (panic → safe-mode → `unlock --reason`) is a **single global
boolean**. There is no per-agent or per-session unlock and no approval
workflow. Therefore, when Joe-captaincy is later enabled, "human overrides
autonomous Joe" via the panic path is a **global emergency stop** (entire
system drops to T1 safe mode), not a session-surgical takeback. This is
acceptable for v1 (Joe-captain is inert) but whoever enables the `joe`
captain seam must know the override is global-blunt until a scoped
unlock/agent-pause mechanism is built. Recorded as a known seam limitation,
not a v1 blocker.

---

## C. Incident-regime mutation rule — session-model-owned gate

C1. When incident regime is active, mutation **through Joe** is accepted only
from the captain session. Non-captain mutation through Joe is refused and
redirected to the captain via the finding/annotation path (A4). Reads and
parallel investigation are unaffected in all sessions.

C2. This gate is a **session-model-owned enforcement point**, not a
delegation to and not a modification of the security layer. It sits
**upstream** of the existing unchanged pipeline. Verified against code: the
authorization primitive `IsAllowed(principal, sourceID, action)` has no
session/caller/incident parameter and cannot make this decision. Pipeline
order Phase 1 must implement:

1. Session model gate (C1): is this mutation from the authorized captain
   session under current incident-mode rules? No → refuse/redirect (A4).
2. If yes, the request proceeds **into the existing, unmodified pipeline**:
   RBAC `IsAllowed` (with the captain's principal per B1) → LLM → Safety
   tier (T1/T2/T3) → execution.

The session model adds a gate; it does not call the security layer as the
authority for the coordination decision and does not alter it.

C3. Out-of-band human mutation outside Joe (manual SSH/terraform/git) is
explicitly **out of scope**. Joe makes no claim over it, does not detect or
prevent it. A known visible gap is safer than a false guarantee; the safe
path is the easy path.

C4. The rule is **positional** (which session the mutation arrives from), not
semantic (what it touches). No blast-radius computation in v1. The
scoped/platform config dial is dropped entirely.

C5. Non-configurable floor, compiled in (not settings): (a) that incident
regime constrains non-captain mutation through Joe at all; (b) the
`pending_captain` null-authority state on autonomous unattended incident
creation.

---

## D. Run state machine & durable-execution boundary (resolves #3)

This is the highest-leverage section. It is design for the post-refactor
single-agentic-runtime state (see §G for the current-code gap and sequencing
consequence).

D1. Run states: `running`, `awaiting_input`, `awaiting_world`; terminals
`completed` / `failed` / `cancelled`. No more. Speculative pause-flavor
distinctions live in solicitation **payload data** (revisable), not in states
(migration-expensive). Justified by the terra-incognita reality (no Joe in
prod, AI-SRE behavior unknown): minimal states, rich revisable payloads.

D2. Field pause types collapse: (1) decision/approval, (3)
do-this-out-of-band-and-report, (4) done-unless-told-otherwise all become
`awaiting_input`, differing only by typed solicitation payload (this defines
the §D-input taxonomy). Pause type (2) waiting-on-the-world is
`awaiting_world` and is the binding constraint on the durable boundary.

D3. Runs are **single-threaded**. `awaiting_input` and `awaiting_world` are
true suspends — a suspended run does nothing else. Intra-incident parallelism
is achieved with **multiple sessions/runs**, never one run doing concurrent
work. Required by the single-agentic-loop invariant: a suspended-but-running
run is definitionally a second concurrent loop activity, which is the
regression to catch in review.

D4. The durable unit is the **step**. A step is durable only once its
externally-observable effect (if any) is recorded with a reattachable
handle — not when the reasoning that chose it completes. `running` is never a
durable resting state; resume re-enters the loop from the last completed
persisted step.

D5. **Idempotency-key invariant (tier: same as one-agentic-loop and R-OVR):**
every world-mutating tool call carries an idempotency key persisted **before**
the call is issued, so a resume that cannot tell whether the issue succeeded
can re-attempt without double-acting. Re-running reasoning on resume is safe
(idempotent by nature); re-running a tool call is safe **only** because of
this rule.

D6. `awaiting_world` boundary: persist intent before issuing; persist the
effect handle (locator + how to query its state) **before** the run suspends.
Resume reads the handle, re-queries the world's actual current state,
continues forward from it — **never re-issues**. Joe actively polls/awaits
the handle while suspended (not passive sleep).

D7. Human override of `awaiting_world`: supplies the world-resolution Joe
couldn't observe, or terminates the run. **Never rewinds to a pre-effect
state** (the effect is real and may be ongoing). Three forms —
declare-completed-with-outcome; declare-aborted-mid-flight (Joe reasons
forward from an indeterminate post-abort world state, not as never-happened);
terminate-run (→ `cancelled`, clean stop + action ledger). Active tracking is
primary; override is the backstop for when tracking fails or a human acts
out-of-band.

D8. Resume reconstruction (also answers the 3am-attach-trust question): resume
does **not** replay the reasoning trace. It rehydrates (a) persisted
synthesized understanding/conversation state, (b) current run state +
solicitation or world-handle if suspended, (c) the **action ledger** of
externally-observable effects taken (by handle, with status). An attaching
SRE sees conclusion + action ledger + what it is waiting on — the ICS SITREP
shape, which is also the cheaper thing to persist (use case and
implementation agree).

D9. Security-layer-unavailable (forced by code verification): in `embedded`
mode (the only mode that exists in code), the security/RBAC tables share the
process and DB; their unavailability means the process is down, so this is
not a distinct in-flight condition for v1. A future `remote` fail-closed mode
would make "security layer unreachable mid-run" a real condition: it must be
treated as a forced wait/halt that emits the plan-of-record waiting-state
notification webhook and **does not** retry into denials — not a run failure,
not an `awaiting_world` resolution. v1: not applicable (embedded only).
Recorded so a future remote-mode does not discover this in prod.

### awaiting_input solicitation taxonomy (resolves #4)

`awaiting_input` carries one of three solicitation types — payload schemas on
a single state, never new states:

- **`decision`** (pause type 1) — bounded choice on a Joe recommendation.
  Payload: proposed action(s), reasoning summary, closed option set. The §C
  captain-mutation-redirect and the B3 outgoing-captain question are
  `decision`s.
- **`provide_data`** (pause type 3) — a human must supply information Joe
  cannot get itself, often out-of-band. Payload: what is needed, why Joe
  cannot self-serve, typed expected-response shape. Carries an optional
  `liveness` flag: "decision from an attached human now" vs "blocked on
  out-of-band human work, attachment may lapse". Sole v1 consequence of the
  latter: on captain disconnect it does **not** auto-cancel but emits the
  plan-of-record-mandated waiting-state notification webhook so paging can
  re-engage. No new state, no timeout. Structured multi-field fill is a
  **deferred payload variant** of this type — addable later with no state /
  type / durable-boundary change (D1 makes this cheap); not in v1.
- **`confirm_close`** (pause type 4) — Joe believes work done, parked pending
  direction. Payload: synthesized conclusion, action ledger, proposed
  disposition. **Requires an explicit human disposition in v1 — no
  silence-is-assent for any session type.** For incident-regime sessions it
  is additionally bound by R5/R8 (it is Joe *proposing* resolution; the human
  disposition is the required human act; it never auto-flips the regime). The
  "Joe disposes its own `confirm_close`" capability is a defined-but-inert
  authorization-gated seam, enabled later when proven — the path to the full
  self-healing loop, explicitly out of v1.

---

## E. Joe warnings surface (new named v1 object)

E1. Destination for Joe's incident-judgments-it-is-not-authorized-to-act-on
(R3). **Not a session.** Append-only, attributed, human-reviewable list of
Joe-raised concerns, each linkable to the investigation context that produced
it.

E2. Deliberately minimal: not a queue with state, not something Joe acts on,
not self-escalating — same restraint as A4. A human reads it and may choose
to declare an incident (which then enters the model normally). Phase 1 must
not build it into an alerting pipeline (R9).

---

## F. Authority policy depth (resolves #6) — no new engine

F1. The authorization/RBAC enforcement layer **already exists in code**
(`internal/rbac`, `PolicyEngine.IsAllowed`, zone-based, per-call,
principal+source+action). The session model introduces **no new authority
engine**. It defers to the existing layer at the gate points the model
defines: the §C captain-session gate is upstream and session-owned (not an
authz call); `can_declare_incident` / `can_resolve_incident` (R2/R6) are RBAC
policy entries; per-captain authority (B1) is principal-threading into the
existing `IsAllowed`. Whatever tier/policy shape that layer implements is the
v1 answer by reference, not by redesign. The flat-vs-context-aware question
is closed by it being an already-made decision in an existing layer.

F2. Execution safety is the **independent Safety layer** (T1/T2/T3, panic,
safe mode, unlock) — verified to exist, downstream of and independent from
RBAC, principal-uniform. Autonomous-Joe safety (future) rests on this plus
the existing panic/unlock path, **not** on inventing a non-human RBAC
principal. Subject to the global-blunt-unlock seam limitation in §B (R-OVR).

---

## 5b. Session lifecycle & persistence

5b-1. **Only `incident` sessions have a lifecycle**: `declared` →
`being_worked` → `believed_mitigated` → `resolved` → `reviewed`. Transitions
are the already-settled R4/R5/R8 resolve rules; `believed_mitigated →
resolved` is fed by a `confirm_close` proposal (§D taxonomy) that a human
disposes. These states are named anchors for already-decided rules, not new
behavior. The `resolved` transition **is** the incident-mode-resolve act
(R4/R5) — this is the incident-lifecycle → regime-resolve coupling.

5b-2. **All other session types have no lifecycle and no terminal/`closed`
state.** They are persistent, searchable, deletable artifacts — the
claude.ai / Claude Code mental model. "Stopping work" is absence of activity,
not a state transition. The postmortem property (an investigation outliving
its incident) is a *consequence* of there being no lifecycle to couple to,
not a special rule.

5b-3. **Team-global, not SRE-private.** All sessions are team-scoped:
searchable and readable across the team, governed by RBAC read-authorization.
Joe's session store is a team-wide corpus. Phase 1 builds session storage and
the search/retrieval API team-scoped from day one.

5b-4. **Storage/retention (resolved as a seam):** Phase 1 — durable,
team-scoped, searchable, individually deletable sessions; **no auto-expiry,
no auto-archiving**; sessions carry retention-relevant metadata (type,
last-activity, linked-incident reference, incident resolution state) as a
defined-but-unused retention/archival seam. Retention policy is deferred
(terra-incognita: depends on real volume/cost/retrieval patterns).

5b-5. **Deletion:** hard delete is supported and is scoped to the **incident
session together with its linked read-only/investigation sessions, as a
single cascade** (not independent per-session deletes). No tombstone — true
expunge (satisfies the secrets-accidentally-captured ops need, incident-wide).
Intended interaction with 5b-2: a *resolved* incident's linked investigations
survive independently (postmortem property); a *hard-deleted* incident
cascades deletion to its linked investigations (expunge is all-or-nothing by
design). Non-incident sessions with no incident linkage are independently
deletable as ordinary artifacts.

5b-6. **Persistence backend:** persistence is interface-abstracted; the
Postgres path is placeheld; SQLite is the default and only deployed path.
Backend swap and prod migration mechanics are tracked in a **separate
dedicated session**, out of Phase 0 scope. Phase 1 builds session/run durable
state **against the persistence interface, not SQLite directly, and must not
introduce SQLite-specific coupling that would break the Postgres seam.** Note
(from code verification): the RBAC layer specifically uses raw `*sql.DB` and
is *not* behind the store interface — that is the RBAC layer's concern, not
the session model's; the session/run durable state is behind the interface
and must stay there.

---

## The incremental-autonomy seam pattern (stated once)

The following are all the same pattern: a defined, authorization-gated,
day-one-**inert** entry point that Phase 1 builds as a *seam*, never as
*behavior*, enabled later only when Joe has earned trust:

- Joe-declare-incident (R2)
- Joe-resolve-incident (R4)
- Joe-dispose-`confirm_close` (§D taxonomy)
- `joe`-type captain (R-CAP4)

Phase 1 builds the seams; it does not build the judgment. The
**human-override-always-wins invariant (R-OVR)** is the non-negotiable floor
that makes enabling any of these safe — subject to the stated global-blunt
limitation of the current override substrate.

---

## G. Reconciliation with current code (Phase 1 sequencing — must read)

Verified against the Joe repository (not docs; the security doc was found
stale in specific ways):

G1. **Single agentic loop / single LLM contact point does NOT exist yet.**
Current code has two LLM adapters (joe-core + joe CLI) and two agentic loops
(Core Agent + `useragent.Agent` REPL). §D is correct as design-for-the-
post-refactor-state, but it is a **precondition Phase 1 establishes**, not an
existing substrate. The plan-of-record sequencing (sessions → migrate REPL →
collapse loopback) stands.

G2. **No loopback-HTTP-to-self exists.** Plan-of-record Phase 3 ("collapse
the /tasks loopback") is partly moot: joe-core's Core Agent uses its own
`tools.Registry` directly and does not HTTP-loop to itself. The real thing to
remove is the **CLI's own loop + adapter** (plan-of-record Phase 2), not a
loopback. **Plan-of-record reconciliation item:** Phase 3's framing should be
revised — there is no loopback to collapse; verify whether Phase 3 has any
remaining content once Phase 2 removes the CLI loop, or fold it into Phase 2.

G3. **`remote` security mode does not exist; `embedded` only.** The
`SecurityPolicy` interface, `joe-security` binary, gRPC, and
`internal/safety/invariants.go` table-protection file referenced in the
security doc are **not present in code**. §D9 (security-unavailable mid-run)
is therefore not-applicable for v1 and is recorded only for a future
remote-mode.

G4. **`Principal` is a bare string** set from the API key at middleware time;
there is no human/agent discrimination. This is why B1 explicitly makes the
principal-to-captain binding the **session model's responsibility** and why
F2 routes autonomous-Joe safety through the Safety layer, not RBAC principals.

G5. Persistence: store interface exists, Postgres driver registered, SQLite
default-and-only-deployed — the 5b-6 seam is real. RBAC tables are *not*
behind the store interface (raw SQL); irrelevant to the session model but
recorded so Phase 1 does not assume uniform interface coverage.

---

## Open / deferred (explicitly NOT Phase 1, tracked elsewhere)

- v2 Engagement/Session/Run three-level model — deferred, clean migration
  path, do not reopen in v1 (A2).
- Retention/archival policy — deferred seam (5b-4).
- DB backend swap + prod migration — separate dedicated session (5b-6).
- Scoped/per-agent unlock (vs the current global-blunt panic) — prerequisite
  for a session-surgical Joe-captain override; not v1 (§B R-OVR limitation).
- Structured multi-field `provide_data` payload variant — add when usage
  shows need; no structural change required (§D taxonomy).
- Plan-of-record Phase 3 re-scoping — see G2.

---

## Invariants (review catches these)

1. One agentic-loop implementation. A second concurrent loop activity —
   including a suspended-but-running run (D3) — is the regression.
2. Idempotency key persisted before every world-mutating call (D5).
3. Human-override-always-wins for `joe`-type captain (R-OVR), compiled in.
4. Incident-mode entry may be automated; exit may not (R5).
5. The §C captain-session gate is session-model-owned and upstream of the
   unchanged security pipeline — never a delegation to authz (C2).
6. No client instantiates its own LLM adapter after the refactor; no
   SQLite-specific coupling above the persistence interface (5b-6, G1).
