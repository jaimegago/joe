Break-tests pinning tool action-class — 58 Read rows are unpinned
Status: open
Priority: next

Rewritten 2026-08-14 (thread `repo-search-tool-residue`). **The previous body was
void**: both gaps it named — `detect_doc_drift` and the `publish_doc_update`
family — carry no rows in `internal/safety/tier.go` at all. They were deleted with
the knowledge store under D-0113, and the surviving mentions of
`publish_doc_update_git` in the tree are test fixtures that use the name
*precisely because it is unclassified*, to exercise the unknown-tool default. A
session acting on the old body would have added assertions for tools that do not
exist and come away believing the surface was covered.

The gap is real. It is 29 times larger than the old body described, and it has a
different shape.

## The principle, unchanged

A tool's action class is an invariant the safety gate depends on, so an unpinned
class is a silent-regression hazard: a refactor, or a careless edit to the
registry, could flip one with the full suite still green. Structural invariants
here are break-tested, not inspected.

## The measurement

`toolRegistry` in `internal/safety/tier.go` holds 89 rows — 86 `ActionRead`, 3
`ActionMutate`.

- **The 3 Mutate rows are pinned twice.** By name in
  `TestClassifyTool_KnownTools`, and by `TestRegisteredMutatesAreUngrantable`,
  which derives the Mutate set from the registry itself and so covers any row
  added later.
- **58 of the 86 Read rows have their class asserted by no test anywhere in the
  tree.** The five files that assert a class against a literal tool name are
  `internal/safety/tier_test.go`, `tier_websearch_test.go`,
  `tier_reposearch_test.go`, `tier_mutate_ungrantable_test.go`, and
  `internal/coreagent/register_component_governance_test.go`. Between them they
  name 28 of the 86.

Re-derive rather than trusting the number: extract the map keys and their classes
from `tier.go` and check each name against those five files. It moves every time a
tool is added.

**The two registry-derived tests do not close this.**
`TestActionClass_IsBinary` asserts every row's class is one of the two;
`TestRegisteredMutatesAreUngrantable` asserts Mutates are denied. A Read silently
flipped to Mutate satisfies both and fails neither — it is still binary, and it is
still a denied Mutate. The Mutate→Read direction is caught by the named table; the
Read→Mutate direction, which is the one that silently disables a tool, is not.

The unpinned 58, for reference at the time of writing: the five shared diagnostics
(`tcp_connect`, `port_scan`, `dns_lookup`, `http_request`, `trace_route`); the
twelve datastore tools; the ten networking and ingress tools; the two Falco tools;
the four proprietary observability tools; the seven K8s CRD tools; the twelve
GitOps/CD/IaC tools; and the six read-only PR/MR tools.

## The fix shape is an open choice, deliberately not made here

The old body prescribed "add the missing assertions". That does not scale to 58
and leaves the 59th unpinned the next time a tool is added — the failure this item
exists to prevent, one level up. Options, none ratified:

- **Extend the named table to all 86.** Explicit and greppable. Restates the
  registry in a second place, and a new tool is unpinned until someone remembers.
- **A checked-in golden set derived from the registry**, asserted equal to the
  live Read set. Catches both a silent flip and a newly added row, since either
  makes the sets differ and forces a deliberate edit. Weakest on readability: the
  diff says a set changed, not why it may.
- **A rule that every registered tool carries an explicit row**, asserted by
  walking `NewCoreRegistry`'s advertised names against `toolRegistry`. A different
  property — it catches reliance on the unknown-tool default rather than a class
  flip — and arguably worth having alongside either of the above.

Whichever is chosen, it touches only test files in `internal/safety`.

## Adjacent, and not to be merged into this

Six of the 58 — `datadog_query`, `splunk_query`, `dynatrace_query`,
`newrelic_query`, `github_list_prs`, `gitlab_list_mrs` — have **no non-test
reference anywhere outside `tier.go`**: no constructor registers them. They are
classification rows for tools that do not exist, which is why no registry-derived
test could reach them either. That belongs to
[`advertised-dead-tools`](advertised-dead-tools.md), and the two items should stay
separate — deciding what to do with a dead row is a different question from
pinning a live one, and folding them together would let either block the other.
