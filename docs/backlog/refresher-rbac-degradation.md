# Backlog — Refresher per-resource-type degradation follow-ups (deferred from refresher-rbac-degradation / D-0093)

Status: open

D-0093 landed per-resource-type **forbidden** degradation in the **kubernetes**
refresher only: a forbidden list error records a skip and continues, the tick
still applies its delta, and a non-empty skip set writes a third `degraded`
component status with a summary. This file carries the work that session
explicitly deferred.

## 1. SelfSubjectAccessReview permission preflight in the connectivity test

The "Test Connection" success copy now states its scope (reachability +
authentication only; list permissions are exercised by the background refresher).
The natural next step is a **preflight** in the connectivity check that runs a
`SelfSubjectAccessReview` (SSAR) for the resource types the refresher lists, so a
human sees the permission gaps **at test time** rather than after the first
refresh interval.

- **Dependency.** This handler work must land **together with** the open
  [`governed-connectivity-check-surface`](governed-connectivity-check-surface.md)
  item — that item is already reworking the connectivity-check surface (legacy
  `handleTestComponent` vs. the admin credential-status Probe). Adding an SSAR
  preflight to the wrong/soon-to-be-retired handler would be throwaway work. Pick
  these up in the same change so the preflight lands on the surviving surface.

## 2. Generalize degradation to the aws, azure, and git refreshers

The aws, azure, and git refreshers share the **hard-fail-on-first-error** pattern
the kubernetes refresher had before D-0093 (`refreshComponent` returns the first
error, which the loop logs and stamps as a sync error; no partial delta). Each
should gain the same partial-tolerance shape, adapted to its own "you may not
read this" error signal:

- **aws** — access-denied is `AccessDenied` / `UnauthorizedOperation` in the
  AWS SDK error shape (`smithy.APIError` code), not an apimachinery typed error.
- **azure** — `*azcore.ResponseError` with `StatusCode == 403`.
- **git** — provider-specific (403/permission on a repo or ref).

The `degraded` status, `store.UpdateSyncState` seam, transition-based logging, and
`summarizeSkips`-style summary are already in place and reusable; only the
per-refresher "is this a permission denial?" predicate and the skip plumbing are
new. Consider hoisting a shared skip/summary helper if the three converge.

## 3. Repo-wide source→component terminology sweep in `internal/coreagent`

D-0021 renamed the entity to "component", but `internal/coreagent` still carries
D-0021 residue: `refreshComponent`/`refreshK8sComponent` and their siblings name
their parameter `source *store.Component`, and some log/field wording still reads
"source". D-0093 fixed only the strings it was already editing (per its scope
guard) and deliberately did **not** sweep the package. Do the full sweep as its
own change so the diff is a clean rename, not entangled with behavior.

## 4. Structured per-resource-type skip field (if the UI needs per-type affordances)

Today the skip set is summarized into the single `last_error` string and the UI
renders that text under a `degraded` badge (no new API field, per D-0093's
minimal-UI scope). If the component page later wants **per-type** affordances
(e.g. a list of skipped types each with a "grant this" hint, or a filter), add a
**structured** skip field to the component read model
(`componentView` in `internal/api/components.go`) carrying `[]{type, reason}`,
and render it. Deferred until there is a concrete UI need — the string summary is
sufficient for the shipped degraded surface.
