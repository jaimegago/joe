# Skills governance hardening — admin-gate the HTTP surface, audit lifecycle events, load-time integrity

Status: open
Priority: next

> **Progress (skills-governance-hardening session, D-0075).** Items **1**
> (admin-gate the mutating skills HTTP endpoints) and **4** (fix the stale
> `internal/skills/skill.go` package comment) are **discharged** by the
> `skills-governance-hardening: admin-gate the mutating skills HTTP endpoints`
> commit — the mutating routes (reload/approve/reject) now gate through
> `requireAdmin`, `handleList` stays authenticated-only by design, and the
> two stale doc comments (the `skills.go` header and the `skill.go` package
> doc) are corrected. That commit also corrected the stale CLAUDE.md
> "RBAC enforcement middleware fires only on paths with a componentID" line
> (false since the Phase-E demotion, D-0008). Items **2** (durable audit Kind
> + migration for skill-lifecycle events) and **3** (load-time content
> integrity) **remain open** with the descriptions below unchanged. Status
> stays **open** until 2 and 3 land.

## Context

A read-only investigation of the skills subsystem (`internal/skills/`, its HTTP
surface in `internal/api/skills.go`, and the `joe skills` CLI) surfaced four
governance gaps. The subsystem loads judgment-elicitation documents from
`~/.joe/skills/` and exposes install/list/remove/update/approve/reject/reload
lifecycle operations over both the CLI and the server's REST surface, but the
governance posture around that surface has not caught up with the comparable
admin surfaces.

This investigation was run **read-only at HEAD `df04650`**. The file:line
coordinates below are accurate as of that commit but should be **re-derived
before implementation** — the tree moves and these citations will drift.

## Items

### 1. Admin-gate the skills HTTP endpoints (fix-before-launch candidate) — DONE (D-0075)

The four handlers in `internal/api/skills.go` — `handleReload`,
`handleList`, `handleApprove`, `handleReject` (registered in
`registerSkillsRoutes` as `POST .../skills/reload`, `.../skills/approve`,
`.../skills/reject`, and the list route) — sit behind **edge auth only** and do
**not** call `server.requireAdmin`. The comparable admin surfaces
(`internal/api/admin.go`, `internal/api/llmsettings.go`) gate every mutating
handler through `requireAdmin`. As a result, **any authenticated principal can
approve a quarantined skill or force a reload** — promoting untrusted content
into the LLM's decision-time context or reloading the registry — without the
admin gate the rest of the operator surface enforces.

This is the **fix-before-launch candidate** among the four items: it is the
smallest change with the largest exposure delta, and it aligns the skills
surface with an already-established pattern (`requireAdmin`) rather than
inventing new mechanism.

### 2. Write skill lifecycle events to the append-only audit store

The lifecycle operations install / remove / update / approve / reject / reload
currently emit **`slog` lines only** via `auditSkillEvent`
(`internal/skills/install.go`, called from the install/remove/update/approve/
reject paths; the reload path in `internal/skills/watcher.go`). There is **no
skill audit Kind** in the append-only audit store, so these security-relevant
events are not durably recorded alongside the other audited actions and cannot
be queried through the audit surface.

Closing this requires a **new audit Kind** for skill-lifecycle events and a
**migration widening the audit `kind` CHECK constraint** to admit it, then
routing `auditSkillEvent` (or a successor) through the audit store in addition
to (or instead of) the `slog` line.

### 3. Load-time content integrity

`LoadDir` / `Reload` load whatever parses under the skills root and **never
verify loaded content against the lockfile hash**. A direct filesystem write
into `~/.joe/skills/` therefore **bypasses the installer, the trusted-source
policy, and quarantine** entirely: content that never passed through the
governed install path is loaded and surfaced into LLM context as if it had. The
installer computes and records hashes, but the load path does not consult them.

The follow-up is to **decide and implement a load-time integrity posture** —
e.g. verifying loaded skill content against the recorded lockfile hash at
`LoadDir`/`Reload` time and refusing (or quarantining) content that does not
match — so that a filesystem-level bypass cannot silently promote untrusted
skills.

### 4. Fix the stale package comment in `internal/skills/skill.go` — DONE (D-0075)

The package doc comment in `internal/skills/skill.go` still describes a
**phase-1 scope** ("static loading at startup, deterministic keyword routing,
no CLI, no hot reload, no quarantine"). The tree has since grown a CLI
(`joe skills ...`), hot reload (the watcher + reload endpoint), and quarantine
(the approve/reject flow), so the comment is now actively misleading about what
the package supports. Update it to describe the as-built scope.

## References (link, do not duplicate)

- `internal/api/skills.go` — the four ungated HTTP handlers (item 1).
- `internal/api/admin.go`, `internal/api/llmsettings.go` — the `requireAdmin`
  pattern item 1 should adopt.
- `internal/skills/install.go`, `internal/skills/watcher.go` — `auditSkillEvent`
  and the `slog`-only lifecycle emissions (item 2).
- `internal/skills/skill.go` — `LoadDir`/`Reload` (item 3) and the stale package
  comment (item 4).
