Guard test pinning the no-MCP-client invariant
Status: open

D-0067 categorically rejects Joe acting as an MCP client, and the live tree
honors it — the only MCP dependency is imported server-side, with no client
import or client symbol in non-test code. But unlike the clientcmd confinement
invariant, which is pinned by a repo-walking break-test in the k8s adapter
package, nothing fails the build if an MCP client import or client construction
is ever introduced. Add a structural guard test in the spirit of the existing
transport break-tests: walk production packages and fail on any import of the
MCP library's client path or equivalent client-capability symbol, with an
explicit exception list if any is ever justified. Until this lands, the public
Safety deep-dive on joeagent.dev deliberately states that the invariant is true
of the tree but not yet test-pinned; when this lands, that sentence on the site
should be updated. The observation derives from a read-only investigation at
HEAD a6425f4; re-derive coordinates and the current MCP dependency before
implementation.
