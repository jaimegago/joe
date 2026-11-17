# Component registration guides — standing watch items
Status: open
Priority: later

The registration how-tos are written: every registrable type has a UI walkthrough under
`docs/public/guides/`. No page work remains here. What remains is a list of facts the
pages state about the product as it is, each of which obliges a page revision if the
product changes. Nothing below is work until its trigger fires.

## The pages

- `register-kubernetes.md` (D-0059) — `kubernetes`.
- `register-git.md` — `git` (D-0150).
- `register-token-components.md` — `github`, `gitlab`, `pagerduty`.
- `register-url-components.md` — `prometheus`, `mimir`, `loki`, `tempo`, `jaeger`,
  `alertmanager`, `falco`: a required `url`, and a bearer token the adapter treats as
  optional.
- `register-url-token-components.md` — `grafana`, `argocd`, `splunk`, `dynatrace`,
  `newrelic`: a required `url` (for `newrelic`, the integer `account_id`), and a token the
  adapter refuses to connect without.
- `register-terraform-envoy.md` — the credential-less `terraform` (`state_path`) and
  `envoy` (`url`), with no promotion step.

All follow D-0119 (promote connects; Test Connection confirms and retries) and D-0148 (no
screenshots, no placeholders; the only sanctioned future path is
`guide-screenshots-e2e-capture.md`). Each type's fields were taken from the registration
contract in `internal/credential/registration.go`, the one `GET /api/v1/component-types`
serves. The cross-surface "you need registered components" framing is tracked separately
in `registered-components-required-framing.md`.

## Watch item — governed connectivity check

Every registration how-to ends on the legacy `POST /components/{id}/test` affordance,
which is not admin-gated and is the subject of the open
`governed-connectivity-check-surface` backlog item. If that item repoints the UI test
affordance at the admin credential-status probe, the confirmation step in every
registration how-to must be revised to match.

Two pages lean on that route harder than the rest, because for them the test is the step
that connects an unpromoted component:

- `register-terraform-envoy.md` — the whole happy path. These types can never be
  promoted, so the test and the boot connect pass are the only things that bring one live.
- `register-url-components.md` § If the backend needs no token — the seven types there
  resolve an empty static credential when unpromoted and send no `Authorization` header,
  so an unpromoted component of those types connects on Test Connection and at boot, and
  stays **inert**. The Promote dialog offers them no no-credential arm; only `git` has one
  (D-0150). `governed-connectivity-check-surface.md` assumes an unpromoted component fails
  to connect, which is not true of these types.

If the test route is gated on arming, or a no-credential arm is extended to those seven
types, both pages change. `docs/public/concepts/component-lifecycle.md` says a registered
component "can do nothing" until it is armed, and the Register dialog says the same; the
two pages above describe what the tree does instead.

## Watch item — what Test Connection proves, per type

The pages state what the test proves for each type, from each adapter's `Connect`. Revise
the matching paragraph if a probe changes.

- `github`, `gitlab` — no API call in `Connect`, so the test passes once the token
  resolves, while the success message says it "verifies reachability and authentication".
  `register-token-components.md` states this.
- `grafana`, `argocd` — the probes are `/api/health` and `/api/version`, which those
  services answer without checking the token. `register-url-token-components.md` states
  this.
- `newrelic` — the probe is a NerdGraph query for the key's own user; `account_id` is not
  part of it.

## Watch item — types with no agent tool

`splunk`, `dynatrace` and `newrelic` register, promote and connect, and no tool in
`internal/tools/default.go` queries them: the refresher writes one graph node per
component and nothing else reads the adapter. `register-url-token-components.md` says so
in its last step. `splunk`'s optional `index` field is declared, collected by the dialog,
and used by nothing beyond the adapter's config parse. If a tool lands for any of the
three, replace that paragraph with an example question, as the other types have.
