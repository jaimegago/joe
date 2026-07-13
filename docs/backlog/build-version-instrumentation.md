# Build & version instrumentation — deferred fast-follows

Status: open

The core of the build-version-instrumentation thread has landed (D-0036):
`internal/buildinfo` as the single source of build truth, ldflags `-X` relocated
off `main` and addressed by full import path, the boot-computed `ui_digest`,
`GET /api/v1/version` with `/status` repointed, the `joe_build_info` gauge, and a
`.goreleaser.yaml` scaffold validated in CI via `goreleaser build --snapshot
--clean`. This file stays **open** for the residual work below.

## Deferred fast-follows

### 1. goreleaser publishing flip (distribution-posture change) — RESOLVED (D-0091)

Done in session `release-pipeline-01`: `.goreleaser.yaml` now publishes on a
`v`-prefixed tag push via `.github/workflows/release.yml`, with UI staging
guaranteed by a goreleaser `before.hooks` entry rather than a workflow-only
step. The residual — the actual tag cut and the update-at-tag-time doc sweep —
is tracked in `docs/backlog/release-pipeline.md`, not here.

### 2. (trivial, do-when-needed) numeric `joe_build_time_seconds` gauge

`joe_build_info` carries `build_time` as a *label* (a string), which is good for
identity but not for math. A separate numeric gauge `joe_build_time_seconds` set
to the build's Unix epoch seconds would enable build-age PromQL
(`time() - joe_build_time_seconds`) and alerting on stale binaries. Cheap: derive
it from the same `BuildTime` (or inject a parallel epoch value) and register it in
the metrics-setup layer beside `joe_build_info`.

### 3. (trivial, do-when-needed) build-time-injected digest cross-check

`ui_digest` is deliberately self-derived at boot so it cannot be absent or
disagree with the embedded bytes. For supply-chain attestation you could ALSO
inject a digest at build time (computed over the staged `dist` before linking)
and have the binary cross-check the injected value against the boot-computed one,
logging/failing loudly on mismatch. This catches a class of tamper/embed-skew the
self-derived digest alone cannot (it would silently agree with whatever was
embedded). Only worth it once there is a real attestation requirement.
