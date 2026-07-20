Backlog — Web-search tool fast-follows
Status: open
Priority: later

The `web_search` shared tool shipped in session `web-search-tool` (D-0064): a Go-native,
`ActionRead`-classified tool behind the `internal/search` `SearchProvider` abstraction, with
**SearXNG** implemented as the recommended keyless self-host backend. It is configured
boot-only (`config.WebSearchConfig` + `JOE_WEBSEARCH_*` env), exposed-and-deny when
unconfigured, registered on the **user task loop only**, and egresses to the single
operator-configured `base_url` with no allow-list, no rate limit, and no audit row. The
following threads are deliberately deferred and keep this item open.

## Deferred items

- **Keyed hosted providers (Tavily, Brave).** Add real keyed provider implementations behind
  the existing `search.Provider` interface and `search.NewProvider` factory switch — the clean
  additive extension the abstraction was built for. Their API keys ride plain config/env like
  the LLM provider keys (`web_search.api_key` / `JOE_WEBSEARCH_API_KEY`), not the component
  credential-reference model. Each is a new `case` in the factory plus a provider file; no
  change to the tool, the classifier row, or the registration path.

- **`agent:core` registration of web search.** This build registers `web_search` only in the
  user-task-loop shared-tool path (`internal/tools/default.go`); the autonomous `agent:core`
  registry (`internal/coreagent`, `registerCoreAgentTools`) is intentionally left unchanged.
  Deciding whether the autonomous refresh/onboarding surface should be able to search the web —
  and under what governance — is its own decision. If taken, it registers the same tool
  (already `ActionRead`) into the agent:core registry.

- **Runtime swap / admin configuration surface.** Web search is boot-only today: changing the
  backend requires a restart, and there is no admin endpoint or audit vocabulary for it. A
  runtime swap surface (admin REST + audit row, in the shape of the read-posture / read-promotion
  admin surfaces) would let an operator repoint or disable the backend without a restart.

- **Egress-trace fast-follow (optional).** An egress rate-limit, a per-`base_url` allow-list, or
  a plain structured per-search log line (query + result count + backend, no bodies) would give
  operators a lightweight egress trace. None are present today by deliberate parity with the
  existing shared-tool posture (external-network egress is not a gate dimension; operator-run
  egress gateways are the substrate). Any of these is a self-contained additive change.
