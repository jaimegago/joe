# Gate build cache — Unit Tests spends most of 219s compiling

Status: open
Priority: next

**Unit Tests is the gate's critical path at 219s, and most of that is
compilation rather than test execution.**

Measured 2026-08-18: locally, with a warm build cache, the `internal/...`
packages sum to roughly 60s — `internal/api` 19.1s, `internal/coreagent` 8.4s,
`internal/adapters/aws` 6.8s, `internal/adapters/git` 5.2s,
`internal/adapters/datastore/redis` 4.6s, then a long tail under 4s. In CI the
same suite takes 219s.

## The cause

`.github/workflows/tests.yml` caches **`~/go/pkg/mod`** — the module cache —
and not **`~/.cache/go-build`**, the build cache. So every gate run downloads
nothing and compiles everything.

`actions/setup-go@v5` handles both when given `cache: true`, which
`post-merge.yml` already does and the gate jobs do not — they call `setup-go`
without it and then add a manual `actions/cache` step for the module path alone.

## The work

Cache the build output across gate runs, keyed so it invalidates correctly.
Either add `~/.cache/go-build` to the existing `actions/cache` step, or drop the
manual step and let `setup-go` do both. Prefer whichever leaves one mechanism
rather than two.

Apply it to every Go gate job — `unit-tests`, `integration-tests`, `lint` — since
they each pay the same compile cost.

**Measure before and after** and record both numbers. The claim is that the gate
is compile-bound, and it deserves a number rather than an assumption.

## What this is not

**Not `t.Parallel()`.** That appears zero times in the suite today, and adding it
is a real option, but it is not this item and would not fix this: the packages
sum to ~60s of actual test time, so parallelism can win at most a minute of the
219s. The other ~2.5 minutes is compilation and only caching touches it.

Filed from the gate tier split, 2026-08-18. joe-pm
`threads/gate-tier-split.md` carries the measurement.
