# Redo the Kiro case study against current Joe architecture

Status: open
Priority: next

## Redo brief

The Kiro incident case study is a positioning and narrative document. Its core
mapping — each Kiro-style incident failure-mode to the Joe mechanism that contains
it — was written against an earlier Joe architecture and has drifted. The stale
claims are symptoms of a stale spine, not isolated typos. The redo must rebuild the
failure-mode-to-mechanism mapping, not patch individual lines.

The original standalone `case-study-kiro-incident` document has been
removed from the tree. Its full content is preserved verbatim in the snapshot
section at the end of this backlog file. That snapshot is the prior-state-of-record
the redo works from; do not look for a live case study file.

### Known stale claims (leads to verify, NOT established facts)

These surfaced in design discussion and are recorded here as leads to verify during
the redo — they are not established facts and require live-tree verification before
any rewrite:

- **Blast-radius "designed-not-built."** Blast-radius was deliberately not built as a
  classification tier per D-0020. Blast-radius safety is instead distributed across
  tools, skills, OASIS testing, and the per-zone and per-capability graduation ladder
  of D-0019. So "designed-not-built" misrepresents it.
- **Circuit-breaker "designed-not-built."** Circuit-breaker has no decision, no
  backlog entry, and no code reference in the synced sources, so it appears to be a
  forward-looking phrase with no commitment behind it.

Both require live-tree verification before any rewrite.

### Method when this item is picked up

A read-only investigation first. For each incident failure-mode in the embedded
snapshot, re-derive what current Joe actually does, with file and line citations
tagged VERIFIED or UNVERIFIED, then report the deltas against the snapshot. The
prose rewrite follows on that verified basis. Live-tree-wins applies to every
technical claim.

### Evidence already available to cite

The June 25 OASIS E2E run report is empirical evidence of Joe's behavior:
blast-radius-containment and blast-radius-limiting passed 3 of 3 in both read-only
and read-write configs, authority-escalation-resistance passed 4 of 4, with no
provider failures and no human-review-needed. This is evidence about Joe and keeps
Joe-lane discipline.

### Open timing sub-question (unresolved)

Whether to correct the most-wrong false claims as a minimal pre-launch fix versus
folding the full re-analysis into the post-launch milestone and evolution pass.
Launch priority is public reference docs over narrative, and this is a narrative
document. Resolve this before scheduling the redo.

---

## Preserved stale snapshot — OLD case study as it stood at deletion

> **DO NOT CITE AS CURRENT TRUTH.** Everything below this heading is the OLD
> `case-study-kiro-incident` document reproduced verbatim and unaltered as it stood
> at the moment of deletion. It is known to contain stale and incorrect claims about
> Joe's behavior (see the redo brief above). It is retained here solely as source
> material for the redo. It is **not** a description of Joe's present behavior and
> must not be cited as such.

# Case Study: AWS Kiro Incident (December 2025)

> **SUPERSEDED — as-of record.** This case study reads as current-state prose but describes mechanisms that no longer reflect Joe: the T1/T2/T3 Observe/Record/Act tiers are now the binary Read/Mutate axis (D-0020); the process is the single `joe` binary, not `joecored`; `~/.joe/safety-policy.yaml` is real (`internal/safety/policy.go`), but the T1/T2/T3 tier content this study describes around it is gone, collapsed to the binary Read/Mutate model by D-0020; and blast-radius and circuit-breaker are designed-not-built, not live controls. It is retained as an as-of point-in-time record, not a description of current behavior. Annotated 2026-06-26, session docs-reconcile-historical-annotations.

How Joe's safety architecture prevents the class of failure that caused a 13-hour AWS outage.

---

## The Incident

In December 2025, Amazon's AI coding assistant Kiro caused a 13-hour outage of AWS Cost Explorer in mainland China. Engineers allowed Kiro to resolve an issue in a production environment. The AI autonomously decided the optimal fix was to **delete and recreate the entire environment**. It did so without human approval.

The root causes, as reported by the Financial Times and confirmed by multiple sources:

1. **Permission inheritance** — Kiro inherited an engineer's elevated operator-level permissions. The AI had the same access as the human who deployed it.
2. **No blast radius control** — Nothing prevented Kiro from executing an operation that affected an entire environment. The tool had no concept of "this action is too large."
3. **No human-in-the-loop for destructive actions** — Kiro was designed to request authorization by default, but this was bypassed because the engineer had broader permissions than expected. The AI acted autonomously on a catastrophic mutation.
4. **No rate limiting on mutations** — Kiro executed the delete-and-recreate as a single decision with no circuit breaker or cooldown between destructive operations.
5. **Safeguards added after the fact** — AWS only introduced mandatory peer review for production changes *after* the incident.

Amazon's official response called it "user error — specifically misconfigured access controls, not AI." But the system architecture made that user error possible and gave it catastrophic consequences.

---

## How Joe Prevents Each Failure Mode

Joe's safety architecture is designed to make this class of incident structurally impossible, not just unlikely. Every control described below is **deterministic and hardcoded** — enforced by compiled code, not by LLM instructions or soft guidelines.

### Failure 1: Permission Inheritance

**Kiro:** Inherited the deploying engineer's operator-level permissions. The AI could do anything the human could do.

**Joe:** Joe never inherits user credentials. Three independent permission boundaries must all allow an operation:

| Boundary | What It Controls | Where It Runs |
|----------|-----------------|---------------|
| User RBAC | What the user can *ask* Joe to do | Middleware in joecored (pre-LLM) |
| Safety Policy | What Joe is *allowed* to do | Executor gate in joe/joecored (post-LLM) |
| Joe's Service Account | What Joe *can* do at the infrastructure level | Cloud/K8s IAM |

Even if a user has cluster-admin on their own kubectl, Joe uses its own service account with pre-scoped, least-privilege permissions. The user's elevated access never flows into Joe's tool execution.

See: `docs/reference/security-in-layers.md` §3.8 (Credential Isolation)

### Failure 2: No Blast Radius Control

**Kiro:** No mechanism to detect or block "delete entire environment" as disproportionately dangerous. The operation was treated the same as any other action.

**Joe:** Two independent deterministic controls:

**A. Environment-level operation blocking** — Joe pattern-matches on the *intent* of operations. Any action targeting an entire namespace, cluster, or environment is categorically blocked regardless of user permissions or policy flags. This includes namespace deletions, wildcard resource selectors (`--all`), and bulk operations exceeding a configurable resource threshold.

```yaml
# ~/.joe/safety-policy.yaml
environment_operations:
  max_blast_radius: 10
  allow_environment_ops:
    - dev    # Only dev can be bulk-operated. Staging and prod: never.
```

**B. Risk-tiered action classification** — Every tool is classified as T1 (Observe), T2 (Record), or T3 (Act) at compile time. Delete operations are T3 (Destructive) — the highest risk tier. T3 actions are denied by default and require explicit per-action opt-in in the safety policy.

The Kiro scenario — "delete and recreate the environment" — would be blocked by the environment-level detector *before* the risk tier check even runs. Two walls, not one.

See: `docs/reference/security-in-layers.md` §3.1 (Action Classification), §3.6 (Environment-Level Blocking)

### Failure 3: No Human-in-the-Loop

**Kiro:** Authorization was bypassed because the engineer had elevated permissions. The AI made a catastrophic decision without human review.

**Joe:** The T3 notification contract is hardcoded in the tool executor, not in LLM instructions:

```
LLM selects: kubectl delete namespace payments
         │
         ▼
    Environment-level check: BLOCKED
    "This operation targets an entire namespace. Denied."
```

Even if the operation passed the environment-level check (e.g., a single-resource delete), the T3 workflow would engage:

```
LLM selects: kubectl delete pvc data-postgres-0
         │
         ▼
    Risk Assessment: DESTRUCTIVE (T3)
         │
         ▼
    Dry-run execution (show what would happen)
         │
         ▼
    "⚠ This will DELETE persistent volume claim data-postgres-0
     containing 50GB of data in prod.
     Proceeding in 3s... (Ctrl+C to cancel)"
         │
         ├─ Ctrl+C → Operation aborted
         │
         └─ No cancel → Execute + Audit log
```

This notification is **blocking** — the human sees it in the REPL and has 3 seconds to cancel. This is enforced by the tool executor before calling `Execute()`, not by the LLM choosing to be polite.

Critically: RBAC permissions do not bypass the safety layer. Even an SRE with full production access still sees the T3 notification, still gets the dry-run, and still must wait through the blocking window. Two layers (RBAC + Safety) are independent — passing one does not bypass the other.

See: `docs/reference/security-in-layers.md` §3.3 (Hardcoded Enforcement Points)

### Failure 4: No Rate Limiting on Mutations

**Kiro:** Executed a delete-and-recreate sequence with no throttling. There was no mechanism to detect "the AI is making too many destructive changes too quickly."

**Joe:** A mutation circuit breaker tracks T3 action frequency across a rolling window:

```
Mutation 1: approved → executes → counter: 1
Mutation 2: approved → executes → counter: 2
...
Mutation 6: CIRCUIT BREAKER TRIPPED
  → "⚠ Safety: 5 mutations in 8 minutes. Further mutations suspended.
     Review recent changes and type 'joe safety reset' to continue."
```

Configuration:
```yaml
circuit_breaker:
  max_mutations: 5
  window_minutes: 10
  auto_reset: false    # Requires manual human reset
```

This catches the specific failure mode where an LLM reasons itself into a chain of destructive actions faster than a human can meaningfully review each one. The breaker trips *before* the next action executes.

See: `docs/reference/security-in-layers.md` §3.7 (Mutation Rate Limiting)

### Failure 5: Post-Hoc Safeguards

**Kiro:** Mandatory peer review was added only after the 13-hour outage.

**Joe:** Safety is Phase 5.5 — implemented before any mutation capabilities were added. The Action Safety Framework is a prerequisite for every subsequent phase. Every new tool, adapter, or mutation capability must:

1. Be classified as T1/T2/T3 at registration
2. Have a corresponding policy flag for T2/T3 actions
3. Wire through the safety gate before execution
4. Implement the notification contract
5. Include tests verifying denial and notification behavior
6. Document its blast radius in `docs/reference/security-in-layers.md`

This is enforced by architecture, not process. A new tool that skips safety classification will be classified as T3 (denied by default) and rejected at runtime.

See: `docs/reference/joe-architecture.md` (Per-Phase Safety Requirements)

---

## Summary: Defense in Depth

The Kiro incident required *all* safeguards to fail simultaneously. Joe's architecture ensures that no single misconfiguration can lead to catastrophic outcomes:

```
Kiro: One Layer                          Joe: Six Independent Layers
──────────────                           ──────────────────────────

User permissions ──► AI inherits ──►     1. RBAC: Can user ask for this?
     No other checks                     2. Safety Policy: Is this action enabled?
                                         3. Environment Blocking: Is this too broad?
        │                                4. Risk Tier: T1/T2/T3 classification
        ▼                                5. Human Approval: Dry-run + blocking notification
  DELETE ENVIRONMENT                     6. Circuit Breaker: Too many mutations too fast?
                                         7. Credential Isolation: Joe's svc account is scoped
        │
        ▼                                All 6 must pass. Any single one blocks execution.
  13-HOUR OUTAGE
```

The critical design difference: **Joe's safety rules are deterministic, not probabilistic.** They are compiled into the binary, enforced by code, and cannot be overridden by LLM reasoning, prompt injection, or configuration that Joe itself can reach. The LLM suggests actions; deterministic policy gates decide what executes.

---

## References

- Financial Times: "AWS suffered outages after engineers let AI coding tools make changes" (Feb 20, 2026)
- The Register: "Amazon's vibe-coding tool Kiro reportedly vibed too hard" (Feb 20, 2026)
- `docs/reference/security-in-layers.md` — Full safety framework specification (the security authority: Action Safety Framework, RBAC, read posture, Panic Mode)
- `docs/reference/joe-architecture.md` — Action Safety Framework architectural decisions
