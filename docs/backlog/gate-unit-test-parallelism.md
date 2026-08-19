# Unit Tests is execution-bound at ~220s — no test in the suite runs in parallel

Status: open
Priority: next

**`t.Parallel()` appears zero times in the entire test suite.** Unit Tests is the
merge gate's critical path at roughly 220s, and every test in it runs
sequentially.

## Why this is the lever, and how that was established the hard way

This item **replaces `docs/backlog/done/gate-build-cache.md`**, which claimed the
gate was compile-bound and explicitly ruled parallelism out. That claim was wrong
and the change it produced was reverted. The reasoning is recorded because it is
the reason to trust this item:

- The compile-bound diagnosis came from comparing a **warm local Apple Silicon
  run** (`internal/...` packages summing to ~60s) against a **2-core ubuntu
  runner** (217s). Different hardware, different cache state — not a comparison.
- `setup-go@v5` caches `GOCACHE` **by default**. The gate was already caching
  build output in every job, so there was no missing cache to add. The `lint`
  job's comment had said exactly this, with a run citation, since before the
  attempt.
- Adding a per-job cache changed Unit Tests from 219s to 217s and 234s — nothing —
  while doubling the cache footprint past the point of eviction.

**With compilation ruled out, ~220s of wall clock is test execution**, run
strictly sequentially.

## The work

1. **Measure first.** `go test -json ./internal/... ` and rank by elapsed time.
   The known heavy packages locally are `internal/api` (19.1s),
   `internal/coreagent` (8.4s), `internal/adapters/aws` (6.8s),
   `internal/adapters/git` (5.2s), `internal/adapters/datastore/redis` (4.6s).
   Establish the CI ranking rather than assuming it matches.
2. **Find what blocks parallelism.** Shared package-level state, `t.Setenv`
   (which forbids `t.Parallel`), shared database fixtures, and ordering
   assumptions between tests in a file. This is the real work and it is why the
   item is not a one-liner.
3. **Add `t.Parallel()` where it is safe**, heaviest packages first, and verify
   under `-race` — parallelism is exactly where shared state surfaces as a race.
4. **Measure again** and record both numbers.

## Ceiling, stated honestly

Go already runs **different packages** concurrently up to `GOMAXPROCS`, so the
win is within-package, and the runner has 2 cores. **This may buy much less than
it looks like.** Step 1 exists to find that out before step 3 is worth doing: if
the time is spread evenly across many small packages rather than concentrated in
a few large ones, package-level concurrency is already extracting most of it and
this item should be closed as not worth it.

## A constraint worth knowing before starting

**The suite cannot be run under `-race` in CI within default timeouts.**
`internal/api` takes **625s** under `-race` for a single run against 19.1s
without it — roughly 33x. A `-count=5 -race` attempt failed at 601s on both heavy
packages, which is Go's default 10-minute package timeout rather than a hang.
Verifying step 3 under `-race` therefore needs an explicit `-timeout`.

Filed 2026-08-19 from the reverted build-cache attempt. joe-pm
`threads/revert-gate-build-cache.md` carries the measurement.
