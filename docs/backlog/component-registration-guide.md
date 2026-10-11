# Component registration guide — UI-driven public how-to
Status: in-progress
Priority: now
Blocked-by: register-component-config-default

## What has shipped

- `docs/public/guides/register-kubernetes.md` (D-0059) — Kubernetes.
- `docs/public/guides/register-git.md` — `git`: repository URL at registration, then a
  static `JOE_GIT_<LABEL>` reference or the explicit no-credential arm (D-0150).
- `docs/public/guides/register-token-components.md` — `github`, `gitlab`, `pagerduty`,
  grouped on one page because they differ only in the env-var prefix and what the token
  is. Covers the static promotion form's candidate picker and the compose-a-label
  fallback with its not-currently-set warning.

All three follow D-0119: promote connects, and Test Connection is a confirmation and
retry step, not the activation step. None carries screenshots or placeholders (D-0148).

The thread that shipped the last two (`component-registration-guide`, 2026-10-11) also
fixed a defect that had made them impossible to write: eight adapters (`github`,
`gitlab`, `pagerduty`, `grafana`, `argocd`, `splunk`, `dynatrace`, `newrelic`) required
their inline credential field in `ParseConfig`, before the armed reference was resolved,
so every promoted component of those types failed to connect with `token is required` /
`api_key is required`. The check now runs in `Connect`, after resolution, pinned per
adapter by `TestConnect_PromotedReferenceOnly` (`ResolvesEnvVar` for `github` and
`gitlab`, which no longer carry a placeholder token).

The "boot-config-only" category this item used to describe is gone: since
`adapter-dispatch-consolidation` every type is built by `factory.New`, so all of them
come live at runtime. `docs/public/components/` was corrected to match.

## Remaining work — blocked on a UI routing locator

Every other documentable type needs a non-credential routing field at registration that
the web UI cannot collect: the Register dialog sends a `config` only for `git`, the static
Promote form collects only the env-var reference, and there is no route for editing a
component's config afterwards.

- `url`: `prometheus`, `mimir`, `loki`, `tempo`, `jaeger`, `alertmanager`, `grafana`,
  `argocd`, `falco`, `splunk`, `dynatrace`, and the credential-less `envoy`.
- `account_id`: `newrelic`.
- `state_path`: the credential-less `terraform`.
- The endpoint override for GitHub Enterprise Server, self-managed GitLab, and PagerDuty's
  EU region (`base_url` / `api_url`).

These types work today through the API, with the field in `config` at registration —
`docs/public/components/connectable-systems.md` marks each one "API only". A UI how-to for
them waits on item 3 of `register-component-config-default.md` (a per-type routing field
in the Register dialog). When that lands, add the static types to
`register-token-components.md` or as sibling pages, and give `terraform` and `envoy` a
page whose happy path has no promotion step.

## Screenshots

Every registration how-to ships with no screenshots, by decision (D-0148): manual capture
was rejected. The only sanctioned future path is deterministic capture from the
joe-oasis-e2e Playwright harness, regenerated per release as a build artifact — see
`guide-screenshots-e2e-capture.md`. Later pages follow the same decision and carry no
placeholders.

## Cross-surface "you need registered components" framing

Tracked separately in `registered-components-required-framing.md`.

## Watch item — governed connectivity check

Every registration how-to ends on the legacy `POST /components/{id}/test` affordance,
which is not admin-gated and is the subject of the open
`governed-connectivity-check-surface` backlog item. If that item repoints the UI test
affordance at the admin credential-status probe, the confirmation step in every
registration how-to must be revised to match.

## Watch item — what Test Connection proves for GitHub and GitLab

The `github` and `gitlab` adapters make no API call in `Connect`, so Test Connection
reports `connection successful` once the token resolves, while the success message itself
says it "verifies reachability and authentication". `register-token-components.md` states
what the test actually proves for those two types. If either adapter gains a probe in
`Connect`, revise that paragraph.
