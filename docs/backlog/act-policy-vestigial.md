# ActPolicy opt-in seam: vestigial after knowledge prune

Status: open
Priority: later
Blocked-by: full-mode-rbac-track

**No registered tool reaches the `IsT3Allowed` allow branch.** After session
`knowledge-store-prune` (D-0113), every `ActionMutate` row in `internal/safety/tier.go`
carries a `PolicyKey` that `IsT3Allowed` (`internal/safety/policy.go`) has no case for —
`github_comment`, `gitlab_comment`, and `github_request_changes` name policy keys with no
corresponding `ActPolicy` field, so they fall to the `default: return false` deny. The
per-action opt-in described by the Action Safety Framework therefore cannot currently be
granted to anything: every registered Mutate is denied regardless of policy configuration.

`publish_doc_update_git` under `git_push` was the last tool whose `PolicyKey` resolved to a
live `ActPolicy` field. It died with the doc-publish arm. The seam itself
(`ActPolicy`, `ActionToggle`, `IsT3Allowed`, `DefaultPolicy`) was deliberately left
structurally intact by that session rather than deleted — the prune's scope was the
knowledge store, and restructuring the safety policy shape is a separate decision.

## Coverage deleted in the prune, to be reconstituted

These exercised the allow branch through `publish_doc_update_git` and had no replacement
fixture once it was gone. Each deletion is commented in place at its former site:

- `TestCheckAccess_MutateEnabled` (`internal/safety/tier_test.go`) — `CheckAccess` returning
  nil for a policy-enabled Mutate.
- `TestExecutor_SafetyGate_T3_AllowedByPolicy` (`internal/tools/executor_test.go`) — a Mutate
  passing the gate through to execution.
- `TestExecutor_Notifier_T3_CalledBeforeAndAfter` and
  `TestExecutor_Notifier_T3_CancelledDuringBefore` (same file) — the executor fires
  `NotifyBefore`/`NotifyAfter` only for `ActionMutate` and only **after** `CheckAccess`
  passes, so with no policy-allowed Mutate the notifier path cannot be driven from a real
  tool name at all. The notifier implementation is untouched; only its unreachable coverage
  went.

`IsT3Allowed`'s own true branch remains covered directly in `internal/safety/policy_test.go`
(`TestIsT3Allowed`), which sets `Act.GitPush.Enabled` and asserts the lookup — that is
policy-level and does not depend on a registered tool. What is uncovered is the path from a
tool name through `ClassifyTool` → `CheckAccess` → execution.

When full mode ships a tool with a real opt-in, restore these four as the first consumer's
tests.

## The pre-existing orphans belong to the same decision

`k8s_write`, `pagerduty_ack`, and `alertmanager_silence` have `ActPolicy` fields and
`IsT3Allowed` cases but **no classifier row has ever named them** as a `PolicyKey` — they
were orphaned before this prune, not by it. So `ActPolicy` now has four fields
(those three plus `git_push`) and zero consumers. Cleanup-or-revive is one decision across
all four: either the seam gets its first real opt-in tool in full mode and the field set is
rebuilt around it, or the whole `ActPolicy`/`IsT3Allowed` mechanism follows the D-0074 rule
and goes. Do not resolve half of it.

The `Record.OnboardingFacts` policy field (`internal/safety/policy.go`) is now doubly vestigial: session `onboarding-feature-removal` (D-0118) deleted the onboarding-facts subsystem it is named for, so it neither gates a live tool nor names a subsystem that still exists — it rides this same cleanup-or-revive decision.
