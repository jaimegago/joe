Node-type vocabulary re-encoded by consumers — the gitops provides-matcher's phantom arms and the test fixtures that green them
Status: open
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

## Not in scope here

The `is_k8s_node` **relation** constant (`internal/graph/relations.go:11`) is unrelated
and correct — it is an edge relation name, not a node type, and the aws/azure refreshers
write it deliberately. Do not "clean" it while sweeping the phantom node types.
