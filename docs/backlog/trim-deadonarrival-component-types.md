# Dead-on-arrival component types — deferred wiring and a related Test-control UX bug

Status: open

The `trim-deadonarrival-component-types` change (D-0058) removed six component
types — `oci_registry`, `dockerhub`, `artifactory`, `ecr`, `cloudwatch`,
`azuremonitor` — from the registrable set (`store.AllowedComponentTypes` /
`store.IsValidComponentType`) so they are unregistrable through every surface. The
trim deliberately wired and built **no** adapter; it only closed the
dead-on-arrival hole. The following work was surfaced by that change and is
explicitly **not** addressed by it.

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
refresh type-switch still names them) and carry a dead-on-arrival comment in
`internal/store/constants.go`.

## 2. Build the two missing adapters from scratch

`cloudwatch` and `azuremonitor` have **no adapter code at all** — their constants
were removed entirely by the trim because nothing outside the registrable lists
referenced them. Adding these as functional types means building the adapter
packages from scratch (config, Connect, refresh/query surfaces), wiring them into a
construction map, and then adding the constants back to the registrable set.

## 3. The runtime Test control reports a misleading no-op success for the boot-only types

Separately from this trim: the Test control (`handleTestComponent`,
`internal/api/webui.go`) calls `newAdapterForType(src.Type)` and, when that returns
`nil`, reports `ok: true` with `"type %q has no connection to test"`. For the
boot-only group (`github`, `gitlab`, `datadog`, `splunk`, `dynatrace`, `newrelic`)
— which IS registrable and IS constructed, but only at boot
(`connectSourcesDefault`), not by the runtime `newAdapterForType` map — Test
therefore returns a misleading success even though a real connection exists and
could be probed (after a restart wires the adapter). That is a UX-correctness bug
in the Test control's adapter-resolution path, not part of this trim. Fixing it
means resolving the live boot-constructed adapter (or otherwise distinguishing
"genuinely connectionless type" from "constructed only at boot") rather than
treating a `nil` runtime adapter as "nothing to test".
