Node-type vocabulary re-encoded by consumers — the gitops provides-matcher's phantom arms and the test fixtures that green them
Status: done — node-type vocabulary single-sourced in `internal/graph/nodetypes.go`, every Go reader and spec table referencing it, pinned by `TestNodeTypeVocabulary`; the Azure provides arms now match what azure writes; thread `node-type-vocabulary`
Priority: next

D-0116 fixed one consumer that bound to a node-type vocabulary no writer emits
(`resolveK8sComponentForService` matched a `k8s_` prefix; the kubernetes refresher
writes unprefixed types). The fix was scoped to that resolver. **The same bug class
is present, verified, in a second consumer** — the gitops refresher's
provides-matcher — and the test fixtures that exercise it are what keep it green.

This is **code-lane work**. It was found while verifying D-0116 and deliberately
left out of that session's scope.

## Gap 1 — half the gitops provides-matcher's arms match a vocabulary no writer emits

`buildProvidesEdges` (`internal/coreagent/gitops_refresh.go:282-288`) filters graph
nodes to "cloud-tier node types" with a switch. Four of its eight arms are phantoms —
checked against what the aws/azure/k8s refreshers actually write:

| Arm | Writer reality | Status |
|-----|----------------|--------|
| `ec2_instance` | `aws_refresh.go:82` writes it | live |
| `eks_cluster` | `aws_refresh.go:131` writes it | live |
| `rds_instance` | `aws_refresh.go:181` writes it | live |
| `node` | `k8s_refresh.go:65` nodeSpecs writes it | live |
| `azure_vm` | azure writes **`vm`** (`azure_refresh.go:69`) | **dead** |
| `azure_aks` | azure writes **`aks_cluster`** (`azure_refresh.go:110`) | **dead** |
| `azure_sql` | azure writes **`sql_database`** (`azure_refresh.go:151`) | **dead** |
| `k8s_node` | k8s writes **`node`** (already matched by the live arm) | **dead** |

Consequence: **a terraform resource can never produce a `provisions` edge to any
Azure resource.** The Azure half of the declared-to-live bridge is silently inert —
the aws and k8s arms work, so the feature looks alive. The `k8s_node` arm is
harmless (the adjacent `node` arm covers the real type) but is the same phantom.

Unlike D-0116's resolver, there is no component-type substitute to bind against
here: the matcher is genuinely selecting node *kinds*, not owners. The fix is
therefore not "bind to the component row" but **make the writer's vocabulary a
shared, single-sourced fact** — the deeper corollary D-0116 names. Options to weigh:
export per-refresher node-type constants the matcher imports (so a rename breaks the
build), or a break-test asserting every arm of this switch is a type some refresher
actually writes (so a phantom fails the suite). The latter generalizes: it would have
caught D-0116's `k8s_` predicate too.

## Gap 2 — test fixtures invent `k8s_node`, which is what keeps the phantom green

Four coreagent test files stage nodes typed `k8s_node` — a type no production writer
emits:

- `internal/coreagent/error_branch_test.go:299`
- `internal/coreagent/edge_coverage_test.go:61`, `:356`
- `internal/coreagent/registry_refresh_test.go:362` ("Add a k8s_node — should NOT produce an image_stored_in edge")
- `internal/coreagent/supplemental_coverage_test.go:403` ("k8s_node type — should be skipped")

These fixtures are why the phantom survives: a test that stages `k8s_node` and asserts
the matcher's behaviour is **asserting against a graph shape that cannot occur**, so it
passes whether or not the production arm is reachable. Note the two named above assert
*negative* behaviour (should be skipped / should NOT produce an edge) — they pass
trivially, since a type nothing writes is skipped by definition.

Cleaning the fixtures to the real vocabulary (`node`) is not cosmetic: it is what makes
the tests capable of failing. Do Gap 1 and Gap 2 together — fixing the fixtures without
fixing the switch will surface the dead arms as red tests, which is the point.

## Gap 3 — the same vocabulary is re-encoded at twelve more read sites, one with the same phantom

Found 2026-09-11 while reading the graph end to end. Every site below compares
`node.Type` against a literal that only agrees with the writer by convention.
Writers are fixed literals from spec tables (`k8sRefreshResources`,
`crdRefreshSpecs`), so the set is static per build; what nothing pins is
**writer/reader agreement**.

| Read site | Literals |
|-----------|----------|
| [internal/api/observe.go:48](../../internal/api/observe.go#L48), `:412` | prefers `service` as the subject |
| [internal/coreagent/observability_refresh.go:99](../../internal/coreagent/observability_refresh.go#L99), `:153`, `:223`, `:293`, `:412`, `:441` | attaches `metrics_in` / `logs_in` / `traces_in` only to `service`, `deployment` |
| [internal/coreagent/networking_refresh.go:152](../../internal/coreagent/networking_refresh.go#L152), `:193` | `service`, `deployment` |
| [internal/coreagent/registry_refresh.go:205](../../internal/coreagent/registry_refresh.go#L205) | `deployment`, `service` |
| [internal/coreagent/gitops_refresh.go:237](../../internal/coreagent/gitops_refresh.go#L237) | `deployment`, `statefulset`, `daemonset`, `service` |
| [internal/coreagent/aws_refresh.go:224](../../internal/coreagent/aws_refresh.go#L224), [azure_refresh.go:193](../../internal/coreagent/azure_refresh.go#L193) | queries `type:node` |
| [internal/coreagent/crd_refresh.go:45-85](../../internal/coreagent/crd_refresh.go#L45) | `TargetTypes` per CRD spec |

The last row carries **a second `azure_vm` phantom**: the Crossplane spec at
`crd_refresh.go:85` lists `ec2_instance`, `rds_instance`, `azure_vm`, `node` as
edge targets, and azure writes `vm` (`azure_refresh.go:69`). A Crossplane
managed resource can therefore never produce a `provisions` edge to an Azure VM
— the same dead arm as Gap 1, in a table instead of a switch.

This does not change the decision Gap 1 poses; it widens what the decision
covers. Whichever option lands — exported per-writer constants, or a break-test
asserting every read-site literal is a type some refresher writes — should be
applied to these sites in the same pass, and the break-test form is the one that
also catches a `TargetTypes` entry. The `<type>_component` anchor literals
(`observability_refresh.go:32`, computed from the component type) and the
relation constants in `internal/graph/relations.go` are the precedent for the
constants form; `edge-type-literal-consolidation` is the same decision for the
relation column.

## Not in scope here

The `is_k8s_node` **relation** constant (`internal/graph/relations.go:11`) is unrelated
and correct — it is an edge relation name, not a node type, and the aws/azure refreshers
write it deliberately. Do not "clean" it while sweeping the phantom node types.

## Closed

Closed by joe-pm thread `node-type-vocabulary`, which records the pull request
and the merge commit. The body above is kept as the historical statement of the
problem; this section records what the fix covered, because it differs from the
body in three places.

**Delivered.** Both options Gap 1 weighed, ratified together. Every node type a
refresher writes is a `graph.NodeType*` constant in `internal/graph/nodetypes.go`,
and every writer and reader references those constants, including the
`buildProvidesEdges` switch and every CRD spec's `TargetTypes`.
`TestNodeTypeVocabulary` (`internal/graph/nodetypes_guard_test.go`) type-checks
every production package that imports `internal/graph`. It fails when a string
literal stands where a node type belongs, and when a constant is written by no
refresher. So a phantom arm cannot be represented: a literal fails the first
check, and an unwritten constant fails the second.

**Behaviour change.** The four dead provides arms are gone. `vm`, `aks_cluster`
and `sql_database` now match in their place, so **a terraform resource can now
produce a `provisions` edge to Azure resources**, and a Crossplane resource can
reach an Azure `vm`. `TestBuildProvidesEdges_ReachesAzureTypes` and
`TestCrossplaneTargetsAzureVM` pin this.

**Where the body was wrong about the tree:**

- **Gap 2's causal claim does not hold.** None of the `k8s_node` fixtures
  exercises the provides-matcher. Every one is a negative case for a different
  matcher, such as datadog, registry, networking or alerting, asserting that a
  non-service node is skipped. Switching them to `node` left the suite green,
  both before and after the switch was fixed. They were cleaned anyway. The
  provides phantom stayed green because nothing tested the Azure arms
  positively, not because of those fixtures.
- **There were more fixtures than listed.** There were eleven `k8s_node`
  fixtures in six files. The body names five in four files.
- **There were more read sites than listed.** Gap 3's table omits
  `alerting_refresh.go` (two sites), `datastore_refresh.go` (two), and the
  `k8s_refresh.go` type switches and namespace checks. All of them are covered.

**Deliberately out of the set:**

- The computed observability anchor, `source.Type + "_component"`. Its value is
  a function of the component-type registry, not a literal any writer declares,
  and no reader compares against it. Anchor naming is
  `component-anchor-node`'s question.
- The parked LLM graph-write tool (D-0110), whose type comes from tool arguments.

**Still open elsewhere:** relations, which are `edge-type-literal-consolidation`.
The UI's own node-type strings are not covered by a Go guard.
