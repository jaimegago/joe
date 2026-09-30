Every registered component gets an anchor node — kubernetes, aws and azure are represented only by their discovered resources
Status: done — kubernetes, aws, azure and git each write one bare anchor typed `<type>_component`; pinned over every registered component type by `TestEveryRegisteredComponentTypeWritesOneAnchor`; thread `component-anchor-node`

Most refreshers open their desired set with one node that stands for the
component itself: `obs/<type>/<component-id>` typed `<type>_component`
([internal/coreagent/observability_refresh.go:32](../../internal/coreagent/observability_refresh.go#L32),
`obsNodeID` at `:329`), and the alerting, datastore, gitops, networking and
registry refreshers do the same under their own prefixes. Three do not:

| Refresher | What it writes | Anchor |
|-----------|----------------|--------|
| kubernetes ([k8s_refresh.go:57-152](../../internal/coreagent/k8s_refresh.go#L57)) | the eight core kinds plus CRD objects, every one carrying `component_id` | **none** |
| aws ([aws_refresh.go](../../internal/coreagent/aws_refresh.go)) | `vpc`, `ec2_instance`, `eks_cluster`, `rds_instance` | **none** |
| azure ([azure_refresh.go](../../internal/coreagent/azure_refresh.go)) | `vnet`, `vm`, `aks_cluster`, `sql_database` | **none** |

A kubernetes component is therefore present in the graph only as the set of rows
sharing its `component_id`. Nothing can point *at the cluster*: there is no node
for an edge to land on, and "which cluster is this in" is answered by a column
rather than by a relation. The `eks_cluster` and `aks_cluster` nodes are a
different component's view of the same thing, and nothing joins the two.

This is a consistency gap, not a correctness bug. Both resolvers that need the
owning kubernetes component already work without an anchor, because they read
`component_id` off the discovered nodes:

- `resolveK8sComponentForService`
  ([internal/api/observe.go:401-447](../../internal/api/observe.go#L401)) walks
  to depth two and looks each distinct `component_id` up in the component store
  (D-0116). With an anchor it would be a one-hop walk to a typed node.
- `ListComponentBindings` attributes an edge to a component through either
  endpoint's `component_id`, so `resolve_component` bindings for a cluster do
  appear — via whichever resource node carries the edge.

## What an anchor would give

- **A subject.** A backend refresher matching a scrape job or log label to a
  cluster-level thing (a kube-state-metrics job, an audit-log stream) has
  nowhere to attach today; the `<type>_component` convention gives it one.
- **One shape for every component type.** `falco-refresher` records the same
  gap for a type that has no refresher at all; `component-type-contract` (item
  three, placement in the graph committed at registration) presumes a node
  exists to place. Both are easier if "every refreshable component writes
  exactly one anchor" is already true.
- **A place to draw the cloud-to-cluster join.** `is_k8s_node` already joins an
  instance to a node by IP; an `eks_cluster` → kubernetes-anchor edge would join
  the two views of one cluster, and cannot be drawn until the anchor exists.

## Open work

Decide the ID and type. The observability convention is
`obs/<type>/<component-id>` + `<type>_component`; the kubernetes family prefix is
`k8s/<component-id>/…`, so `k8s/<component-id>` typed `kubernetes_component` is
the natural spelling, with `aws/<component-id>` and `azure/<component-id>` the
same way. Then:

1. Emit the anchor first in each of the three desired sets, through the ordinary
   delta reconcile — no new write path (D-0110 untouched).
2. Decide whether the anchor carries edges to its own top-level resources
   (`contains` → namespaces; `contains` → VPCs) or stays a bare subject. The
   observability anchors are bare; starting bare is the smaller change.
3. Pin the invariant: a test that refreshes a fixture component of every type
   with a refresher and asserts exactly one node typed `<type>_component` (or
   the chosen spelling) carries that `component_id`.

Untriaged. Found while reading the graph for the maintainer's 2026-09-11
deep-dive; recorded under the `GRAPH` milestone in the maintainer's ledger.

## Closed

Closed by joe-pm thread `component-anchor-node`, which records the pull request
and the merge commit. The body above is kept as the historical statement of the
problem; this section records what the fix covered, because it differs from the
body in four places.

**Delivered.** The kubernetes, aws, azure and git refreshers each emit one anchor
node first in their desired set, through the ordinary delta reconcile:
`k8s/<component-id>`, `aws/<component-id>`, `azure/<component-id>` and
`git/<component-id>`, typed `kubernetes_component`, `aws_component`,
`azure_component` and `git_component`. The four types are declared in
`internal/graph/nodetypes.go`. The anchors are bare: no edge joins one to its
component's resources (Open work item 2, decided bare).

**Pinned.** `TestEveryRegisteredComponentTypeWritesOneAnchor`
(`internal/coreagent/component_anchor_test.go`) walks
`store.AllowedComponentTypes()` — the registry, not the refreshers — and asserts
exactly one node typed `<type>_component` per component. A registered type with
neither a fixture nor a named exemption fails.
`TestUnregistrableCloudTypesWriteOneAnchor` covers aws and azure separately.

**Where it differs from the body.**

- **git was not in the table and is covered.** Its refresher wrote one `git_repo`
  node and no `git_component`. It now writes both.
- **aws and azure are UNREGISTRABLE** (`internal/store/constants.go`), which the
  body does not say. Their anchors are written only for rows stored before the
  trim, and the registry walk cannot reach them — hence the second test.
- **Three registered types are exempt by name**, not one: `falco`
  ([falco-refresher](../falco-refresher.md)), `github` and `gitlab`. None has a
  case in `refreshComponent`'s switch.
- **"Most refreshers … do the same" holds only for registered types.** The
  unregistrable `nginx-ingress` writes `nginx_component`, and the four registry
  types write `artifact_registry`; neither is `<type>_component`. Left as it is:
  the walk does not reach them, and nothing was changed.

**Not delivered.** `contains` edges from an anchor to its top-level resources,
and the `eks_cluster`/`aks_cluster` → kubernetes-anchor join.
`resolveK8sComponentForService` is unchanged and keeps its depth-two walk.

