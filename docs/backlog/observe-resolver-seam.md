Observe-resolver seam — component resolution is reachable only through an HTTP request

Status: open
Priority: later

`resolveComponentForService` and `resolveK8sComponentForService`
(`internal/api/observe.go`) resolve a signal backend from a service name by
walking the refresher-derived graph edges — `metrics_in`, `logs_in`, `traces_in`,
`alerts_in`, `paged_via`, `managed_by`, `provisions`, `image_stored_in`. Both are
unexported and welded to the HTTP handler and its request-derived principal
context. The logic cannot be entered any other way.

## Why this is filed separately, and why it is `later`

This was carried inside `component-resolution-tool` as that item's enabling work:
extract a shared seam so the HTTP handler and a new agent-loop tool "cannot
drift". **That justification no longer holds.** The resolution tool was settled as
a *naming* hop — task phrase to ranked component candidates, with graph context
returned as evidence — and a naming hop does not consume this resolver. No second
caller is coming, so there is no live drift to prevent, and nothing is blocked on
this.

What survives is weaker and still real:

- Logic reachable only through an HTTP request is hard to exercise in isolation.
  There is no way to test resolution without constructing a request and a
  principal context around it.
- Two resolution paths live here — the generic one and the k8s type-walk — plus
  an alerts fallback chain. Paths of that shape diverge quietly once anything
  reuses them, and today nothing would notice.

If a second consumer ever appears, the drift argument becomes live again and this
turns from cleanup into prerequisite work. Filed at `later` on the judgement that
latent reusability is not worth displacing committed work, not on the judgement
that it is unimportant.

## Constraint on any extraction

RBAC-through-accessor is preserved on every caller. The principal context is
request-derived today, and that is precisely what a seam has to abstract without
loosening — a seam that makes resolution callable without a principal has
widened the disclosure surface rather than tidied it.

## Note on `component-resolution-tool`

That item still describes the seam extraction as part of its own planned shape.
It is stale in that respect and has not been rewritten. Read this file as the
current position on the seam.
