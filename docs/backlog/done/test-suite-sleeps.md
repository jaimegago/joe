# Test-suite `time.Sleep` calls — 14 across 8 files

Status: done
Priority: next

**Sleeps in tests are where flake comes from**, and every one of these sits in the
merge gate's critical path. A sleep is either long enough to be slow or short
enough to be flaky, and usually it drifts into being both.

## Where they are

Fourteen occurrences across eight files, found with
`grep -rn 'time.Sleep' --include='*_test.go' .`:

- `internal/api/tasks_test.go`
- `internal/auth/audit_login_test.go`
- `internal/client/client_http_test.go`
- `internal/coreagent/agent_test.go`
- `internal/coreagent/supplemental_coverage_test.go`
- `internal/skills/watcher_test.go`
- `internal/slack/server_test.go`
- `test/e2e/basic_test.go` — post-merge tier since the gate tier split, so lower
  priority than the seven above

## The work

Replace each with a deterministic wait. In rough order of preference:

1. **Synchronise properly** — a channel, a `sync.WaitGroup`, or an injected clock.
   A test that can be made to wait for the actual event should.
2. **Poll with a timeout** where the event is not directly observable — tight
   interval, generous ceiling. Fast when it passes, and it fails with a useful
   message rather than a mystery.
3. **Leave it and say why**, in a comment on the line, where neither is available.
   An acknowledged sleep is worth more than a silently reintroduced one.

`internal/skills/watcher_test.go` is likely the hardest — filesystem watchers are
genuinely asynchronous — and is worth doing last, once the pattern is settled on
the easy ones.

## Why this is worth doing

The gate is the only thing in joe that reads a change before it lands. Its value
is entirely in being trusted, and a suite with sleeps in it eventually produces a
red run that everyone assumes is flake. That is the failure mode: not a false
alarm, but a true alarm that gets ignored.

Filed from the gate tier split, 2026-08-18. joe-pm
`threads/gate-tier-split.md` carries the measurement.

## Closed — 2026-08-19

**The item's headline count was wrong, and the correction is the useful part.**
"14 across 8 files" was a raw `grep` count, not a defect count. Reading them:

**Already the target pattern — poll-with-timeout, no change made:**

- `internal/api/tasks_test.go:539` and `:594` — sleeps inside a loop with a 2s
  ceiling. This is what the other sites were being converted *to*.
- `internal/skills/watcher_test.go:175` — same shape, 1s ceiling.

**The sleep is the fixture or the semantics, not a wait — no change made:**

- `internal/client/client_http_test.go:184` — the sleep is inside the `httptest`
  handler, making a deliberately slow server so context cancellation has
  something to cancel.
- `internal/auth/audit_login_test.go:271` — 40ms against a 20ms
  `AuditDedupWindow`, asserting a second row is written after the window
  elapses. **It cannot flake**: load makes a sleep longer, never shorter, and
  only a shorter one would break the assertion. Fixing it properly needs a clock
  seam in `EdgeAuth`, which is a production change and not this item.

**Genuinely sleep-and-hope — fixed:**

- `internal/coreagent/agent_test.go:124`, `supplemental_coverage_test.go:255`,
  `:272`, `:296` — all four **removed outright**. The synchronisation already
  existed: `Start` sets `r.cancel` synchronously and `refreshLoop` does
  `defer close(r.doneCh)` (`refresh.go:127`, `:155`), so `Stop`'s `doneCh` wait
  and the tests' own `select` on `doneCh` are deterministic whether or not the
  goroutine has been scheduled. The sleeps were guarding something already
  guarded.
- `internal/slack/server_test.go:149` — replaced by waiting on the observable
  effect. `mockSlackPoster` gained an optional `posted` channel; the test waits
  for the dispatched goroutine to reach `PostMessage` and now **asserts the
  channel ID**, where before it asserted nothing. The channel also supplies a
  happens-before edge for the mock's fields that the sleep did not.
- `internal/slack/server_test.go:165` — **removed**, with the reason recorded in
  place: the event data is deliberately invalid so the goroutine returns at its
  type assertion, touching nothing observable. There is no effect to wait for,
  and that early return is already covered synchronously by
  `TestHandleEventsAPI_NotOK` in the same file.
- `internal/api/tasks_test.go:663` — the hard case, a **negative** assertion.
  Absence cannot be polled for, so `sentinelTitleStubLLM` gained a `titled`
  channel closed when the title prompt is seen. The test now waits for the code
  that would have written the title to have run, then asserts it did not. A 300ms
  guess became an edge.
- `test/e2e/basic_test.go:80` — removed; it paced five sequential requests apart
  for no reason, costing 500ms.
- `test/e2e/basic_test.go:105` — replaced with poll-until-the-endpoint-stops-
  answering, 5s ceiling. The fixed 500ms was both too long in the normal case and
  too short on a loaded runner, where it would fail as a phantom "server did not
  shut down".

**Net: 14 occurrences → 6.** Four are the poll-loop pattern or a deliberate
fixture; one is the poll loop this item introduced; one is the `AuditDedupWindow`
case above, which needs a production clock seam to retire and is left with its
reasoning written down.

Verified with `go test -race -count=1` over the three touched packages: all `ok`,
no races.
