Two hardcoded `0.1.0` version literals sit outside `buildinfo`
Status: open
Priority: later

Opened from the `v0.2.0` go/no-go sweep (session `release-v0.2.0`, D-0137),
which grepped the tree for version strings the release would falsify.

Two packages declare their own version literal rather than reading build truth
from `internal/buildinfo`:

- `internal/mcp/server.go` — `serverVersion = "0.1.0"`, reported as the MCP
  server's version in its protocol handshake.
- `internal/observability/otel.go` — `serviceVersion = "0.1.0"`, attached as the
  OpenTelemetry resource's service version.

Neither is an ldflags `-X` injection target (those are addressed by full import
path into `internal/buildinfo`, per `.goreleaser.yaml`), so neither moves with a
release. Both are unpublished — no page in `docs/public/` states either value —
and neither is read by any release surface: `GET /api/v1/version`,
`GET /api/v1/status`, and the `joe_build_info` gauge all go through
`buildinfo.Get()`, which is unaffected.

The reason this surfaced now rather than earlier is a coincidence that has just
expired. Both literals happened to equal the released version at `v0.1.0`, so
anything reading them got a correct answer by luck. At `v0.2.0` they do not, and
an MCP client reading the handshake version or a trace backend grouping by
`service.version` now sees `0.1.0` against a `0.2.0` binary.

Scope: route both at `internal/buildinfo` so the value cannot drift from the
built artifact, and so the coincidence cannot recur. CLAUDE.md already states
that `internal/buildinfo` is the single source of build truth and that no other
package declares build-identity vars — these two are the counterexample, and
closing this item makes that claim true rather than aspirational. Worth
considering a guard test pinning it, in the shape of the existing structural
guards, since a grep-able literal is exactly the kind of thing that re-enters.

Not in scope: any change to what the MCP handshake or the OTel resource reports
beyond the value's provenance, and any decision about whether the MCP protocol
version (a distinct concept from the server's own version) should likewise be
derived rather than stated.
