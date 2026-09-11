# The forced-synthesis path can return an empty `answer`: success is judged before the markers are stripped
Status: open

When the loop reaches its iteration cap, `synthesizeFinalAnswer` makes one
tool-less call and counts the synthesis as successful if the raw content is
non-empty
([internal/agentloop/agent.go:702](../../internal/agentloop/agent.go#L702)). The
turn-kind marker and the declaration block are only stripped **after** that
decision, back in `Run`
([agent.go:558-576](../../internal/agentloop/agent.go#L558)). So a synthesis made
only of `ROOT-CAUSE:`, `DISCARDED:` and `TURN-KIND:` lines passes as a success,
loses every line to the strip, and goes out as a completed `answer` whose
`final_answer` is `""`. It carries `stop_reason: max_iterations`, and the cap-hit
audit row records `synthesized: true`.

That breaks two rules at once:

- **Loop budget exhaustion (D-0096–D-0100).** An empty synthesis is explicitly a
  failure that falls through to `ErrMaxIterations`
  (`TestMaxIterations_SynthesisFailureFallsThrough`). The rule was written before
  D-0158 and D-0159 put joe's own plumbing into the content, and the check was
  never moved behind the strip.
- **D-0160.** joe does not return an `answer` terminal turn whose operator-facing
  prose is empty. That gate sits on the loop's no-tool-calls branch, and the loop
  has already exited when synthesis runs, so there is nothing to re-enter.
  Marker-only synthesis is the one path that still returns this shape.

Unobserved in any run. Found by reading the tree while implementing D-0160
(joe-pm `threads/empty-answer-gate.md`).

## Likely remedy, not decided here

Judge synthesis success on the stripped content: strip first, and treat an empty
result as the synthesis failure it already is under D-0096–D-0100. It returns
`ErrMaxIterations`, and the audit row says `synthesized: false`. That extends an
existing rule and adds no new mechanism.

**To settle:** on that path, is a declared conclusion recorded on the session or
discarded? The run returns an error, not a terminal turn, and under D-0158 an
errored run carries no turn kind.

## Related

- [`loop-budget-exhaustion`](loop-budget-exhaustion.md) — the synthesis seam's
  other follow-ups. This item is not one of its three.
