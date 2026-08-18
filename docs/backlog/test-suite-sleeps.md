# Test-suite `time.Sleep` calls — 14 across 8 files

Status: open
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
