# IaC declared-to-live bridge upgrade — identifier-derived edges, git_repo anchoring, and flux

Status: open
Priority: later

Filed from the session `iac-graph-ingestion`, whose Phase 1 recon established the as-built
picture below. That session made one scoped change (the three onboarding-era graph-write
tools parked out of the agent:core registry) and deferred everything else here. See D-0110
for the deterministic-graph invariant that constrains this work.

## What ships today

The session opened intending to design declared-to-live bridging from scratch. Recon
falsified that premise: bridges already ship, just not anchored to any repository.

- **Argo CD and Helm bridges ship** as `managed_by` edges from k8s workloads to
  `argocd_app` / `helm_release` nodes, built by `buildManagedByEdges`
  (`internal/coreagent/gitops_refresh.go`). The match is **name plus namespace**, so the
  edges carry `Confidence: Inferred` with `Source: gitops_name_match`. A same-named
  workload in scope can produce a wrong edge.
- **Terraform ships** `provisions` edges from `terraform_resource` nodes to cloud nodes
  (ec2/rds/eks/azure), built by `buildProvidesEdges` on the same name-match basis
  (`Source: terraform_name_match`). Its adapter parses a **local `terraform.tfstate` JSON
  file**, not a repository, and redacts sensitive attributes
  (`internal/adapters/iac/terraform/terraform.go`).
- **No edge is incident on any `git_repo` node.** The git refresher builds exactly one node
  from HEAD commit identity (hash, date, author) and emits **zero edges**
  (`internal/coreagent/git_refresh.go`) — the state D-0042 left it in when it deleted the
  `.joe/` ingestion path. The declared side is therefore unanchored to its repo.
- **`argocd_app` carries `repo_url` as metadata only** — a string on the node, not an edge.
- **No flux refresher exists.** Flux is tools-only (`internal/tools/core/flux_tools.go`),
  reading through the core client; it writes nothing to the graph.

## The work

The gap is not "parse IaC" — it is that the bridges that exist are name guesses, and the
declared side has no repository anchor.

1. **Upgrade name-match bridges to identifier-derived edges.** Extract the repo, path, and
   revision the CD system already resolved (Argo CD app spec source, Helm release
   annotations, flux tracking labels) instead of matching on name. Promote `Confidence` to
   `Explicit` where the identifier confirms the relationship. CD systems come first
   precisely because they have **already resolved desired → live**; Joe reads that
   resolution rather than recomputing it.
2. **Emit `git_repo`-incident edges** so declared infrastructure anchors to its repository —
   the bridge that answers *what defines this resource* and *does live match declared*. Git
   components anchor declared infrastructure the way live components anchor observed
   infrastructure.
3. **Add a flux refresher.** It is the one CD system with graph-visible tooling but no
   refresher at all.
4. **Terraform**: state parsing already carries real resource identifiers joinable to cloud
   refresher nodes; the work is joining on those identifiers rather than on names, and
   deciding whether state is sourced from anywhere other than a local path.
5. **Raw manifest and DSL parsing (puppet, ansible, kustomize) is last or never.** It is the
   only class requiring Joe to compute desired state itself, rather than read a resolution
   some system already performed.

## Constraints

- **Graph ingestion is deterministic parsing only**, per D-0110. Nodes and edges come solely
  from adapter and refresher code through the delta-reconcile seam. LLM-inferred
  understanding of a repository — intent, conventions, architecture — is **never** written to
  the graph. **It now has nowhere to go.** It was destined for the knowledge store's
  **derived** tier, but D-0113 deleted the knowledge store outright, so that destination no
  longer exists and `knowledge-store-maturation` (which was to produce its governed write
  path) is archived as superseded. The D-0110 bar on the graph is unaffected and still
  absolute. This is the one real forward dependency the prune left open: **whatever consumes
  IaC-derived inference must decide its home before that work starts**, and "put it in the
  graph" is not available. Designing a knowledge-store v2 to receive it is one option; so is
  a different structure entirely. That decision is unmade — do not resolve it by widening
  the graph.
- `Confidence` on a graph edge is a **heuristic-strength marker on a deterministic
  derivation**, not an authority tier — `Inferred` means "name-matched", not "LLM-guessed".
  Do not reintroduce it as a trust axis.

## Refresh semantics

**Full re-derivation at HEAD plus delta-reconcile is the correctness mechanism.** Parse the
whole tree at the current revision, build the desired node/edge set, and let
`BuildGraphDelta` / `ApplyGraphDelta` reconcile — the same seam every refresher uses, which
is what removes structure that no longer exists.

HEAD-hash comparison is a **skip optimization only**: unchanged hash means skip the tick, not
diff it. **No incremental diff parsing** — deriving graph state from a diff means the graph's
correctness depends on every prior tick having been correct, which is exactly the property
delta-reconcile exists to avoid needing.

## Out of scope by design

**Format expertise is a skills concern** — how puppet works, what an ansible role means, HCL
semantics. That is skill content, not graph content.

**Repo conventions are curated knowledge** — "we always tag prod resources this way" is a
knowledge-store entry a human owns, not an edge.

Neither enters the graph.

## Designated consumer: change-impact analysis

Change-impact analysis (D-0140, `change-impact-analysis`) is a designated consumer of this
work. It needs two edge families from here:

- **identifier-derived `repo`-to-`repo` and `repo`-to-infra edges**, so cross-repo
  co-requisite detection stops depending on per-repo `git log` co-change history, which
  cannot cross a repository boundary;
- a **version-provenance mapping from deployed workloads to commits**, so an assessment can
  ground against what is actually running rather than against HEAD.

Both must remain **deterministic derivations** per D-0110 — this consumer does not license
an inference path into the graph.
