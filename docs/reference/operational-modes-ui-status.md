# Operational Modes — UI/API Exposure Audit

**Scope:** How Joe's operational modes are exposed for UI consumption. Inventory and
gaps only — no fixes proposed.

**Method:** Established from the live working tree (branch `main`). Every factual claim
cites `file:line`. Doc/memory/prior-doc claims were re-verified against code; drift and
discrepancies are recorded in the final section rather than silently reconciled.

**Note on prior work:** an earlier version of this file predated the D-0018 write-floor
refactor. It denied that any daemon-wide read-only posture existed and described a
file-based panic/safe-mode mechanism (a `panic.state` file, a `safeModeActive` atomic,
a live `ActivateSafeMode` setter) that has since been deleted. This version
(`docs-reference-audit-01`) was re-derived against the live tree: observation mode is now
a real boot-resolved read-only posture, panic state lives only in the
`cluster_panic_state` DB row, and the typed denial is the reason-carrying
`WriteFloorError`. All claims were re-verified here; the supersession is logged in §9.

## The axes

Joe exposes two **independent and composable** operational axes, not one enum:

| # | Axis | Reasons / states | Entered via | State lives in |
|---|------|------------------|-------------|----------------|
| 1 | **Write floor** (boot-resolved) | `full` (down) / `observation` / `safe_mode` (up) | `JOE_MODE=observation` (observation); `joe panic`, `kill -USR1`, `POST /api/v1/panic` (safe_mode) | boot-sealed `WriteFloor` value + `cluster_panic_state` DB row |
| 2 | **Incident regime + captain gate** | `normal` / `incident` | `POST /api/v1/regime/declare`; captain gate in `internal/captaingate` + `internal/sessiongate` | `system_regime` / `session_captains` tables |

The write floor is a **single** boot-resolved value with a `reason` (D-0018): `observation`
and `safe_mode` are two reasons of the *same* floor and are mutually exclusive — a boot
resolves to exactly one ([internal/safety/floor.go:45-54](../../internal/safety/floor.go)).
The incident axis is independent: an install can be in incident mode with the floor down,
or in safe mode with no incident. "Full / normal" is the absence of both (§4).

All `/api/v1` routes sit behind `auth.EdgeAuth`; the source-keyed
`rbac.EnforcementMiddleware` only fires on paths carrying a `componentID`. The read
endpoints below (`/mutate-status`, `/panic/status`, `/regime`, `/me`) carry no
`componentID`, so they are gated by **authentication only**, not per-zone RBAC.
`apiPrefix = "/api/v1"` ([internal/api/constants.go:8](../../internal/api/constants.go)).

---

## 1. Axis 1a — Write floor / observation mode — read path **PRESENT**

**Resolved answer: a daemon-wide, boot-resolved, runtime-immutable read-only posture
exists (observation mode), and it is readable via a dedicated endpoint.** This corrects
the prior version of this doc, which denied any global read-only posture.

- **The posture is the write floor.** `WriteFloor` is a boot-resolved, runtime-immutable
  value exposing only `Up()` and `Reason()`
  ([internal/safety/floor.go:28-37](../../internal/safety/floor.go)). It is resolved
  exactly once by the pure `ResolveWriteFloor(panicStatePresent, observationEnvSet)`
  ([floor.go:45-54](../../internal/safety/floor.go)) and **sealed**: there is no setter,
  package function, or method anywhere in the binary that lowers a resolved floor —
  recovery is restart, never a live down-transition ([floor.go:22-37](../../internal/safety/floor.go)).
- **Observation mode is set via `JOE_MODE=observation`.** `env.Mode = "JOE_MODE"` and
  `env.ModeObservation = "observation"`
  ([internal/env/keys.go:25,31](../../internal/env/keys.go)). When set, the floor boots up
  with reason `FloorReasonObservation`
  ([floor.go:16,49-50](../../internal/safety/floor.go)) — a "calm, intended read-only
  resting posture; NOT panic/safe mode."
- **Boot resolves and seals it.** `cmd/joe/server.go` reads the env var, calls
  `ResolveWriteFloor`, logs the resulting reason, and seals the value into `Services`
  ([cmd/joe/server.go:415-430,459](../../cmd/joe/server.go)). The same boot reads the
  sticky panic state from the cluster store first; **panic wins over observation**, so a
  panicked Joe boots `safe_mode`, never the calmer `observation`
  ([floor.go:46-48](../../internal/safety/floor.go); [server.go:411-416](../../cmd/joe/server.go)).
- **Read endpoint:** `GET /api/v1/mutate-status`.
  - Registered: `registerMutateStatusRoutes`
    ([internal/api/mutatestatus.go:38-45](../../internal/api/mutatestatus.go)), wired at
    [internal/api/server.go:128](../../internal/api/server.go).
  - Handler: `handleMutateStatus`
    ([mutatestatus.go:64-70](../../internal/api/mutatestatus.go)) — both fields come from a
    SINGLE read of the boot-sealed floor, so they cannot disagree.
  - Response shape: `mutateStatusResponse{ can_mutate bool, reason string }` with explicit
    snake_case JSON tags ([mutatestatus.go:50-58](../../internal/api/mutatestatus.go)).
    `reason` is **always** one of `"full"` / `"observation"` / `"safe_mode"` — never the
    empty string ([mutatestatus.go:15-27,78-89](../../internal/api/mutatestatus.go)).
  - **Gating:** authentication only (no `componentID` in the path).
- **Source of truth:** the boot-sealed `services.WriteFloor`
  ([cmd/joe/server.go:459](../../cmd/joe/server.go)). **Survives restart: YES** — it is
  re-derived deterministically at every boot from the `JOE_MODE` env var (observation) and
  the `cluster_panic_state` DB row (safe_mode), so the posture is identical across restarts
  given the same inputs.

**The RBAC per-zone posture is a separate, finer-grained concept** — what a *given caller*
may do in a *given zone* — and is unchanged. It is readable via `GET /api/v1/me`, which
returns the caller's reachable zones with each zone's `allowed_actions` plus `rbac_enabled`
([internal/api/currentuser.go:31,35-51](../../internal/api/currentuser.go)). This is
per-principal and per-zone; the write floor is the install-wide read/write posture. The two
are orthogonal: a `full`-floor install still denies writes the caller's zone forbids, and a
`safe_mode`/`observation` floor denies *every* mutate regardless of zone grant.

**Write-floor read path: PRESENT** (`/mutate-status`). **RBAC per-zone posture: PRESENT**
(`/me`, per-zone not global).

---

## 2. Axis 1b — Panic / safe mode — read path **PRESENT**

Panic/safe mode is the *other reason* the same write floor can be up. It is the sticky
recovery state, distinct from the calm observation posture in §1.

- **Read endpoint:** `GET /api/v1/panic/status`
  - Registered: `registerPanicRoutes`
    ([internal/api/panic.go:28-40](../../internal/api/panic.go), `POST /api/v1/panic` +
    `GET /api/v1/panic/status`), wired at
    [internal/api/server.go:124](../../internal/api/server.go).
  - Handler: `handlePanicStatus`
    ([panic.go:96-116](../../internal/api/panic.go)).
  - Response shape: `panicStatusResponse{ safe_mode bool, triggered_at, trigger_source,
    trigger_reason }` with explicit snake_case JSON tags
    ([panic.go:51-56](../../internal/api/panic.go)). When safe mode is off it returns just
    `{safe_mode:false}` and omits the detail fields ([panic.go:98-101](../../internal/api/panic.go)).
  - **`safe_mode` is computed from the boot-sealed floor**, not a mutable atomic:
    `inSafeMode := h.floor.Reason() == safety.FloorReasonSafeMode`
    ([panic.go:97](../../internal/api/panic.go)). The detail fields are enriched from the
    panic DB row, never from disk ([panic.go:103-113](../../internal/api/panic.go)).
  - **Gating:** authentication only (no `componentID` in the path).
- **Source of truth — the `cluster_panic_state` DB row only (no file).** Panic state has a
  single home: the `ClusterPanicStore` DB row
  ([internal/safety/panic.go:31-43](../../internal/safety/panic.go)). `Trigger` sets the
  process-global `panicked atomic.Bool` and persists the row via the boot-registered store
  ([panic.go:45-76](../../internal/safety/panic.go)); boot reads `IsPanicked` from the row
  to raise the floor ([cmd/joe/server.go:399,411-416](../../cmd/joe/server.go)).
  - **There is no `panic.state` file and no `safemode.go` atomic.**
    `internal/safety/panic_state.go` and `internal/safety/safemode.go` do not exist; git
    `8da88d6` deleted the `panic.state` file. Two break-tests forbid their reintroduction:
    `TestWriteFloor_NoRuntimeLoweringPath` fails if `safeModeActive` / `ActivateSafeMode` /
    `DeactivateSafeMode` / `IsSafeModeActive` / `ErrSafeModeActive` / `Reset` reappear in
    production code ([internal/safety/floor_guard_test.go:30-93](../../internal/safety/floor_guard_test.go)),
    and `TestPanicState_SingleHomeNoFileConcept` fails if the `panic.state` file API or path
    literal reappears ([floor_guard_test.go:130-192](../../internal/safety/floor_guard_test.go)).
  - **Survives restart: YES.** The cluster row is sticky; boot reads it and raises the
    `safe_mode` floor on the next startup. Recovery is `joe unlock` (which clears the row)
    **plus a restart** — there is no live in-process clear
    ([panic.go:31-43](../../internal/safety/panic.go); [server.go:417-427](../../cmd/joe/server.go)).

---

## 3. Axis 2 — Incident regime + captain gate

### 3a. Incident regime — read path **PRESENT**

- **Read endpoint:** `GET /api/v1/regime`
  - Registered: `registerRegimeRoutes`
    ([internal/api/regime.go:54](../../internal/api/regime.go)), wired at
    [internal/api/server.go:147](../../internal/api/server.go). (Routes register only when
    `services.SessionModel` is non-nil [regime.go:40-42](../../internal/api/regime.go).)
  - Handler: `read` [regime.go:116-138](../../internal/api/regime.go) → `repo.GetRegime`.
  - Response shape: `regimeReadResponse` embeds `sessionmodel.Regime` (serialized with
    **no JSON tags**, so the wire keys are the exported Go field names
    `{ Mode, DeclaredAt, DeclaredByPrincipal, DeclaredKind }`
    [internal/sessionmodel/types.go:186-191](../../internal/sessionmodel/types.go)) plus the
    incident-master fields `{ IncidentSessionID, IncidentState, IncidentTitle }`, all
    `omitempty` and present only in incident mode
    [regime.go:104-114,122-136](../../internal/api/regime.go). `Mode` is `"normal"` or
    `"incident"` [types.go:172-173](../../internal/sessionmodel/types.go).
  - **Gating:** authentication only; the `read` handler performs no RBAC. (The
    `declare`/`resolve` *write* paths are RBAC-gated and seam-gated
    [regime.go:55-56](../../internal/api/regime.go), but those are not the read path.)
- **Source of truth:** `system_regime` single-row table via `GetRegime`
  [internal/sessionmodel/repository.go:858](../../internal/sessionmodel/repository.go).
  **Survives restart: YES** (SQLite-backed).

### 3b. Captain (who is the current captain) — read path **ABSENT**

- **No GET / read endpoint exists for the current captain.** `registerCaptainRoutes`
  registers only `POST` routes under `/api/v1/sessions/{id}/captain/*` — `attach`,
  `heartbeat`, `transfer/begin|confirm|cancel`
  [internal/api/captain.go:49-60](../../internal/api/captain.go). (The legacy
  `/agent-sessions` namespace was removed in B005, §12.8
  [captain.go:21-22](../../internal/api/captain.go).) No `GET .../captain` route exists.
- The regime read endpoint does **not** include the captain: the embedded `Regime` has only
  `Mode/DeclaredAt/DeclaredByPrincipal/DeclaredKind`
  [types.go:186-191](../../internal/sessionmodel/types.go) (the read response adds incident
  *session* fields, not the captain principal).
- The repository *can* answer it — `GetActiveCaptain`
  [repository.go:985](../../internal/sessionmodel/repository.go) and
  `CurrentCaptainPrincipal` [repository.go:1064](../../internal/sessionmodel/repository.go)
  — but no API handler calls either on a read path.
- **Source of truth (not exposed):** `session_captains` table; SQLite, would survive
  restart — but is unreadable via any API.

---

## 4. Full / normal mode

- "Full" (write floor down) and "normal" (no incident) are **not** stored or independently
  readable states; each is the **absence** of the active reasons on its axis.
- The write-floor axis reports `full` when the floor is down: `GET /api/v1/mutate-status`
  returns `{can_mutate:true, reason:"full"}`
  ([internal/api/mutatestatus.go:19,84-85](../../internal/api/mutatestatus.go)), and
  `GET /api/v1/panic/status` returns `{safe_mode:false}`
  ([internal/api/panic.go:98-101](../../internal/api/panic.go)).
- The incident axis reports `normal` (`RegimeModeNormal = "normal"`
  [internal/sessionmodel/types.go:172](../../internal/sessionmodel/types.go)) via
  `GET /api/v1/regime` when no incident is active.
- A client infers fully-normal operation by reading these endpoints and finding the floor
  down and the regime normal. The baseline that still applies is the RBAC per-zone posture
  from §1 (readable via `GET /api/v1/me`).

---

## 5. Read-path inventory (summary)

| Axis | Read endpoint | Source of truth | Survives restart | Status |
|------|---------------|-----------------|------------------|--------|
| 1a Write floor / observation (global) | `GET /api/v1/mutate-status` ([mutatestatus.go:44,64-70](../../internal/api/mutatestatus.go)) | boot-sealed `WriteFloor` ([server.go:459](../../cmd/joe/server.go)) | YES (re-derived from `JOE_MODE` + DB row) | **PRESENT** |
| 1 RBAC posture (per-zone, not global) | `GET /api/v1/me` ([currentuser.go:31,35-51](../../internal/api/currentuser.go)) | per-principal RBAC grants | YES (DB) | PRESENT — per-zone, not a global flag |
| 1b Panic / safe mode | `GET /api/v1/panic/status` ([panic.go:28-40,96-116](../../internal/api/panic.go)) | `cluster_panic_state` DB row ([panic.go:31-43](../../internal/safety/panic.go)) | YES (DB row) | **PRESENT** |
| 2a Incident regime | `GET /api/v1/regime` ([regime.go:54,116-138](../../internal/api/regime.go)) | `system_regime` table ([repository.go:858](../../internal/sessionmodel/repository.go)) | YES (DB) | **PRESENT** |
| 2b Captain | none ([captain.go:49-60](../../internal/api/captain.go) — POST only) | `session_captains` table (unexposed) | YES (DB, unexposed) | **ABSENT** |
| Full / normal | inferred from 1 + 2a | absence of the active reasons | n/a | derived |

---

## 6. UI coverage matrix

| Axis | UI fetches state? | UI renders indicator? | Evidence |
|------|-------------------|------------------------|----------|
| 1a Write floor / observation | **Yes** | **Yes (real, wired)** | `fetchMutateStatus` [ui/src/api/mutateStatus.ts:10](../../ui/src/api/mutateStatus.ts) → `useMutateStatus` [ui/src/hooks/useMutateStatus.ts:13](../../ui/src/hooks/useMutateStatus.ts) → `ObservationBanner` renders a calm blue banner only when `reason === 'observation'` [ui/src/components/layout/ObservationBanner.tsx:25-50](../../ui/src/components/layout/ObservationBanner.tsx), mounted in the app shell [ui/src/components/layout/AppShell.tsx:26](../../ui/src/components/layout/AppShell.tsx). `MutateStatusSchema` maps `{can_mutate, reason}` with `reason` an enum [ui/src/api/schemas.ts:410-413](../../ui/src/api/schemas.ts). |
| 1 RBAC posture | **Yes** | **Partial** | `CurrentUserSchema` carries `zones[].allowed_actions` via `ZoneAccessSchema` [ui/src/api/schemas.ts:328-345](../../ui/src/api/schemas.ts); the chat view uses zone presence to gate the composer / show an "access pending" state. No dedicated read-only/read-write *indicator chip* for the daemon — observation mode (above) is the global read-only signal instead. |
| 1b Panic / safe mode | **Yes** | **Yes (real, wired)** | `usePanicStatus` → `SafeModeBanner` renders a red banner only when `safeMode` is true [ui/src/components/layout/SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx), mounted in the app shell [AppShell.tsx:25](../../ui/src/components/layout/AppShell.tsx). `PanicStatusSchema` maps the snake_case wire keys [ui/src/api/schemas.ts:388-400](../../ui/src/api/schemas.ts). |
| 2a Incident regime | **Yes** | **Yes (real, wired)** | `useRegime` → `IncidentBanner` renders an amber banner only when `mode==='incident'` [ui/src/components/layout/IncidentBanner.tsx:10-52](../../ui/src/components/layout/IncidentBanner.tsx), mounted at [AppShell.tsx:27](../../ui/src/components/layout/AppShell.tsx). `RegimeSchema` maps the capitalized wire keys [ui/src/api/schemas.ts:354-379](../../ui/src/api/schemas.ts). |
| 2b Captain | **No** | **No** | No api-client function and no component reads captain state. |

### App-shell banners (mounted)

`AppShell` mounts **three** write-axis / incident banners, in order
([ui/src/components/layout/AppShell.tsx:25-27](../../ui/src/components/layout/AppShell.tsx)):

1. `SafeModeBanner` (red) — gates on `panicStatus.safeMode`
   [SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx).
2. `ObservationBanner` (blue) — gates on `mutateStatus.reason === 'observation'`
   [ObservationBanner.tsx:25-50](../../ui/src/components/layout/ObservationBanner.tsx).
3. `IncidentBanner` (amber) — gates on `regime.mode === 'incident'`
   [IncidentBanner.tsx:10-52](../../ui/src/components/layout/IncidentBanner.tsx).

Safe mode and observation are mutually exclusive (the floor carries one reason), so their
two banners can never co-render; incident is an independent axis and can show alongside
either ([AppShell.tsx:18-24](../../ui/src/components/layout/AppShell.tsx)).

### Specific indicators

- **Observation (global read-only) indicator — PRESENT.** Real, bound to live state via
  `useMutateStatus`/`fetchMutateStatus`
  [ObservationBanner.tsx:25-50](../../ui/src/components/layout/ObservationBanner.tsx);
  [AppShell.tsx:26](../../ui/src/components/layout/AppShell.tsx). This is the daemon-wide
  read-only chip the prior version of this doc said did not exist.
- **Safe-mode / panic indicator — PRESENT.** Real, bound to live state via
  `usePanicStatus`/`fetchPanicStatus`
  [SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx);
  [AppShell.tsx:25](../../ui/src/components/layout/AppShell.tsx).
- **Active-incident banner — PRESENT.** Real, bound to live state via `useRegime`/`fetchRegime`
  [IncidentBanner.tsx:10-52](../../ui/src/components/layout/IncidentBanner.tsx);
  [AppShell.tsx:27](../../ui/src/components/layout/AppShell.tsx).
- **Captain indicator — ABSENT** (no read path to consume, §3b).

---

## 7. Failure feedback (mode-specific denial messaging)

The shared dispatch lives in `classifyWriteFailure`, which maps a typed per-tool error to a
stable `error_code` ([internal/api/writefailure.go:49-73](../../internal/api/writefailure.go)).
The classifier is injected into the loop via `agentloop.WithToolErrorClassifier`
([writefailure.go:35-37](../../internal/api/writefailure.go)). The UI maps codes to
sentences in `writeFailureMessage`
([ui/src/hooks/useChat.ts:79-94](../../ui/src/hooks/useChat.ts)).

### Write-failure code inventory

The codes are **declared in [internal/api/constants.go](../../internal/api/constants.go)**;
that declaration block is authoritative for the exact set (per D-0032, this doc points at it
rather than freezing a count here). As of this audit the typed write-failure codes are
`zone_denial`, `incident_mode`, `safe_mode`, and `observation`
([constants.go:27-30](../../internal/api/constants.go)), with `internal_error`
([constants.go:15](../../internal/api/constants.go)) doubling as the fallback bucket. The
UI `writeFailureMessage` switch handles all five plus an `undefined` default
([useChat.ts:79-94](../../ui/src/hooks/useChat.ts)); the turn-level `writeFailureCode` field
documents the same set [useChat.ts:68-72](../../ui/src/hooks/useChat.ts).

**Precedence** (D-0019 decision 9: floor > incident > RBAC, ordered by resolvability depth)
is enforced upstream by check order — the floor in `tools.Executor`/`captaingate`, the gate
in `captaingate`, the RBAC accessor inside the tool — so exactly **one** typed error reaches
the classifier; the branch order is documentation of intent, not the enforcement
([writefailure.go:40-48](../../internal/api/writefailure.go)).

### Write-floor denial (observation **or** safe_mode) — **PRESENT (typed `WriteFloorError`)**

- The executor denies a `Mutate` when the floor is up by returning the single
  reason-carrying `&safety.WriteFloorError{Reason: e.floor.Reason()}`
  ([internal/tools/executor.go:215-219](../../internal/tools/executor.go)). The floor is
  checked **first** among the denials (precedence above) and the reason rides out as data —
  one branch, one error type, two presentations.
- `WriteFloorError` carries `FloorReasonObservation` or `FloorReasonSafeMode`
  ([internal/safety/floor.go:64-87](../../internal/safety/floor.go)); `errors.Is(err,
  ErrWriteFloor)` is true for either reason ([floor.go:60-87](../../internal/safety/floor.go)).
- `classifyWriteFailure` reads the reason via `errors.As` and emits `observation` or
  `safe_mode` accordingly
  ([writefailure.go:56-65](../../internal/api/writefailure.go)). The two never co-occur (the
  floor resolves to one reason at boot, safe_mode winning).
- UI: `safe_mode` → "System is in safe mode. Only read-only operations are permitted — run
  `joe unlock` to resume writes."; `observation` → "Joe is in observation mode — it can read
  and explain but will not make changes. This is the intended read-only posture."
  ([useChat.ts:85-88](../../ui/src/hooks/useChat.ts)).

### RBAC zone denial — **PRESENT (typed `zone_denial`)**

- A write blocked because the caller lacks the zone action surfaces as
  `access.ErrPermissionDenied` → `zone_denial`
  ([writefailure.go:68-69](../../internal/api/writefailure.go)) → "Access to this zone has
  not been granted to you…" ([useChat.ts:81-82](../../ui/src/hooks/useChat.ts)).

### Incident captain gate denying a non-captain mutation — **PRESENT (typed `incident_mode`)**

- The executor wrapper returns a typed `*captaingate.GateRefusalError` on refusal; the gate
  is wired into the user task loop and the Core Agent path. `classifyWriteFailure` maps it →
  `incident_mode` ([writefailure.go:66-67](../../internal/api/writefailure.go)); UI → "System
  is in incident mode. Writes are temporarily blocked." ([useChat.ts:83-84](../../ui/src/hooks/useChat.ts)).
- **Caveat (granularity loss):** `sessiongate.Check` distinguishes several refusal outcomes
  (regime mismatch, wrong session, pending-captain/null-authority, non-captain principal),
  and `GateRefusalError` carries the `CaptainSessionID` redirect target. `classifyWriteFailure`
  collapses them all into the single `incident_mode` code, discarding the redirect, so the
  user cannot tell "no captain attached yet" from "you are not the captain."

### Incident regime declare/resolve write denials

- These return generic HTTP errors keyed on RBAC/seam state, not a mode-specific typed code
  — e.g. `403` for missing capability / inert Joe-autonomous seam, `409` for state-machine
  violations ([regime.go:55-56](../../internal/api/regime.go) register the write paths).
  These are the regime *control* surface, not the agentic write path the captain gate governs.

---

## 8. Gap list

**Axes whose current state cannot be read via an API:**

1. **Captain — no read endpoint at all.** Only `POST` mutation routes exist
   [internal/api/captain.go:49-60](../../internal/api/captain.go); the repository can answer
   "who is the captain" ([repository.go:985,1064](../../internal/sessionmodel/repository.go))
   but no handler exposes it. "Who is the current captain" is unanswerable by any client.

**Axes with a read path but no UI indicator:**

2. **Captain — no UI consumption** (and no read path to consume). No `ui/src` component or
   api-client touches captain state.

**Failure-feedback gaps:**

3. **Captain-gate refusal granularity is collapsed** — several distinct gate outcomes reduce
   to one `incident_mode` message [writefailure.go:66-67](../../internal/api/writefailure.go);
   the `CaptainSessionID` redirect target is discarded before reaching the user.

**Closed since the prior version of this doc (no longer gaps):**

- Global read-only posture — now PRESENT as observation mode, with a read endpoint
  (`GET /api/v1/mutate-status`) and a wired UI banner
  ([ObservationBanner.tsx:25-50](../../ui/src/components/layout/ObservationBanner.tsx)).
- Panic / safe-mode UI indicator — now PRESENT
  ([SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx)).
- Write-floor denial typing — now the typed, reason-carrying `WriteFloorError` → `safe_mode`
  or `observation` ([writefailure.go:56-65](../../internal/api/writefailure.go)).

**Coverage summary:**

| Axis | Read API | Survives restart | UI fetch | UI indicator | Typed denial |
|------|----------|------------------|----------|--------------|--------------|
| 1a Write floor / observation (global) | PRESENT (`/mutate-status`) | YES | YES | PRESENT (banner) | PRESENT (`observation`) |
| 1 RBAC posture (per-zone) | PRESENT (`/me`) | YES | YES | PARTIAL (gating, no chip) | PRESENT (`zone_denial`) |
| 1b Panic / safe mode | PRESENT (`/panic/status`) | YES (DB row) | YES | PRESENT (banner) | PRESENT (`safe_mode`) |
| 2a Incident regime | PRESENT (`/regime`) | YES (DB) | YES | PRESENT (banner) | PRESENT (`incident_mode`, via gate) |
| 2b Captain | ABSENT | YES (DB, unexposed) | NO | NO | PRESENT (`incident_mode`, collapsed) |
| Full / normal | derived | n/a | derived | derived | n/a |

---

## 9. Code-vs-doc contradictions encountered

1. **Prior version of this doc denied the global read-only posture — SUPERSEDED.** The
   earlier file's §1 thesis ("Read mode … ABSENT … the concept does not exist as a single
   state") is now false: observation mode is a daemon-wide, boot-resolved, runtime-immutable
   read-only posture set via `JOE_MODE=observation`
   ([internal/env/keys.go:25,31](../../internal/env/keys.go);
   [internal/safety/floor.go:16,45-54](../../internal/safety/floor.go)) and readable at
   `GET /api/v1/mutate-status` ([internal/api/mutatestatus.go:44](../../internal/api/mutatestatus.go)).
   It is not a mutable `operational_mode` flag — it is the boot-resolved write floor (D-0018).

2. **Prior version described a file-based panic/safe-mode mechanism — DELETED.** The earlier
   file cited a `<joeDir>/panic.state` file (`internal/safety/panic_state.go`), a
   `safeModeActive atomic.Bool` (`internal/safety/safemode.go`), and a live
   `safety.ActivateSafeMode()` setter called at boot. None exist: panic state lives only in
   the `cluster_panic_state` DB row ([internal/safety/panic.go:31-43](../../internal/safety/panic.go)),
   safe-mode state is the boot-resolved `WriteFloor` reason
   ([internal/safety/floor.go:17-19,45-54](../../internal/safety/floor.go)), and recovery is
   `joe unlock` + restart with no live setter. Two break-tests forbid reintroducing the old
   mechanism ([internal/safety/floor_guard_test.go:30-93,130-192](../../internal/safety/floor_guard_test.go)).

3. **Tier vocabulary (T1/T2/T3) is retired — replaced by the binary Read/Mutate axis
   (D-0020).** The earlier file framed safety in terms of T1/T2/T3. The classification is now
   binary: `ActionRead` (never mutates the managed system; always allowed) and `ActionMutate`
   (denied by default, per-action opt-in)
   ([internal/safety/tier.go:14-27](../../internal/safety/tier.go)). The former
   Observe/Record/Act three-tier scheme was collapsed by D-0018/D-0019; the Record band is
   gone and Joe's own model-maintenance tools are Reads ([tier.go:180-211](../../internal/safety/tier.go)).

4. **Write-failure code set grew — now four typed codes.** The earlier file listed three
   (`zone_denial`/`incident_mode`/`safe_mode`); the live set adds `observation`
   ([internal/api/constants.go:27-30](../../internal/api/constants.go)). Per D-0032 this doc
   points at that declaration as authoritative rather than fixing a count.

5. **Captain route prefix corrected.** The earlier file placed captain routes under
   `/api/v1/agent-sessions/{id}/captain/*`; they live under `/api/v1/sessions/{id}/captain/*`
   (the `/agent-sessions` namespace was removed in B005)
   ([internal/api/captain.go:21-22,49-60](../../internal/api/captain.go)).

6. **Single binary — `cmd/joe-core` does not exist.** The live tree has only `cmd/joe`; the
   server entrypoint is [cmd/joe/server.go](../../cmd/joe/server.go), matching `CLAUDE.md`'s
   single-binary model.

7. **Deny-only captain gate — VERIFIED, no contradiction.** `captaingate` is DENY-ONLY: the
   session gate only ever allows or denies and never elevates RBAC; on allow the wrapper does
   principal substitution but grants no authority.

8. **Regime wire contract is fragile (observation, not a doc contradiction).** `GET
   /api/v1/regime` serializes the embedded `sessionmodel.Regime` with **no JSON tags**,
   emitting capitalized Go field names
   [internal/sessionmodel/types.go:186-191](../../internal/sessionmodel/types.go);
   [internal/api/regime.go:97-114](../../internal/api/regime.go). The UI schema compensates
   explicitly [ui/src/api/schemas.ts:354-379](../../ui/src/api/schemas.ts), so the contract
   works today but depends on the Go field names staying unchanged. By contrast
   `panicStatusResponse` and `mutateStatusResponse` use explicit snake_case tags
   [internal/api/panic.go:51-56](../../internal/api/panic.go);
   [internal/api/mutatestatus.go:50-58](../../internal/api/mutatestatus.go) — the read
   endpoints are inconsistent in their wire-key convention.
