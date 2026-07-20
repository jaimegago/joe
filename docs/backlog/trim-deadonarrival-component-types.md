# Unregistrable component types — deferred wiring, a Test-control UX bug, and a latent read-promotion residue

Status: open
Priority: later

Two trims narrowed the registrable-type set (`store.AllowedComponentTypes` /
`store.IsValidComponentType`, `internal/store/constants.go`) so that a type absent
from it is unregistrable through every surface. `trim-deadonarrival-component-types`
(D-0058) removed six types that are non-functional regardless of config —
`oci_registry`, `dockerhub`, `artifactory`, `ecr`, `cloudwatch`, `azuremonitor`.
`trim-unsupported-component-types` extended that to the twelve that fail the D-0055
documentable gate — `azure`, `helm`, `nginx-ingress`, `git`, `aws`, `datadog`,
`postgresql`, `mysql`, `redis`, `mongodb`, `kafka`, `elasticsearch` — none of which
is credential-wired, so promotion already rejected them and none could be completed
into a working integration.

Neither trim wired or built **any** adapter; both only closed the hole. The work
below was surfaced by those changes and is explicitly **not** addressed by them.

## 1. Wire the four adapter-bearing registry types into a construction map

`oci_registry`, `dockerhub`, `artifactory`, and `ecr` already have adapter
packages (`internal/adapters/registry/...`), refresh paths
(`internal/coreagent/registry_refresh.go`, plus the type-switch cases in
`internal/coreagent/refresh.go`), query tools (`internal/tools/core/registry_tools.go`),
access wiring (`internal/access/registry.go`), and HTTP routes
(`internal/api/registry.go`) — but they are constructed by **no** path: neither the
boot map `connectSourcesDefault` (`cmd/joe/server.go`) nor the runtime map
`newAdapterForType` (`internal/api/components.go`) builds them. To make them
functional, add construction entries to the appropriate map(s) and only then
restore them to the registrable set. Their constants remain defined today (the
refresh type-switch still names them) and carry an unregistrable comment in
`internal/store/constants.go`.

## 2. Build the two missing adapters from scratch

`cloudwatch` and `azuremonitor` have **no adapter code at all** — their constants
were removed entirely by the D-0058 trim because nothing outside the registrable
lists referenced them. Adding these as functional types means building the adapter
packages from scratch (config, Connect, refresh/query surfaces), wiring them into a
construction map, and then adding the constants back to the registrable set.

## 3. Wire a credential path for the twelve unsupported types

Unlike the groups above, all twelve have adapter packages AND construction entries —
`aws`, `azure`, `git`, and `datadog` are built at boot (`connectSourcesDefault`), and
the rest are in the runtime map `newAdapterForType`. What they lack is a **credential
path**: none appears in `wiredTypes` (`internal/credential/wiring.go`), so the
promotion endpoint rejects them as unwired and they can never be armed. Restoring any
one means building its credential provider, adding it to `wiredTypes` (plus a segment
in `envPrefixSegments`, `internal/credential/references.go`, if it is `KindStatic`),
and only then adding the constant back to the two registrable lists. Their constants
remain defined and carry an unregistrable comment.

The credential shape each needs, per the deliberately-absent notes in `wiring.go`:

- `git` — `auth_type`-discriminated `ssh-key-path` / `http-token`, not a single
  static token, so it needs a discriminated provider rather than the static seam.
- `datadog` — an `api_key` + `app_key` pair, which is why A003-W2 left it out of the
  static-token batch.
- `helm`, `nginx-ingress` — kubeconfig-shaped. Note these are the two accepted
  `clientcmd` importers in the D-0064 break-test allow-list; wiring them must not
  re-admit `clientcmd` to the transport or credential path.
- `aws`, `azure` — cloud SDK credential chains. AWS already has its own item,
  `aws-credential-provider.md` (deferred from A003); treat that as the authority for
  the AWS half rather than duplicating it here.
- `postgresql`, `mysql`, `redis`, `mongodb`, `kafka`, `elasticsearch` — DSN/URI plus
  password shapes, not an unambiguous single static token.

Note that after the second trim, the registrable-but-unwired intersection is exactly
`{terraform, envoy}` — both credential-less. That pair is what keeps the promotion
reject-unwired guard reachable in production, and it is the fixture the
`TestPromote_RejectsUnwiredType` / `..._UnwiredSorted` tests now use (they used
`datadog` until it became unregistrable).

## 4. The runtime Test control reports a misleading no-op success for the boot-only types

Separately from either trim: the Test control (`handleTestComponent`,
`internal/api/webui.go`) calls `newAdapterForType(src.Type)` and, when that returns
`nil`, reports `ok: true` with `"type %q has no connection to test"`. For the
boot-only group — which IS registrable and IS constructed, but only at boot
(`connectSourcesDefault`), not by the runtime `newAdapterForType` map — Test
therefore returns a misleading success even though a real connection exists and
could be probed (after a restart wires the adapter). That is a UX-correctness bug
in the Test control's adapter-resolution path, not part of either trim. Fixing it
means resolving the live boot-constructed adapter (or otherwise distinguishing
"genuinely connectionless type" from "constructed only at boot") rather than
treating a `nil` runtime adapter as "nothing to test".

The affected registrable group is now `github`, `gitlab`, `splunk`, `dynatrace`,
`newrelic`. `datadog` was in this group until `trim-unsupported-component-types` made
it unregistrable; it stays boot-constructed for already-stored rows, so the
misleading Test result still reaches such a row, but no new datadog component can be
registered.

## 5. Latent: a stored read-promotion row for an unregistrable type still enforces

The auto_promote_reads admin surface is the fourth consumer of the registrable-type
seam: `listReadPromotions` (`internal/api/admin.go`) composes its per-type view by
iterating `AllowedComponentTypes` and overlaying stored rows, and `setReadPromotion`
validates against `IsValidComponentType`. Enforcement, however, does **not** go
through the enum — the resolver `IsPromoted`
(`internal/promotereads/promotereads.go`) queries `agent_read_promotions` by
`component_type` directly, with no enum gate.

So an install that already had an ON row for a now-unregistrable type (say `aws`)
keeps admitting `agent:core` reads on its stored `aws` components, while the operator
can no longer see that row in the admin view or flip it through the setter — the flag
becomes invisible but live. This is the same shape as the read-path tolerance D-0058
accepted (a stored row of a removed type still lists and reads), and it is **latent,
not new**: no shipped install can have such a row, because no version that allowed
registering these types was ever tagged. It is recorded here rather than fixed
because the honest fix is a decision, not a patch — either reconcile stored promotion
rows against the enum at boot, or have `IsPromoted` consult the enum and fail closed
for an unregistrable type. Do not change enforcement without deciding which, since
both alter an authz-adjacent path.
