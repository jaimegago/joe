# Incident-Captain Flow — Backend Audit

**Scope:** Audit only, no code changes. Every factual claim cites `file:line` against the
live working tree. Where a thing could not be found, that is stated explicitly rather than
inferred.

**Intended product flow under test:** a user declares an incident → Joe enters incident
mode → that user becomes the captain → holds the captain role → until it is transferred to
someone else.

---

## TL;DR verdict on captain scope

**The captain is a per-session row, but behaviorally there is one effective "incident
captain" at a time — the captain bound to the single active incident session.** It is not a
free-standing global role; it is the `session_captains` row (`detached_at IS NULL`) attached
to whichever session is the unique active incident. Because regime is a single global row
(`system_regime`, one row, `id = 1`) and declare creates exactly one incident session while
resolve clears it, at most one active incident session — and therefore at most one
authority-bearing captain — exists at any moment by construction.

Citations for the verdict are in [Question 2](#question-2--captain-scope-global-vs-per-session).

---

## Prose walkthrough of the intended flow mapped onto code

### Step 1 — Declare → enter incident mode → become captain — **SUPPORTED**

- HTTP entry: `POST /api/v1/regime/declare`, registered at
  [internal/api/regime.go:55](../../internal/api/regime.go) and handled by
  `regimeHandler.declare` at [internal/api/regime.go:74](../../internal/api/regime.go).
- Authorization: the declaring principal must pass `PolicyEngine.HasZoneAccess(..., "regime-control", ActionDeclareIncident)`
  at [internal/api/regime.go:128](../../internal/api/regime.go). If RBAC is unconfigured the
  endpoint returns 503 ([internal/api/regime.go:75-79](../../internal/api/regime.go)).
- The handler then calls `repo.DeclareIncidentRegime(principal, declaredKind)` at
  [internal/api/regime.go:156](../../internal/api/regime.go).
- That repository method runs **one DB transaction** doing three writes
  ([internal/sessionmodel/regime_transitions.go:32-118](../../internal/sessionmodel/regime_transitions.go)):
  1. insert an `incident`-type session in state `declared`
     ([regime_transitions.go:71-79](../../internal/sessionmodel/regime_transitions.go));
  2. flip `system_regime` to `incident` with `declared_by_principal = principal`
     ([regime_transitions.go:82-89](../../internal/sessionmodel/regime_transitions.go));
  3. **attach the declaring principal as captain** (`session_captains` row,
     `captain_type='human'`, `transfer_state='active'`, `last_seen_at` seeded) —
     [regime_transitions.go:91-104](../../internal/sessionmodel/regime_transitions.go).
- On success the handler returns `session_id`, `captain_id`, `declared_by`
  ([internal/api/regime.go:166-170](../../internal/api/regime.go)).

**So declare-to-captain linkage exists and is atomic** (this is the "R-CAP1" rule named in
the interface doc at [repository.go:147-163](../../internal/sessionmodel/repository.go) and
the handler comment at [regime.go:68-69](../../internal/api/regime.go)). The declaring human
becomes captain in the same transaction as entering incident mode. See
[Question 1](#question-1--declare-to-captain-linkage) for the definitive statement.

### Step 2 — A later/other human attaches as captain (secondary path) — **SUPPORTED (subsequent attach only)**

- HTTP entry: `POST /api/v1/agent-sessions/{id}/captain/attach`
  ([internal/api/captain.go:52](../../internal/api/captain.go)), handler
  `captainHandler.attach` ([captain.go:63-124](../../internal/api/captain.go)).
- Delegates to `CaptainService.Attach`
  ([internal/sessionmodel/captain.go:94-159](../../internal/sessionmodel/captain.go)).
- This is the **R-CAP2 "first authorized human on a pending-captain incident"** path, *not*
  the declare path — confirmed by the comment at
  [captain.go:91-93](../../internal/sessionmodel/captain.go). Because declare already attaches
  the declarer as captain (Step 1), this attach route only does something when the active
  incident has no captain yet. See [Question 5](#question-5--attach-preconditions).

### Step 3 — Hold the role — **PARTIALLY SUPPORTED**

- The captain "holds" simply by being the `session_captains` row with `detached_at IS NULL`
  ([internal/sessionmodel/repository.go:731-740](../../internal/sessionmodel/repository.go)).
- Authority is enforced on every mutating tool call by the §C gate (see
  [Question 2](#question-2--captain-scope-global-vs-per-session)).
- A heartbeat endpoint exists ([captain.go:126-157](../../internal/api/captain.go)) and
  updates `last_seen_at`, **but holding the role does not require heartbeats** — there is no
  timeout that detaches an idle captain. See
  [Question 4](#question-4--heartbeat-and-lapse). Marked *partial* because "hold until
  transferred" is true, but the reachability signal the heartbeat feeds is only consulted
  during a transfer, not to maintain the role.

### Step 4 — Transfer to someone else — **SUPPORTED (with authorization gaps)**

- Three endpoints: `transfer/begin`, `transfer/confirm`, `transfer/cancel`
  ([captain.go:54-56](../../internal/api/captain.go)). Full state machine and authorization
  analysis in [Question 3](#question-3--transfer-handshake-semantics).

### Step 5 — Resolve the incident — **SUPPORTED**

- HTTP entry: `POST /api/v1/regime/resolve`
  ([internal/api/regime.go:56](../../internal/api/regime.go)), handler `resolve`
  ([regime.go:184-261](../../internal/api/regime.go)).
- Requires `HasZoneAccess(..., "regime-control", ActionResolveIncident)`
  ([regime.go:217](../../internal/api/regime.go)) and that the active incident is in
  `believed_mitigated` ([regime_transitions.go:187-190](../../internal/sessionmodel/regime_transitions.go)).
- `ResolveIncidentRegime` transitions the session to `resolved` and clears regime to
  `normal` in one tx ([regime_transitions.go:129-222](../../internal/sessionmodel/regime_transitions.go)).
- **Note on captain teardown:** resolve does **not** explicitly detach the captain. The
  captain row is left as-is; it stops mattering because the gate's `findActiveIncident`
  excludes `resolved`/`reviewed` sessions
  ([sessiongate.go:142-161](../../internal/sessiongate/sessiongate.go)), so the regime is
  `normal` and the gate allows all mutations again
  ([sessiongate.go:93-95](../../internal/sessiongate/sessiongate.go)). The
  `session_captains` row remains with `detached_at IS NULL` but is inert.

---

## Question 1 — Declare-to-captain linkage

**Definitive answer: declaring an incident DOES create and designate a captain, and attaches
the declaring principal as that captain, atomically.**

- The declare handler calls `DeclareIncidentRegime`
  ([internal/api/regime.go:156](../../internal/api/regime.go)).
- `DeclareIncidentRegime` → `DeclareIncidentRegimeWithHook`
  ([regime_transitions.go:19-21](../../internal/sessionmodel/regime_transitions.go)) inserts a
  `session_captains` row for the declaring `principal` as step 3 of its single transaction
  ([regime_transitions.go:91-104](../../internal/sessionmodel/regime_transitions.go)).
- All three writes (session, regime flip, captain attach) are in one tx; any failure rolls
  the whole thing back ([regime_transitions.go:45-117](../../internal/sessionmodel/regime_transitions.go)).

This is **not** the separate `captain/attach` route — that route's service method
(`CaptainService.Attach`) is explicitly documented as the *subsequent* attach path, not the
declare path ([internal/sessionmodel/captain.go:91-93](../../internal/sessionmodel/captain.go)).
So captain designation at declare time is built in and requires no follow-up call.

---

## Question 2 — Captain scope: global vs per-session

**Definitive answer: per-session in the data model; effectively a single global incident role
in behavior.**

Resolution path for "who is the captain" when the gate checks a write:

1. The gate wrapper `captaingate.Wrapper.Execute`
   ([internal/captaingate/captaingate.go:141-197](../../internal/captaingate/captaingate.go))
   is installed around the task loop executor at
   [internal/api/tasks.go:302-305](../../internal/api/tasks.go) (and an identical wrapper
   around the Core Agent's executor — comment at
   [captaingate.go:1-15](../../internal/captaingate/captaingate.go)).
2. It reads the *calling* session id from context (`agentctx.SessionID(ctx)`,
   [captaingate.go:156](../../internal/captaingate/captaingate.go)) and delegates to the pure
   function `sessiongate.Check`
   ([internal/sessiongate/sessiongate.go:76-137](../../internal/sessiongate/sessiongate.go)).
3. `Check` does **not** look up a captain for the calling session directly. In incident
   regime it first finds **the active incident session** via `findActiveIncident`
   ([sessiongate.go:104](../../internal/sessiongate/sessiongate.go),
   [sessiongate.go:142-161](../../internal/sessiongate/sessiongate.go)), which scans
   incident-type sessions and returns the one not in `resolved`/`reviewed`.
4. It then requires the calling session to **be** that active incident session
   ([sessiongate.go:113-116](../../internal/sessiongate/sessiongate.go)), and looks up the
   captain bound to *that* session via `CurrentCaptainPrincipal(active.ID)`
   ([sessiongate.go:119](../../internal/sessiongate/sessiongate.go)).
5. The mutation is allowed only if the calling principal equals that captain's principal
   ([sessiongate.go:129-136](../../internal/sessiongate/sessiongate.go)).

So although `session_captains` is keyed per session and the attach route is per-agent-session
([captain.go:52](../../internal/api/captain.go)), and `GetActiveCaptain` is per-session
([repository.go:731-738](../../internal/sessionmodel/repository.go)), the gate always resolves
the captain through the **single** active incident session. The relevant session is
**not** "the calling session's captain" — it is "the active incident's captain", and any call
from a different session is refused outright
([sessiongate.go:113-116](../../internal/sessiongate/sessiongate.go)).

Combined with: regime is a single global row (`system_regime WHERE id = 1`,
[repository.go:629-631](../../internal/sessionmodel/repository.go)); declare creates exactly
one incident session ([regime_transitions.go:67-79](../../internal/sessionmodel/regime_transitions.go));
and `Attach` only writes a captain row for incident-type sessions
([captain.go:126-130](../../internal/sessionmodel/captain.go)) — there is at most one active
incident session, hence at most one authority-bearing captain.

**Verdict (one line): the incident captain is effectively a single global role, realized as
the captain row bound to the unique active incident session — not one independent captain per
arbitrary session.**

---

## Question 3 — Transfer handshake semantics

The handshake spans three endpoints in [internal/api/captain.go](../../internal/api/captain.go)
delegating to `CaptainService` in
[internal/sessionmodel/captain.go](../../internal/sessionmodel/captain.go). The state lives on
the active captain row's `transfer_state` column (`active` → `transfer_requested` →
`transfer_confirmed`), defined at
[types.go:120-124](../../internal/sessionmodel/types.go).

### transfer/begin — [captain.go:165-241](../../internal/api/captain.go) → `BeginTransfer` [captain.go:203-304](../../internal/sessionmodel/captain.go)

- **Body:** `initiator` (`"outgoing"` | `"incoming"`), `incoming_principal`, `run_id`
  (required — a transfer needs an active run to host the decision solicitation,
  [captain.go:181-185](../../internal/api/captain.go)).
- **Who is authorized:**
  - No RBAC zone gate (see [Question 5](#question-5--attach-preconditions) — captain routes
    are not RBAC-gated). Caller must be an authenticated principal (401 otherwise,
    [captain.go:171-175](../../internal/api/captain.go)).
  - `outgoing`-initiated: the service requires `current.Principal == requestingPrincipal` —
    i.e. **only the current captain** may begin an outgoing transfer
    ([captain.go:257-260](../../internal/sessionmodel/captain.go)).
  - `incoming`-initiated: the handler forces `incoming = caller`
    ([captain.go:200-202](../../internal/api/captain.go)) — i.e. the **requesting principal is
    the incoming candidate**. Any authenticated principal may request to take over.
- **Requires state:** there must be an active captain
  ([captain.go:219-221](../../internal/sessionmodel/captain.go), else `ErrNoActiveCaptain`),
  and that captain's `transfer_state` must be `active` (no transfer already in flight,
  [captain.go:222-224](../../internal/sessionmodel/captain.go), else
  `ErrTransferAlreadyInFlight`).
- **Produces state — three branches:**
  - `outgoing`-initiated → opens a `decision` solicitation (`outgoing_finish_or_cancel`) on
    the run and sets `transfer_state = transfer_requested`
    ([captain.go:261-271](../../internal/sessionmodel/captain.go)).
  - `incoming`-initiated **and current captain reachable** → opens a solicitation
    (`incoming_request_approve_decline`) and sets `transfer_requested`
    ([captain.go:273-292](../../internal/sessionmodel/captain.go)). Reachability is
    `IsCaptainReachable` against the 90s window
    ([captain.go:274](../../internal/sessionmodel/captain.go)).
  - `incoming`-initiated **and current captain unreachable** → skips the handshake and goes
    straight to `transfer_confirmed` via `completeTransfer`
    ([captain.go:293-299](../../internal/sessionmodel/captain.go)). This is the single
    sanctioned timeout exception.
  - (Also: a `joe`-type current captain + incoming-initiated force-overrides directly to
    `completeTransfer` — [captain.go:244-250](../../internal/sessionmodel/captain.go) — but
    `joe`-type captain is an inert seam in Phase 1, so this is unreachable in production.)

### transfer/confirm — [captain.go:243-273](../../internal/api/captain.go) → `ConfirmTransfer` [captain.go:316-331](../../internal/sessionmodel/captain.go)

- **Who is authorized:** the handler reads the principal **only for the audit row**
  ([captain.go:249-255](../../internal/api/captain.go)); it does **not** check who the caller
  is before calling `ConfirmTransfer(sessionID)`. `ConfirmTransfer` itself performs **no
  principal check** ([captain.go:316-331](../../internal/sessionmodel/captain.go)). So **any
  authenticated principal can confirm a pending transfer.** (See gap list.)
- **Requires state:** an active captain with `transfer_state == transfer_requested`
  ([captain.go:324-326](../../internal/sessionmodel/captain.go)) and a recorded
  `incoming_principal` ([captain.go:327-329](../../internal/sessionmodel/captain.go)).
- **Produces state:** `completeTransfer`
  ([captain.go:360-380](../../internal/sessionmodel/captain.go)) detaches the outgoing captain
  (`MarkCaptainDetached`) and inserts a fresh `active` captain row for the
  `incoming_principal`. The new row becomes the canonical captain (`GetActiveCaptain` returns
  it). Note these are **two sequential writes, not one tx** — a documented Phase 1 gap
  leaving a window where the session is captain-less
  ([captain.go:355-359](../../internal/sessionmodel/captain.go)).

### transfer/cancel — [captain.go:275-301](../../internal/api/captain.go) → `CancelTransfer` [captain.go:335-348](../../internal/sessionmodel/captain.go)

- **Who is authorized:** same as confirm — principal read for audit only
  ([captain.go:281-287](../../internal/api/captain.go)); `CancelTransfer` performs **no
  principal check** ([captain.go:335-348](../../internal/sessionmodel/captain.go)). **Any
  authenticated principal can cancel a pending transfer.**
- **Requires state:** active captain with `transfer_state == transfer_requested`
  ([captain.go:343-345](../../internal/sessionmodel/captain.go)).
- **Produces state:** resets `transfer_state` back to `active` and clears
  `incoming_principal`/`initiator` ([captain.go:346-347](../../internal/sessionmodel/captain.go));
  the current captain keeps the role.

### Which party confirms

The endpoints are mechanism, not party-bound. `BeginTransfer`'s solicitation payload names a
`reason` (`outgoing_finish_or_cancel` vs `incoming_request_approve_decline`,
[captain.go:261](../../internal/sessionmodel/captain.go),
[captain.go:281](../../internal/sessionmodel/captain.go)) describing *who is supposed* to
decide, but the backend does **not** enforce that the confirm/cancel caller is that party
(see authorization notes above). The intended semantics — outgoing captain finishes/cancels
their own handoff; outgoing captain approves/declines an incoming request — live only in the
solicitation payload, not in an enforced check.

### Prose state diagram

```
                         [active captain row, transfer_state = active]
                                          │
        ┌─────────────────────────────────┼──────────────────────────────────┐
        │ outgoing-initiated               │ incoming-initiated                │ incoming-initiated
        │ (caller == current captain)      │ (current captain REACHABLE)       │ (current captain UNREACHABLE,
        │                                  │                                   │  >90s since last_seen_at)
        ▼                                  ▼                                   ▼
  open solicitation               open solicitation                   completeTransfer:
  "outgoing_finish_or_cancel"     "incoming_request_                  detach outgoing,
  state → transfer_requested      approve_decline"                    insert new active captain
        │                          state → transfer_requested          state → transfer_confirmed
        │                                  │                            (new captain row, active)
        ├──── transfer/confirm ───────────┤                                   ▲
        │     (ANY principal — unchecked) │                                   │
        │     completeTransfer:           │                                   │
        │     detach outgoing,            │                              (no handshake;
        │     insert new active captain   │                               direct takeover)
        │     for incoming_principal      │
        │     → new active captain        │
        │                                 │
        └──── transfer/cancel ────────────┘
              (ANY principal — unchecked)
              state → active (current captain retained,
                              incoming_principal cleared)
```

Terminal of a confirmed transfer: the outgoing row has `detached_at` set
([repository.go:862-876](../../internal/sessionmodel/repository.go)) and a new
`active` captain row exists for the incoming principal — which then becomes "the captain" for
all subsequent gate checks.

---

## Question 4 — Heartbeat and lapse

**`captain/heartbeat`** ([captain.go:126-157](../../internal/api/captain.go) →
`RecordCaptainHeartbeat` [repository.go:821-842](../../internal/sessionmodel/repository.go)):
updates the active captain row's `last_seen_at` to now, but only if the calling principal IS
the active captain — otherwise `ErrCaptainPrincipalMismatch` (403) or `ErrNoActiveCaptain`
(409) ([captain.go:139-148](../../internal/api/captain.go),
[repository.go:825-834](../../internal/sessionmodel/repository.go)).

**Does captaincy expire if heartbeats stop? No.** There is no timeout that detaches an idle
captain. Evidence:

- The only consumer of the reachability signal is `IsCaptainReachable`, and the only caller of
  `IsCaptainReachable` in non-test code is `BeginTransfer`
  ([captain.go:274](../../internal/sessionmodel/captain.go)) — confirmed by grep across
  `internal/` and `cmd/`: the sole non-test, non-definition call site.
- The only caller of `MarkCaptainDetached` in non-test code is `completeTransfer`
  ([captain.go:362](../../internal/sessionmodel/captain.go)) — i.e. detach happens only as
  part of a transfer, never on a timer.
- There is **no** background reaper / ticker / cron / sweep that scans `last_seen_at` and
  detaches stale captains. Searched `internal/` and `cmd/` for reaper/sweep/expire/ticker/cron
  against captain code; none found.

So a missing heartbeat only changes what happens *if and when an incoming-initiated transfer
is attempted*: a captain unreachable for >90s
([cmd/joe/server.go:406](../../cmd/joe/server.go), threshold hardcoded to 90) lets an incoming
candidate take over directly without the outgoing captain's approval
([captain.go:293-299](../../internal/sessionmodel/captain.go)). Absent a transfer attempt, an
idle captain holds the role indefinitely.

**Implication for the UI:** the UI does **not** need to maintain a heartbeat to *hold* the
role. It only needs to heartbeat if it wants to **prevent** another party from using the
unreachable-takeover shortcut during a transfer. (See gap list — note this is "fail-open to
takeover," not "fail-closed to detach.")

---

## Question 5 — Attach preconditions

Via `POST /api/v1/agent-sessions/{id}/captain/attach`
([captain.go:63-124](../../internal/api/captain.go)) → `CaptainService.Attach`
([captain.go:94-159](../../internal/sessionmodel/captain.go)):

- **RBAC gate:** none. The captain routes are *sourceless*, so the source-keyed RBAC
  `EnforcementMiddleware` never fires, and Phase 1 deliberately does not add a zone gate —
  documented at [captain.go:29-34](../../internal/api/captain.go). The only auth requirement
  is a resolved principal (401 otherwise, [captain.go:69-73](../../internal/api/captain.go)).
  This contrasts with `regime/declare` and `regime/resolve`, which **do** gate on the
  `regime-control` zone ([regime.go:128](../../internal/api/regime.go),
  [regime.go:217](../../internal/api/regime.go)).
- **Must regime be in incident mode?** The session must be `type=incident`
  ([captain.go:126-130](../../internal/sessionmodel/captain.go)); attaching to a non-incident
  session returns `BecameCaptain=false` with no row written (informational, §B4). The method
  does not re-read `system_regime` directly — it keys off the session type. (An incident-type
  session only exists because a declare created it, which also set regime to incident.)
- **Must there be no current captain already?** Yes, to *become* captain. If
  `GetActiveCaptain` returns a row, the attach is informational: `BecameCaptain=false`, no row
  written — the second human is treated as an observer
  ([captain.go:132-140](../../internal/sessionmodel/captain.go)). It does **not** error and
  does **not** displace the sitting captain.
- **What happens if someone attaches while a captain already holds the role?** Nothing to the
  captaincy — the call succeeds with `BecameCaptain=false`
  ([captain.go:136-140](../../internal/sessionmodel/captain.go)), and the handler returns 200
  with `became_captain: false` ([captain.go:118-123](../../internal/api/captain.go)). Taking
  over from a sitting captain requires the transfer handshake, not attach.
- **`captain_type`:** `"human"` (default) is accepted; `"joe"` is refused with 403 as an
  inert Change-12 seam ([captain.go:82-95](../../internal/api/captain.go),
  [captain.go:101-114](../../internal/sessionmodel/captain.go)).

---

## Question 6 — Read surface

**Confirmed: there is no GET/read HTTP endpoint for the current captain.** The captain route
registration ([captain.go:46-57](../../internal/api/captain.go)) mounts exactly five routes,
all `POST`: `attach`, `heartbeat`, `transfer/begin`, `transfer/confirm`, `transfer/cancel`.
Grep for any `GET`/`HandleFunc` mentioning "captain" across `internal/api/` returns only those
five POST lines — no read route anywhere in the API package.

The read methods exist on the repository but are consumed only internally (the gate and the
captain service), never exposed over HTTP:

- **`GetActiveCaptain(ctx, sessionID)`**
  ([repository.go:731-740](../../internal/sessionmodel/repository.go)): returns the single
  `*Captain` row for `sessionID` with `detached_at IS NULL`, ordered `attached_at DESC LIMIT 1`
  — i.e. the full active captain record (id, session_id, captain_type, principal, attached_at,
  transfer_state, incoming_principal, transfer_initiator, last_seen_at). Returns `(nil, nil)`
  when there is no active captain. Callers: `CaptainService.Attach/BeginTransfer/ConfirmTransfer/CancelTransfer`
  and the reachability/heartbeat repo methods — all internal.
- **`CurrentCaptainPrincipal(ctx, sessionID)`**
  ([repository.go:810-819](../../internal/sessionmodel/repository.go)): thin wrapper over
  `GetActiveCaptain` returning `(principal string, ok bool, err error)`. `ok=false` means no
  active captain (pending-captain / null authority). Callers: `sessiongate.Check`
  ([sessiongate.go:119](../../internal/sessiongate/sessiongate.go)) and the §B1 principal
  substitution in `captaingate`
  ([captaingate.go:187](../../internal/captaingate/captaingate.go)) — both internal.
- (`ListCaptainsForSession` [repository.go:742-763](../../internal/sessionmodel/repository.go)
  returns the full captain history for a session, also unused over HTTP.)

So any UI that needs to display "who is the captain" has **no HTTP read path today** — the
data is reachable only in-process.

---

## Gap list — what the intended flow needs that the backend does not provide

1. **No captain read endpoint.** There is no `GET .../captain` (or equivalent) to read the
   current captain. `GetActiveCaptain` / `CurrentCaptainPrincipal`
   ([repository.go:731,810](../../internal/sessionmodel/repository.go)) exist but are
   in-process only; the five mounted captain routes are all POST mutations
   ([captain.go:52-56](../../internal/api/captain.go)). A UI cannot render captain identity or
   transfer state without one. **(Missing read path — explicitly requested gap.)**

2. **No way to discover the active incident / its session id from the captain surface.** Even
   with a read endpoint, the captain routes are keyed by `agent-sessions/{id}`; the effective
   captain lives on the *active incident session*. Resolving "which session is the active
   incident" is done internally by `findActiveIncident`
   ([sessiongate.go:142-161](../../internal/sessiongate/sessiongate.go)) /
   `ActiveIncidentSession` ([repository.go:350-359](../../internal/sessionmodel/repository.go)),
   but a UI needs an HTTP path to that id before it can ask "who is captain of it."
   `GET /api/v1/regime` ([regime.go:54](../../internal/api/regime.go)) returns regime mode and
   `declared_by_principal` but **not** the active incident session id or the current captain.

3. **Declare-to-captain linkage is present, not missing — but the secondary attach is fragile
   under the per-session vs global mismatch.** Linkage at declare is solid (Question 1). The
   gap is conceptual: the `attach` route accepts any `agent-sessions/{id}`, yet only the
   active incident session's captain has authority. Attaching a captain to a non-incident
   session silently no-ops (`BecameCaptain=false`,
   [captain.go:126-130](../../internal/sessionmodel/captain.go)), which a UX could
   misinterpret as success. (Not a missing-linkage gap for declare; flagged as a surface
   sharp edge.)

4. **Transfer confirm/cancel are not authorization-bound to the correct party.** Both
   `ConfirmTransfer` and `CancelTransfer` perform no principal check
   ([captain.go:316-331,335-348](../../internal/sessionmodel/captain.go)); the handlers read
   the caller only to write the audit row
   ([captain.go:249-255,281-287](../../internal/api/captain.go)). The intended UX ("the
   outgoing captain approves/declines an incoming request," or "the outgoing captain
   finishes/cancels their handoff") is encoded only in the solicitation payload's `reason`
   string ([captain.go:261,281](../../internal/sessionmodel/captain.go)), not enforced. Any
   authenticated principal can confirm or cancel a pending transfer. **(Missing
   handshake-authorization step the desired UX needs.)**

5. **No linkage between the solicitation decision and confirm/cancel.** `BeginTransfer` opens
   a `decision` solicitation on the run
   ([captain.go:388-418](../../internal/sessionmodel/captain.go)), but nothing ties resolving
   that solicitation to calling `transfer/confirm` or `transfer/cancel` — the handler comment
   even says "caller resolves the solicitation first and then calls this"
   ([captain.go:306-311](../../internal/sessionmodel/captain.go)). The two-step
   approve-then-confirm handshake the UX implies is **not** atomic or enforced server-side;
   it relies on the client doing both steps in order. **(Missing handshake glue.)**

6. **`completeTransfer` is not transactional.** Detach-then-attach are two sequential
   repository writes ([captain.go:360-380](../../internal/sessionmodel/captain.go)); a failure
   between them leaves the active incident captain-less (gate then refuses all mutations as
   pending-captain). Self-documented as a Phase 1 gap
   ([captain.go:355-359](../../internal/sessionmodel/captain.go)). **(Reliability gap in the
   transfer step.)**

7. **No automatic captain lapse, and the reachability behavior is fail-open to takeover.**
   There is no timeout that detaches an idle captain (Question 4). The only effect of a stale
   heartbeat is that an incoming-initiated transfer can complete *without the outgoing
   captain's consent* once `last_seen_at` exceeds 90s
   ([captain.go:293-299](../../internal/sessionmodel/captain.go),
   [server.go:406](../../cmd/joe/server.go)). If the UX expects "captaincy auto-releases when
   the captain disappears," that does not exist; if it expects "the role is stable unless
   explicitly transferred," that is true except for the unreachable-takeover shortcut. Either
   way, the UI must understand it is responsible for heartbeating to *block* an unconsented
   takeover, not to *hold* the role. **(Behavioral gap vs a likely UX assumption.)**

8. **Resolve leaves a dangling active captain row.** `ResolveIncidentRegime` does not detach
   the captain ([regime_transitions.go:129-222](../../internal/sessionmodel/regime_transitions.go));
   the `session_captains` row stays `detached_at IS NULL` after resolve. It is inert (gate
   ignores resolved sessions), but a captain-history/read surface would show a "still
   attached" captain on a resolved incident. **(Data-hygiene gap; not a flow blocker.)**

---

## Things I could not find / did not verify

- I did not find any HTTP endpoint exposing the active incident session id together with its
  captain in one read; confirmed by route-registration inspection but there is no aggregate
  "incident status" endpoint in `internal/api/` that I located.
- I did not trace the front-end (`ui/`) to confirm whether it already calls any of these
  endpoints — the task is backend-scoped, so the "the UI must…" statements are inferences
  about what a UI *would* need given the backend surface, not observations of current UI code.
- The 90s reachability threshold is hardcoded at
  [cmd/joe/server.go:406](../../cmd/joe/server.go); I did not find a config knob overriding it
  in production wiring (the service accepts a parameter, but the sole production caller passes
  the literal `90`).
