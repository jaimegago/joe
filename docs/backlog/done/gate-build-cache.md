# Gate build cache — Unit Tests spends most of 219s compiling

Status: done
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

## Closed — 2026-08-19

Every Go job in `.github/workflows/tests.yml` now caches `~/.cache/go-build`
alongside `~/go/pkg/mod`, through a single explicit `actions/cache` step keyed by
`github.job`.

**The key had to be per-job, and that is the part worth carrying.** `setup-go`'s
own cache key carries platform, arch, Go version and the `go.sum` hash and **no
job component**, so every Go job in the workflow competes for one entry: the
first to finish uploads, the rest skip as already-existing, and later runs restore
whichever job won that race. The jobs compile different trees, so the winner's
build cache is the wrong one for everybody else.

**This reverses a prior ruling and does so explicitly.** The `lint` job carried a
comment ruling out a second `actions/cache` step on the ground that `setup-go`
already covered strictly more — GOCACHE as well as GOMODCACHE. That was correct
about coverage and is reversed on the key-collision ground above, not on the
coverage one. The comment now records the reversal in place rather than leaving
it to be inferred from the diff.

**The measurement that prompted this stands on its own**: `lint` had the build
cache through `setup-go` and ran in 87s; `unit-tests` had only the module cache
and ran in 219s, against packages summing to ~60s locally. The before/after the
item asked for is the gate run on the pull request that lands this.

## Reverted — 2026-08-19

**This item was wrong, its change was reverted, and the "Closed" section above is
left standing as written rather than edited.** What it claimed is what was
believed at the time; the correction belongs after it, not in place of it.

### What was wrong

- **The premise.** `setup-go@v5`'s `cache` input **defaults to true**, so every Go
  job was already caching `GOCACHE` as well as `GOMODCACHE`. There was no missing
  build cache. The `lint` job's comment had stated this, citing run
  `31430252511`, before this item was filed — and the item's "Closed" section
  above records reversing that ruling. It reversed a correct, evidenced claim.
- **The measurement.** "219s against packages summing to ~60s locally" compared a
  warm local Apple Silicon run against a 2-core ubuntu runner. That is not a
  comparison, and it is the entire basis of the compile-bound diagnosis.
- **The result.** Unit Tests: 219s before, **217s and 234s after**. Nothing.

### What it cost

The change **doubled the cache footprint** — removing setup-go's explicit
`cache: true` did nothing, since true is the default, so both mechanisms ran.
Stored on `main` afterwards: ~880MB of `setup-go-*` caches alongside 932MB
(`unit-tests`), 955MB (`lint`), 879MB (`integration-tests`), 888MB (`e2e-tests`)
and 1591MB (`goreleaser-build`) of per-job ones, against a 10GB repository limit.

Eviction followed within hours: the 2026-08-19 nightly logged `Cache not found
for input keys: Linux-go-unit-tests-...`.

### What replaces it

`docs/backlog/gate-unit-test-parallelism.md`. With compilation ruled out, the
~220s is execution, and `t.Parallel()` — which this item explicitly dismissed —
is the remaining lever. That item states its own ceiling honestly and puts
measurement before change, which is what this one failed to do.

**The per-job key observation was correct** and is preserved in the workflow's
do-not-retry marker: setup-go's key genuinely has no job component. It was a true
fact that did not make the change worth making.
