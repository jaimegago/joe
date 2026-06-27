# Operational Modes — UI/API Exposure Audit

**Scope:** How Joe's operational modes are exposed for UI consumption. Inventory and
gaps only — no fixes proposed.

**Method:** Established from the live working tree (branch `main`). Every factual claim
cites `file:line`. Doc/memory/prior-doc claims were re-verified against code; drift and
discrepancies are recorded in the final section rather than silently reconciled.

**Note on prior work:** no standalone `incident-mode-status` investigation exists
in the tree. A prior version of *this* file existed (covering only panic/incident/captain,
three axes) and was used as a starting point; it has since drifted on the panic/safe-mode
axis (a UI banner and a typed denial now exist that it recorded as absent). All of its
claims were re-verified here; drift is logged in §5.

## The four axes

The task frames up to four operational axes. The first three are runtime states that can
flip mid-session; the fourth is the baseline posture. They are **independent and
composable**, not one enum.

| # | Axis | Entered via | State lives in |
|---|------|-------------|----------------|
| 1 | Read mode (read-only vs read-write) | — (**no such flag exists**, see §1) | — |
| 2 | Panic / safe mode | `joe panic`, `kill -USR1`, `POST /api/v1/panic` | `internal/safety` + persisted file + cluster row |
| 3 | Incident regime + captain gate | `POST /api/v1/regime/declare`; gate in `internal/captaingate` + `internal/sessiongate` | `system_regime` / `session_captains` tables |
| 4 | Normal | none of the above active | absence of 2/3; baseline posture is RBAC (see §4) |

All `/api/v1` routes sit behind `auth.EdgeAuth` ([cmd/joe/server.go:776-789](../../cmd/joe/server.go));
the source-keyed `rbac.EnforcementMiddleware` ([cmd/joe/server.go:787](../../cmd/joe/server.go))
only fires on paths carrying a `sourceID`. The read endpoints below carry no `sourceID`,
so they are gated by **authentication only**, not per-zone RBAC. `apiPrefix = "/api/v1"`
([internal/api/constants.go:7](../../internal/api/constants.go)).

---

## 1. Axis 1 — Read mode (`operational_mode` flag): **ABSENT**

**Resolved answer: the flag does not exist in the current code, and never has in this
repo's git history.**

- No `operational_mode` / `OperationalMode` / `ReadMode` / `read_mode` / `ReadOnlyMode` /
  `ReadWriteMode` identifier exists anywhere in the tree (`grep -rniI` over all `.go`,
  `.ts`, `.tsx` returns nothing for those tokens). The only `read_only` hits are unrelated:
  the **session-sharing** field `read_only` on `GET /sessions/{id}` (non-owner viewing a
  shared chat) [ui/src/api/schemas.ts:126-129](../../ui/src/api/schemas.ts), which is a
  per-session viewer flag, not a daemon-wide read/write posture.
- No git history for the flag: `git log -S "operational_mode" --all` and
  `git log -S "ReadMode" --all` both return zero commits.
- The server `Config` struct has no read-mode field
  ([internal/config/config.go](../../internal/config/config.go) — grep for `Mode` finds
  only `EmbeddingModel`, comments, no operational-mode toggle).
- System-prompt assembly does **not** branch on a read mode. All prompt strings live in
  `internal/prompts/` (`joefile.go`, `knowledge.go`, `observe.go`, `prompts.go`,
  `zones.go`); none reference a read/write operational mode (grep for
  `read_only`/`operational`/`read mode` over `internal/prompts/` returns only incidental
  comment text [internal/prompts/zones.go:142](../../internal/prompts/zones.go)).
- No `OASIS` launch config carries it either — `OASIS`/`oasis` references are eval-suite
  scenarios and `oasisctl` API-compat notes only
  ([internal/api/tasks.go:863](../../internal/api/tasks.go)),
  not a daemon read-mode setting.

**Where the read/write *concept* actually lives now** (subsumed, not a global flag):

- **RBAC per-zone actions.** The closest analog to "read-only vs read-write" is the
  per-zone allowed-action set. `ActionRead` covers read-only (T1) observations
  ([internal/rbac/zones.go:11](../../internal/rbac/zones.go)); a zone grants some subset of
  actions. This is **per-zone and per-caller**, not a single system-wide posture.
- **Safety tier defaults.** `safety.DefaultPolicy()` gates T2/T3 confirmation
  ([internal/safety/policy.go:60-63](../../internal/safety/policy.go)); T1 is always
  read-only ([internal/safety/tier.go:7](../../internal/safety/tier.go)). Again, this is a
  per-tool tiering policy, not a reportable read/write switch.

**Read path for axis 1 — PARTIAL (RBAC posture only, no global read mode).**

- There is **no** endpoint that reports a global "current read mode."
- The nearest readable signal is `GET /api/v1/me`, which returns the caller's reachable
  zones and each zone's `allowed_actions`
  ([internal/api/currentuser.go:29-52](../../internal/api/currentuser.go)), plus
  `rbac_enabled` ([currentuser.go:37](../../internal/api/currentuser.go)). This tells a
  client what the *caller* may do per zone — not whether the daemon is globally read-only.
- **Source of truth:** the per-principal RBAC grants the policy engine reads for every
  decision, surfaced on `/me` ([currentuser.go:44-51](../../internal/api/currentuser.go)).
  Survives restart: YES (DB-backed RBAC policies).

**Flag existence: ABSENT.** **Read path (global read mode): ABSENT** (only a per-zone RBAC
posture via `/me` is readable).

---

## 2. Axis 2 — Panic / safe mode — read path **PRESENT**

- **Read endpoint:** `GET /api/v1/panic/status`
  - Registered: [internal/api/panic.go:26](../../internal/api/panic.go)
    (`registerPanicRoutes`, prefix `/api/v1`), wired at
    [internal/api/server.go:111](../../internal/api/server.go).
  - Handler: `handlePanicStatus` [panic.go:98-122](../../internal/api/panic.go).
  - Response shape: `panicStatusResponse{ safe_mode bool, triggered_at, trigger_source,
    trigger_reason }` with explicit snake_case JSON tags
    [panic.go:39-44](../../internal/api/panic.go). When neither safe mode nor panic is
    active it returns just `{safe_mode:false}` and omits the detail fields
    [panic.go:99-102](../../internal/api/panic.go).
  - **Gating:** authentication only (EdgeAuth chain). No `sourceID` in the path, so
    `rbac.EnforcementMiddleware` does not fire; the handler performs no per-call RBAC.
- **Source of truth:**
  - In-memory: `safety.safeModeActive atomic.Bool`
    [internal/safety/safemode.go:18](../../internal/safety/safemode.go) and
    `safety.panicked atomic.Bool` [internal/safety/panic.go:31](../../internal/safety/panic.go).
  - Persisted (local): `<joeDir>/panic.state`, `panicStateFile = "panic.state"`
    [internal/safety/panic_state.go:13,29](../../internal/safety/panic_state.go);
    `joeDir = paths.JoeDirPath()` = `~/.joe` (`JoeDir = ".joe"`)
    [internal/paths/defaults.go:11,30](../../internal/paths/defaults.go).
  - Persisted (cluster): `ClusterPanicStore` interface, DB-backed `cluster_panic_state`
    row [internal/safety/panic.go:24-37](../../internal/safety/panic.go), registered at
    startup [cmd/joe/server.go:410-411](../../cmd/joe/server.go).
  - **Survives restart: YES.** Startup reads both the local `panic.state` file *and* the
    cluster row and calls `safety.ActivateSafeMode()` if either is set
    [cmd/joe/server.go:413-424](../../cmd/joe/server.go). The read endpoint reports from
    the in-memory atomics first ([panic.go:99](../../internal/api/panic.go)) and reads the
    file only to enrich `triggered_at`/`trigger_source`/`trigger_reason`
    ([panic.go:110-121](../../internal/api/panic.go)).

---

## 3. Axis 3 — Incident regime + captain gate

### 3a. Incident regime — read path **PRESENT**

- **Read endpoint:** `GET /api/v1/regime`
  - Registered: [internal/api/regime.go:54](../../internal/api/regime.go)
    (`registerRegimeRoutes`), wired at [server.go:124](../../internal/api/server.go).
    (Routes register only when `services.SessionModel` is non-nil
    [regime.go:40-42](../../internal/api/regime.go).)
  - Handler: `read` [regime.go:59-66](../../internal/api/regime.go) → `repo.GetRegime`.
  - Response shape: `sessionmodel.Regime` serialized with **no JSON tags**, so wire keys
    are the exported Go field names `{ Mode, DeclaredAt, DeclaredByPrincipal, DeclaredKind }`
    [internal/sessionmodel/types.go:101-106](../../internal/sessionmodel/types.go). `Mode`
    is `"normal"` or `"incident"` [types.go:87-88](../../internal/sessionmodel/types.go).
  - **Gating:** authentication only; the `read` handler performs no RBAC. (The
    `declare`/`resolve` *write* paths are RBAC-gated and seam-gated
    [regime.go:55-56](../../internal/api/regime.go), but those are not the read path.)
- **Source of truth:** `system_regime` single-row table via `GetRegime`
  [internal/sessionmodel/repository.go:622](../../internal/sessionmodel/repository.go).
  **Survives restart: YES** (SQLite-backed).

### 3b. Captain (who is the current captain) — read path **ABSENT**

- **No GET / read endpoint exists for the current captain.** `registerCaptainRoutes`
  registers only `POST` routes — `attach`, `heartbeat`, `transfer/begin|confirm|cancel`
  [internal/api/captain.go:52-56](../../internal/api/captain.go). No `GET .../captain`
  route exists.
- The regime read endpoint does **not** include the captain: `Regime` has only
  `Mode/DeclaredAt/DeclaredByPrincipal/DeclaredKind`
  [types.go:101-106](../../internal/sessionmodel/types.go).
- The repository *can* answer it — `GetActiveCaptain`
  [repository.go:731](../../internal/sessionmodel/repository.go) and
  `CurrentCaptainPrincipal` [repository.go:810](../../internal/sessionmodel/repository.go)
  — but no API handler calls either; the only API-layer session-model read is `GetRegime`
  ([regime.go:60](../../internal/api/regime.go)).
- **Source of truth (not exposed):** `session_captains` table; the current captain is the
  row with `DetachedAt == nil`
  [types.go:135-154](../../internal/sessionmodel/types.go);
  [repository.go:731](../../internal/sessionmodel/repository.go). SQLite, would survive
  restart — but is unreadable via any API.

---

## 4. Axis 4 — Normal

- "Normal" is not a stored or independently readable state; it is the **absence** of
  panic/safe mode and incident regime. The regime enum's normal value is
  `RegimeModeNormal = "normal"`
  [internal/sessionmodel/types.go:87](../../internal/sessionmodel/types.go), which `GET
  /api/v1/regime` returns when no incident is active (Mode `"normal"`); and `GET
  /api/v1/panic/status` returns `{safe_mode:false}` when not in safe mode
  [internal/api/panic.go:99-102](../../internal/api/panic.go). A client infers "normal" by
  reading those two endpoints and finding both clear.
- The baseline posture that still applies in normal operation is the RBAC per-zone posture
  from §1 (readable via `GET /api/v1/me`), not a dedicated read-mode flag.

---

## 5. Read-path inventory (summary)

| Axis | Read endpoint | Source of truth | Survives restart | Status |
|------|---------------|-----------------|------------------|--------|
| 1 Read mode (global flag) | none | n/a (flag absent) | n/a | **ABSENT** (flag never existed) |
| 1 RBAC posture (nearest analog) | `GET /api/v1/me` ([currentuser.go:29-52](../../internal/api/currentuser.go)) | per-principal RBAC grants | YES (DB) | PARTIAL — per-zone, not global |
| 2 Panic / safe mode | `GET /api/v1/panic/status` ([panic.go:26,98-122](../../internal/api/panic.go)) | atomics + `~/.joe/panic.state` + cluster row | YES (file + cluster row) | **PRESENT** |
| 3a Incident regime | `GET /api/v1/regime` ([regime.go:54,59-66](../../internal/api/regime.go)) | `system_regime` table ([repository.go:622](../../internal/sessionmodel/repository.go)) | YES (DB) | **PRESENT** |
| 3b Captain | none ([captain.go:52-56](../../internal/api/captain.go) — POST only) | `session_captains` table (unexposed) | YES (DB, unexposed) | **ABSENT** |
| 4 Normal | inferred from 2 + 3a | absence of 2/3 | n/a | derived |

---

## 6. UI coverage matrix

| Axis | UI fetches state? | UI renders indicator? | Evidence |
|------|-------------------|------------------------|----------|
| 1 Read mode (global) | n/a | n/a | No global read mode exists to fetch. |
| 1 RBAC posture | **Yes** | **Partial** | `CurrentUserSchema` carries `allowed_actions` [ui/src/api/schemas.ts:141-150](../../ui/src/api/schemas.ts); `ChatPage` uses zone presence to gate the composer / show an "access pending" state [ui/src/pages/ChatPage.tsx:173-179](../../ui/src/pages/ChatPage.tsx). No dedicated read-only/read-write *indicator chip* for the daemon. |
| 2 Panic / safe mode | **Yes** | **Yes (real, wired)** | `fetchPanicStatus` [ui/src/api/panic.ts:8](../../ui/src/api/panic.ts) → `usePanicStatus` (polls every 15s) [ui/src/hooks/usePanicStatus.ts:17-28](../../ui/src/hooks/usePanicStatus.ts) → `SafeModeBanner` renders a red banner only when `safeMode` is true [ui/src/components/layout/SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx), mounted in the app shell [ui/src/components/layout/AppShell.tsx:14](../../ui/src/components/layout/AppShell.tsx). `PanicStatusSchema` maps the snake_case wire keys [ui/src/api/schemas.ts:192-204](../../ui/src/api/schemas.ts). |
| 3a Incident regime | **Yes** | **Yes (real, wired)** | `fetchRegime` [ui/src/api/regime.ts:7](../../ui/src/api/regime.ts) → `useRegime` (polls every 30s) [ui/src/hooks/useRegime.ts:10-21](../../ui/src/hooks/useRegime.ts) → `IncidentBanner` renders an amber banner only when `mode==='incident'` [ui/src/components/layout/IncidentBanner.tsx:9-28](../../ui/src/components/layout/IncidentBanner.tsx), mounted at [AppShell.tsx:15](../../ui/src/components/layout/AppShell.tsx). `RegimeSchema` maps the capitalized wire keys [ui/src/api/schemas.ts:171-183](../../ui/src/api/schemas.ts). |
| 3b Captain | **No** | **No** | No `captain` references in `ui/src` other than a comment in `SafeModeBanner.tsx` (grep: only [SafeModeBanner.tsx:11](../../ui/src/components/layout/SafeModeBanner.tsx)). No api-client function, no component. |

### Specific indicators requested

- **Read-only / read-write indicator — ABSENT** as a daemon-wide chip. There is no global
  read-mode flag to indicate (§1). The UI does react to the *per-zone RBAC* posture
  (access-pending empty state) [ChatPage.tsx:173-179](../../ui/src/pages/ChatPage.tsx), and
  to *per-session* read-only sharing (a "Read-only" badge on shared sessions)
  [ChatPage.tsx:245](../../ui/src/pages/ChatPage.tsx) — but neither is a daemon read-mode
  indicator.
- **Active-incident banner — PRESENT.** Real, bound to live state via
  `useRegime`/`fetchRegime`, not a scaffold
  [IncidentBanner.tsx:9-28](../../ui/src/components/layout/IncidentBanner.tsx);
  [AppShell.tsx:15](../../ui/src/components/layout/AppShell.tsx).
- **Safe-mode / panic indicator — PRESENT.** Real, bound to live state via
  `usePanicStatus`/`fetchPanicStatus`, not a scaffold
  [SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx);
  [AppShell.tsx:14](../../ui/src/components/layout/AppShell.tsx). (This is the largest drift
  from the prior version of this doc, which recorded it ABSENT.)

---

## 7. Failure feedback (mode-specific denial messaging)

The shared dispatch lives in `classifyWriteFailure`, which maps a typed per-tool error to
a stable `error_code` [internal/api/writefailure.go:36-51](../../internal/api/writefailure.go).
Codes are defined at [internal/api/constants.go:26-28](../../internal/api/constants.go):
`zone_denial`, `incident_mode`, `safe_mode`. The classifier is injected into the loop via
`agentloop.WithToolErrorClassifier(classifyWriteFailure)`
[internal/api/tasks.go:379](../../internal/api/tasks.go); `error_code` is emitted at both
per-tool and turn level [tasks.go:85,121,585,637](../../internal/api/tasks.go). The UI
maps codes to sentences in `writeFailureMessage`
[ui/src/hooks/useChat.ts:64-75](../../ui/src/hooks/useChat.ts).

### Read mode (RBAC posture) denial — **PRESENT (typed as `zone_denial`)**

- A write blocked because the caller lacks the zone action surfaces as
  `access.ErrPermissionDenied` → `zone_denial`
  [writefailure.go:44-45](../../internal/api/writefailure.go) → "Access to this zone has
  not been granted to you…" [useChat.ts:66-67](../../ui/src/hooks/useChat.ts). This is the
  RBAC posture, not a "global read mode" denial (no such mode exists). Distinguishable from
  a generic 403 by the `zone_denial` code.

### Safe mode allowing only Tier-1 — **PRESENT (typed)**

- The executor denies T2/T3 in safe mode with the typed sentinel
  `safety.ErrSafeModeActive`
  [internal/safety/safemode.go:14-16](../../internal/safety/safemode.go), returned at
  [internal/tools/executor.go:226-232](../../internal/tools/executor.go).
- `classifyWriteFailure` matches it via `errors.Is` → `safe_mode`
  [writefailure.go:46-47](../../internal/api/writefailure.go).
- UI maps `safe_mode` → "System is in safe mode. Only read-only operations are permitted —
  run `joe unlock` to resume writes." [useChat.ts:70-71](../../ui/src/hooks/useChat.ts).
- Distinguishable from a generic tool failure (typed code) and from `incident_mode` /
  `zone_denial`. (This is drift from the prior doc, which recorded this as PARTIAL /
  untyped string.)

### Incident captain gate denying a non-captain mutation — **PRESENT (typed as `incident_mode`)**

- The executor wrapper returns a typed `*captaingate.GateRefusalError` on refusal
  [internal/captaingate/captaingate.go:61-76,164-178](../../internal/captaingate/captaingate.go);
  the gate is wired into the user task loop
  [internal/api/tasks.go:304](../../internal/api/tasks.go) and the Core Agent path
  [cmd/joe/server.go:675](../../cmd/joe/server.go).
- `classifyWriteFailure` maps it → `incident_mode`
  [writefailure.go:42-43](../../internal/api/writefailure.go); UI → "System is in incident
  mode. Writes are temporarily blocked." [useChat.ts:68-69](../../ui/src/hooks/useChat.ts).
- **Caveat (granularity loss):** `sessiongate.Check` distinguishes four refusal outcomes —
  regime mismatch, wrong session, pending-captain/null-authority, and non-captain principal
  [internal/sessiongate/sessiongate.go:108-134](../../internal/sessiongate/sessiongate.go)
  — and `GateRefusalError` carries the `CaptainSessionID` redirect target
  [captaingate.go:61-76](../../internal/captaingate/captaingate.go). `classifyWriteFailure`
  collapses them all into the single `incident_mode` code, discarding the
  `CaptainSessionID`, so the user cannot tell "no captain attached yet" from "you are not
  the captain."

### Incident regime declare/resolve write denials

- These return generic HTTP errors keyed on RBAC/seam state, not a mode-specific typed
  code — e.g. `403` for missing capability / inert Joe-autonomous seam, `409` for
  state-machine violations ([regime.go:55-56](../../internal/api/regime.go) register the
  write paths; the handlers return RBAC/seam/conflict errors). These are the regime
  *control* surface, not the agentic write path the captain gate governs.

---

## 8. Gap list

**Axes whose current state cannot be read via an API:**

1. **Read mode — no global flag and no global read-path** (§1). There is no
   `operational_mode`/read-mode flag in code or git history; only a per-zone RBAC posture
   is readable, via `GET /api/v1/me`
   [internal/api/currentuser.go:29-52](../../internal/api/currentuser.go). "Is the daemon
   globally read-only" is not answerable because the concept does not exist as a single
   state.
2. **Captain — no read endpoint at all.** Only `POST` mutation routes exist
   [internal/api/captain.go:52-56](../../internal/api/captain.go); the repository can
   answer "who is the captain" ([repository.go:731,810](../../internal/sessionmodel/repository.go))
   but no handler exposes it. "Who is the current captain" is unanswerable by any client.

**Axes with a read path but no UI indicator:**

3. **Captain — no UI consumption** (and no read path to consume). No `ui/src` component or
   api-client touches captain state (grep empty apart from a comment).

**Failure-feedback gaps:**

4. **Captain-gate refusal granularity is collapsed** — four distinct gate outcomes
   [sessiongate.go:108-134](../../internal/sessiongate/sessiongate.go) reduce to one
   `incident_mode` message [writefailure.go:42-43](../../internal/api/writefailure.go); the
   `CaptainSessionID` redirect target is discarded before reaching the user.

**Closed since the prior version of this doc (no longer gaps):**

- Panic / safe-mode UI indicator — now PRESENT
  [SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx).
- Safe-mode denial typing — now typed `safe_mode`
  [safemode.go:14-16](../../internal/safety/safemode.go);
  [writefailure.go:46-47](../../internal/api/writefailure.go).

**Coverage summary:**

| Axis | Read API | Survives restart | UI fetch | UI indicator | Typed denial |
|------|----------|------------------|----------|--------------|--------------|
| 1 Read mode (global flag) | ABSENT (flag absent) | n/a | n/a | ABSENT | n/a |
| 1 RBAC posture (analog) | PARTIAL (`/me`) | YES | YES | PARTIAL (gating, no chip) | PRESENT (`zone_denial`) |
| 2 Panic / safe mode | PRESENT (`/panic/status`) | YES (file + cluster) | YES | PRESENT (banner) | PRESENT (`safe_mode`) |
| 3a Incident regime | PRESENT (`/regime`) | YES (DB) | YES | PRESENT (banner) | PRESENT (`incident_mode`, via gate) |
| 3b Captain | ABSENT | YES (DB, unexposed) | NO | NO | PRESENT (`incident_mode`, collapsed) |
| 4 Normal | derived | n/a | derived | derived | n/a |

---

## 9. Code-vs-doc contradictions encountered

1. **`operational_mode` flag — does not exist (contradicts the task's premise).** The task
   background says the flag "was previously observed to branch the system-prompt assembly
   and the exposed tool surface." No such flag exists in current code or anywhere in git
   history (§1). The read/write concept is realized through RBAC per-zone actions and the
   safety tier policy, neither of which is a single global mode. If it was ever observed, it
   was in a different repo or an out-of-tree launch config, not this daemon.

2. **Prior version of this doc is stale on the panic axis.** The earlier
   `operational-modes-ui-status.md` recorded "Safe-mode / panic indicator — ABSENT" and
   "Safe mode allowing only Tier-1 — PARTIAL (human-readable string, not typed)." Both have
   since changed: a wired `SafeModeBanner`
   [SafeModeBanner.tsx:13-38](../../ui/src/components/layout/SafeModeBanner.tsx) and a typed
   `safety.ErrSafeModeActive` → `safe_mode` code
   [safemode.go:14-16](../../internal/safety/safemode.go);
   [writefailure.go:46-47](../../internal/api/writefailure.go) now exist (recent commits
   `3b310ee`/`258fb3e`).

3. **`cmd/joe-core` binary does not exist.** `MEMORY.md` states "Two binaries: `cmd/joe` +
   `cmd/joe-core` (:7777)". The live tree has only `cmd/joe`; the server entrypoint is
   [cmd/joe/server.go](../../cmd/joe/server.go), matching `CLAUDE.md`'s single-binary model.
   The two-binary memory is stale drift.

4. **Panic-state persistence has a cluster component not in CLAUDE.md/MEMORY.** Both docs
   say panic state is persisted to `~/.joe/panic.state` (YAML) only. Code confirms that
   file [panic_state.go:13,29](../../internal/safety/panic_state.go) but *also*
   persists/reads a DB-backed `ClusterPanicStore` row
   [safety/panic.go:24-37](../../internal/safety/panic.go);
   [cmd/joe/server.go:410-424](../../cmd/joe/server.go). The persisted location is file
   **plus** cluster row, not file alone.

5. **Deny-only captain-gate claim — VERIFIED, no contradiction.**
   [captaingate.go:17](../../internal/captaingate/captaingate.go) asserts the gate is
   DENY-ONLY. Confirmed: `sessiongate.Check` only ever returns `Decision{Allow: true|false}`
   and never elevates RBAC
   [sessiongate.go:84-136](../../internal/sessiongate/sessiongate.go); on allow the wrapper
   does §B1 principal substitution but does not grant authority
   [captaingate.go:164-178](../../internal/captaingate/captaingate.go).

6. **Endpoint paths — VERIFIED, no contradiction.** Task background names `POST
   /api/v1/panic`, `joe unlock`, and `/regime/*`; all match code: panic/unlock at
   [panic.go:25-27](../../internal/api/panic.go), regime at
   [regime.go:54-56](../../internal/api/regime.go), captain under
   `/api/v1/agent-sessions/{id}/captain/*` [captain.go:52-56](../../internal/api/captain.go).

7. **Regime wire contract is fragile (observation, not a doc contradiction).** `GET
   /api/v1/regime` serializes `sessionmodel.Regime` with **no JSON tags**, emitting
   capitalized Go field names [types.go:101-106](../../internal/sessionmodel/types.go). The
   UI schema compensates explicitly
   [ui/src/api/schemas.ts:171-183](../../ui/src/api/schemas.ts), so the contract works today
   but depends on the Go field names staying unchanged. By contrast `panicStatusResponse`
   uses explicit snake_case tags [panic.go:39-44](../../internal/api/panic.go) — the two
   read endpoints are inconsistent in their wire-key convention.
