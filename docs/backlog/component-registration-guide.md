# Component registration guide — UI-driven public how-to
Status: in-progress

The first UI-driven registration how-to shipped this session (D-0059):
`docs/public/guides/register-kubernetes.md` covers **Kubernetes only**, with
Quickstart now including a Kubernetes register-and-promote step. The thread stays
in-progress because the following work remains.

## Deferred work

### Per-type registration how-tos (sibling Guides pages)

A Guides page per remaining documentable type (or grouped sibling pages), each
walking the real web-UI affordances for that type — register → assign zone →
promote → activate — the way `register-kubernetes.md` does. They split by
activation path (D-0056):

- **Runtime-registerable** static-credential types whose happy path ends at a UI
  **Test Connection** click, like Kubernetes does: `prometheus`, `mimir`, `loki`,
  `tempo`, `jaeger`, `alertmanager`, `pagerduty`, `grafana`, `argocd`, `falco`.
  These differ from Kubernetes only in the promotion form — a static env-var
  reference (`JOE_<SEGMENT>_<LABEL>`) instead of a kubeconfig locator.
- **Credential-less** types that register and test live with nothing to promote:
  `terraform`, `envoy`.
- **Boot-config-only** types (`github`, `gitlab`, `splunk`, `dynatrace`,
  `newrelic`): these register and promote at runtime, but the UI Test Connection
  reports a misleading "no connection to test" and registers nothing — they come
  live **only at the next daemon restart**. Their how-to must make the restart
  step explicit and must **not** present the Test click as the activation step.

Structure these so the Kubernetes page does not need reworking — add them as
sibling pages and extend the Guides index list.

### Static-provider promotion nuance

The static promotion form has behaviour the Kubernetes page did not need to cover:
a live **candidate picker** of matching env vars currently set in Joe's
environment, plus a compose-a-label fallback that warns when the composed
`JOE_<SEGMENT>_<LABEL>` is not currently set. The static how-tos should document
this picker/compose affordance.

### Screenshots

`register-kubernetes.md` carries five clearly-marked image placeholders under
`images/guides/register-kubernetes/` (components page, register dialog, zone
assign, promote kubeconfig form, successful test). These must be captured against
a running binary and committed, and the placeholders replaced with real image
references. Each later per-type page will need its own capture set.

### Cross-surface "you need registered components" framing

Tracked separately in `registered-components-required-framing.md` — the docs
should state up front that Joe is near-useless without registered components, in
Overview and Quickstart.

### Watch item — governed connectivity check

The happy path ends on the legacy `POST /components/{id}/test` affordance, which
is not admin-gated and is the subject of the open
`governed-connectivity-check-surface` backlog item. If that item repoints the UI
test affordance at the admin credential-status probe, the activation step in every
registration how-to (including the shipped Kubernetes page) must be revised to
match.
