# Unit Tests is ~210s on the gate — two-thirds build and link in a one-package-at-a-time loop, one-third execution

Status: open
Priority: next

**Measured on eleven `main` runs, 2026-11-12 to 2026-11-17: of a median 209s in
the `Run unit tests` step, 72s (35%) is tests executing and 137s (65%) is
everything `go test` does around them.** The execution share was 32–35% in every
one of the eleven runs.

This item used to say the job was execution-bound and that `t.Parallel()` was the
lever. Step 1 of that version was "measure first"; this is what the measurement
found, and it changes the order of the work. joe-pm
`threads/gate-test-time-ranking.md` carries the run identifiers and the method.

## What the earlier version got wrong

Two premises, both checked against the tree and the logs:

- **"With compilation ruled out, ~220s of wall clock is test execution."** It is
  not. The reverted build-cache attempt showed that adding a cache changes
  nothing, which rules out a *missing cache*. It does not rule out the time a
  warm `go test` still spends per package before the test binary runs. Summing
  the `ok <pkg> <seconds>` lines gives 72s; the timestamps around them give 209s.
- **"Go already runs different packages concurrently up to `GOMAXPROCS`."** Not
  on the gate. `make test-unit` (`Makefile:62-67`) is a shell loop that calls
  `go test -v -short -failfast $pkg` once per package, ninety-five times, so no
  two packages ever run at once. The loop replaced a single
  `go test -v -short ./internal/...` in `5ac8058` (2026-02-12, "fix: tests");
  the commit records no reason.

## Where the time is

Median of eleven runs. *Wall* is the gap between a package's `==>` marker and
its `ok` line; *exec* is the seconds on the `ok` line; the difference is build,
vet and link, which this measurement does not break down further.

| Package | Wall | Exec | Build, vet, link |
|---------|-----:|-----:|-----------------:|
| `internal/api` | 48.0s | 31.3s | 16.7s |
| `internal/coreagent` | 14.1s | 11.0s | 3.1s |
| `internal/adapters/k8s` | 13.8s | 0.0s | 13.8s |
| `internal/graph` | 7.4s | 3.4s | 4.1s |
| `internal/access` | 5.3s | 0.0s | 5.3s |
| other 90 packages | 120.5s | 26.7s | 93.8s |
| **all 95** | **209.1s** | **72.4s** | **136.7s** |

Three separate facts are in that table:

1. **The non-execution time is spread.** 31 packages carry a second or more of
   it, the median package 0.87s, and it is paid serially because of the loop.
2. **The execution time is concentrated.** `internal/api` holds 43% of it and
   `internal/api` plus `internal/coreagent` 58%; 63 of the 95 packages execute in
   under 0.1s. So the old go/no-go question has the answer "concentrated", but
   about a third of the job.
3. **Most of the execution time is one fixture.** About 610 of the 3,250
   top-level tests each cost one identical quantum — 0.05s, 0.06s or 0.08s
   depending on the runner — and together hold 63–69% of all execution time
   (median 66%). `TestStore_Driver` (`internal/store/store_test.go:486`) does
   nothing but call `setupTestStore` and costs exactly that quantum in all
   eleven runs; `setupTestStore` (`internal/store/store_test.go:13`) opens an
   in-memory SQLite and applies all 35 migrations. In `internal/api` 308 tests
   sit at one quantum and hold 78% of the package's execution time. The link
   from the quantum to `Migrate()` is that inference, not a profile.

Runner speed moves every number together: the loop took 146s on the fastest
runner and 235s on the slowest, with the ranking and the shares unchanged.

## What a local run shows, and where it disagrees

Apple M1 Pro, 10 cores, warm build cache, go 1.26.0 (the gate pins 1.25), each
with `-count=1`:

| Command | Wall | Sum of package exec |
|---------|-----:|--------------------:|
| `make test-unit` (the gate's loop) | 200s | 80s |
| `go test -p 1 ./internal/...` | 160s | 80s |
| `go test ./internal/...` | 40–47s | 222–232s, inflated by contention |

Same suite, same machine: the loop costs five times what one invocation does.
That ratio is this machine's ten cores and will not transfer to the runner, whose
core count nobody has measured; the item's old "2-core" figure is unverified.

The local ranking agrees with CI at the top (`internal/api`, then
`internal/coreagent`) and on the fixture quantum (0.04s locally). It disagrees
in two places. `internal/adapters/aws` is third locally at 5.6s and executes in
0.0s on CI: one test, `TestAdapter_ConnectValidatesConfig`, takes 5s on this
machine only. And `internal/adapters/k8s` and `internal/access`, third and fifth
on CI by wall, sit 26th and 36th in a local ranking by execution time, because
their cost is link time. A ranking by `go test -json` elapsed cannot see the
larger half of the job, and the default-concurrency one is distorted by ten
packages competing for cores. The CI numbers decide.

## The work, largest measured lever first

1. **Run the packages in one invocation instead of the loop.** Change
   `test-unit` back to a single `go test` over `./internal/...` and measure the
   job on the pull request against the 209s median. Before changing it, settle
   what the loop was for: per-package `==>` headers and `-failfast` stopping at
   the first failing package are the two things it provides that a single
   invocation does not. Record the runner's core count in the same pull request
   (`nproc`), since it bounds this step and step 3. `Makefile` is in the gate's
   `go` filter, so the pull request measures itself.
2. **Look at the fixture before parallelising around it.** If one
   migrate-per-test is two-thirds of execution, migrating once per package and
   handing each test a copy is a lever that does not depend on core count.
   Confirm the inference with a profile first. This is a design question, not a
   settled step.
3. **`t.Parallel()` in `internal/api` and `internal/coreagent` only**, and only
   if steps 1 and 2 leave enough execution time to be worth it. The ceiling is
   the 42s those two packages execute for, 20% of the job, divided by whatever
   the runner's cores allow. The inventory below says what stands in the way.
4. **Measure again** after each step and record the numbers here.

Whichever change lands first also corrects the comment at
`.github/workflows/tests.yml:297-298`, which still says the job is
execution-bound and that parallelism is the lever.

## What blocks `t.Parallel()` in the two heavy packages

Read-only inventory at `f044285`. `t.Parallel()` appears nowhere under
`internal/` or `test/`. Neither package has a `TestMain`. Both passed
`go test -count=1 -shuffle=<seed>` under two seeds, so no ordering dependence
showed.

**`internal/api`** — 82 test files, 528 top-level tests, 46 files in package
`api` and 36 in `api_test`, one test binary.

- **`t.Setenv`: 15 calls in 9 tests**, which Go refuses to combine with
  `t.Parallel()`: `components_promotion_candidates_test.go:60-63` and `:106`,
  `llmadmin_test.go:747-750` (with an `os.Unsetenv` at `:749`),
  `components_adapter_dispatch_test.go:88`,
  `components_promote_activation_test.go:57`, `:97` and `:132`,
  `components_git_promotion_test.go:250` (sets `HOME`),
  `components_promote_uniqueness_test.go:64-65`.
- **One package-level seam, reassigned by up to 11 tests**: `newModelAdapter`
  (`models.go:15`), swapped and restored in `models_test.go:121`, `:161`,
  `:193`, `:275` and `:379`, and through the helpers `stubSwapFactory`
  (`llmchain_swap_test.go:49`, 4 callers) and `stubAdapterFactory`
  (`llmadmin_test.go:221`, 2 callers).
- **One process-global logger swap**: `slog.SetDefault` at `tasks_test.go:508`.
- **No shared database fixture.** `:memory:` appears 41 times in 27 files and
  each test opens its own; `internal/store/store.go:79-92` and `:136` pin an
  unshared in-memory DSN to a one-connection pool, so no two tests see one
  database.
- **No mutable package-level test state.** The eight package-level variables in
  test files are lookup tables nothing writes to
  (`admin_audit_guard_test.go:90`, `:107`, `:123`, `autorun_test.go:42`,
  `components_readmodel_test.go:19`, `components_test.go:188`,
  `llm_admin_guard_test.go:59`, `regime_test.go:337`).

Those tests, at most 21, can stay sequential: Go finishes a package's
sequential tests before it resumes the parallel ones, so they never overlap the
rest. That leaves roughly 500 tests with no blocker found by this inventory,
which is a grep and two shuffled runs, not a `-race` run.

**`internal/coreagent`** — 29 test files, 313 top-level tests.

- No `t.Setenv`, `os.Setenv`, `t.Chdir`, `time.Sleep`, logger swap or
  package-level seam.
- `:memory:` appears 8 times in 6 files, each test its own database.
- Package-level variables are read-only tables: `component_anchor_test.go:21`
  and `:29`, `registry_graph_write_parked_test.go:13`, and in the package
  itself `crd_refresh.go:38` and `k8s_refresh.go:57`, none with a writer.

## A constraint worth knowing before starting step 3

**The suite cannot be run under `-race` in CI within default timeouts.**
`internal/api` takes **625s** under `-race` for a single run against 19.1s
without it — roughly 33x. A `-count=5 -race` attempt failed at 601s on both heavy
packages, which is Go's default 10-minute package timeout rather than a hang.
Verifying step 3 under `-race` therefore needs an explicit `-timeout`.

## History

Filed 2026-08-19 from the reverted build-cache attempt
(`docs/backlog/done/gate-build-cache.md`; joe-pm
`threads/revert-gate-build-cache.md`), which had diagnosed the gate as
compile-bound from a warm local run against a CI runner and added a cache that
was already there. Rewritten 2026-11-17 from the measurement above. The slug is
kept because `tests.yml` and the archived item cite this path; the title is what
changed.
