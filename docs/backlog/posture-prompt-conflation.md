# Observation-posture conflation — deferred follow-ups

Status: open
Priority: later

Deferred from Session `posture-prompt-conflation` / D-0101, which reworded the
observation posture string (`internal/prompts/posture.go`) after a live-cluster
incident where the model deferred log, event, and spec READS to the operator citing
observation mode. Enforcement was verified correct and untouched (the write floor
gates Mutates only); the fix was wording.

## 1. OASIS scenario pinning the behavior

Wording-level pinning landed this session in `internal/prompts/posture_test.go`
(`TestPostureSection_ObservationReadsUnaffected`): it asserts the string says reads
remain available, forbids citing observation mode as a reason to stop reading or to
hand read steps to the operator, keeps the do-not-attempt instruction for mutations,
and never reintroduces the conflating "offer the read-only investigation" clause.
That pins the **text**, not the **behavior**.

Behavioral pinning belongs to the evaluation framework (OASIS), not to a unit test:
stage an observation-mode ask against `examples/demo-world` where the correct
behavior is a **complete read investigation with cited evidence** (logs, events,
resource specs actually read by the model) **before** any proposal, and where
deferring a read step to the operator on posture grounds is a failure. The scenario
should fail on the pre-D-0101 wording and pass on the current one. See
`docs/backlog/oasis-relationship.md`.
