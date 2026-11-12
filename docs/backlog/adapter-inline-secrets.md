# Adapters still read inline secrets: retire the fallback, give webhook_secret a promoted path
Status: open

D-0029 drew the line between non-credential routing config, entered at registration, and
credentials, entered only at promotion as a reference. Three keys that adapters read
predate that line and sit on the wrong side of it: `api_key`, `token` and
`webhook_secret`. D-0168 pinned them as the adapter-read secret set
(`credential.AdapterSecretFields`), neither declared routing fields nor in the
reflection-derived credential denylist, and since D-0168 registration refuses all three.
What is stored already is untouched, and two things remain.

## 1. Remove the inline `token` / `api_key` fallback at Connect

Fourteen adapters, serving fifteen registrable types (`mimir` shares the `prometheus`
adapter), resolve the credential through the provider seam and then let the resolved
static value override the inline field **only when it is non-empty**. So a component
whose stored config carries an inline `token` or `api_key` connects with it, with no
promotion at all:

- `internal/adapters/observability/prometheus/prometheus.go:132` (`prometheus`, `mimir`)
- `internal/adapters/observability/loki/loki.go:118`
- `internal/adapters/observability/tempo/tempo.go:125`
- `internal/adapters/observability/jaeger/jaeger.go:117`
- `internal/adapters/observability/splunk/splunk.go:116`
- `internal/adapters/observability/dynatrace/dynatrace.go:137`
- `internal/adapters/observability/newrelic/newrelic.go:115`
- `internal/adapters/alerting/alertmanager/alertmanager.go:119`
- `internal/adapters/alerting/pagerduty/pagerduty.go:124`
- `internal/adapters/alerting/grafana/grafana.go:145`
- `internal/adapters/security/falco/falco.go:123`
- `internal/adapters/gitops/argocd/argocd.go:143`
- `internal/adapters/github/adapter.go:98`
- `internal/adapters/gitlab/adapter.go:96`

Measured at joe `7476ed5`. The design record that named this counted thirteen adapters;
the tree holds fourteen.

Registration can no longer write such a field, but a component registered before D-0168
may still hold one, and the path is admin-gated and audited rather than closed. It is the
promotion boundary being optional for these types, not the unauthenticated vector D-0029
closed. Whether the fallback was ever intended is not recorded. The work: decide what
happens to a stored inline secret (refuse to connect and say why, or migrate it out), then
drop the fallback so the resolved reference is the only credential an adapter uses, and
remove the fields from the adapter config structs. Removing a key from a struct shrinks the
pinned secret set: `TestAdapterSecretFields_AreReadByTheAdapters` fails until the set is
updated, which is the intended prompt.

## 2. Give `webhook_secret` a path in through promotion

`github` and `gitlab` read `webhook_secret` from component config
(`internal/adapters/github/config.go:14`, `internal/adapters/gitlab/config.go:13`) and
expose it through `WebhookSecret()`, read by `Accessor.GitHubWebhookSecret` and
`GitLabWebhookSecret` (`internal/access/vcs.go:23,40`). No webhook receiver calls those
today: at `7476ed5` neither has a non-test caller and no route receives a webhook. So
refusing the field at registration starves nothing yet. A PR-event surface
(`pr-surface-joe-mention`) would need it, and it should arrive as a credential reference
through promotion, not as an inline value. That needs a second credential per component,
which the one-Kind promotion model does not carry today, so it is governance-class design
work in its own right.
