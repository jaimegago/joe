# `SplitTurnKind` strips only the last `TURN-KIND` line, so an earlier copy reaches the operator
Status: open

`SplitTurnKind` looks for the marker on every line and lets the last match win
([internal/agentloop/turnkind.go:94](../../internal/agentloop/turnkind.go#L94)).
It then removes **only that one line**
([turnkind.go:99](../../internal/agentloop/turnkind.go#L99)). A model that writes
`TURN-KIND: answer` twice, for example once mid-reply and again at the end as
instructed, has the first copy left in the prose. D-0158 promised the marker
reaches none of the probe's replay, the session history, the observer, or the
operator. A second copy reaches all four.

`SplitConclusion` does not have this defect: it strips every `ROOT-CAUSE:` and
`DISCARDED:` line whatever their number
([conclusion.go:106](../../internal/agentloop/conclusion.go#L106)). The two
parsers share a matching rule, but only one of them removes every line it matches.

**It also interacts with D-0160.** A turn whose only "prose" is a leftover marker
line is not empty by that decision's definition, so the empty-answer gate lets
it through. The operator receives `TURN-KIND: answer` and nothing else. That is
correct under the gate's own terms: it keys on what reaches the operator. So the
fix belongs here and not in the gate.

Unobserved in any run. Found by reading the tree while implementing D-0160
(joe-pm `threads/empty-answer-gate.md`).

## Likely remedy, not decided here

Strip every line that parses as a turn-kind declaration, and keep the rule that
the last line decides the value. The declared kind stays the same and only the
leak closes.

**To settle:** what happens to a marker whose value falls outside the vocabulary
(`TURN-KIND: clarification`). `TestSplitTurnKind` currently pins that line as
kept in the prose, on the grounds that an unrecognised value is not a
declaration. Stripping it too would treat it as plumbing even though it declared
nothing. Keeping it means a malformed marker still reaches the operator.
