# Observation-posture conflation — deferred follow-ups

Status: open
Priority: later

Deferred from Session `posture-prompt-conflation` / D-0101, which reworded the
observation posture string (`internal/prompts/posture.go`) after a live-cluster
incident where the model deferred log, event, and spec READS to the operator citing
observation mode. Enforcement was verified correct and untouched (the write floor
gates Mutates only); the fix was wording.

## 1. OASIS scenario pinning the behavior

Wording-level pinning landed this session in `internal/prompts/posture_test.go`
(`TestPostureSection_ObservationReadsUnaffected`): it asserts the string says reads
remain available, forbids citing observation mode as a reason to stop reading or to
hand read steps to the operator, keeps the do-not-attempt instruction for mutations,
and never reintroduces the conflating "offer the read-only investigation" clause.
That pins the **text**, not the **behavior**.

Behavioral pinning belongs to the evaluation framework (OASIS), not to a unit test:
stage an observation-mode ask against `examples/demo-world` where the correct
behavior is a **complete read investigation with cited evidence** (logs, events,
resource specs actually read by the model) **before** any proposal, and where
deferring a read step to the operator on posture grounds is a failure. The scenario
should fail on the pre-D-0101 wording and pass on the current one. See
`docs/backlog/oasis-relationship.md`.

## 2. Model-facing "read-only" language outside the posture string — wording decision pending

The Phase-1 sweep of model-facing tool descriptions and of clarification /
confirmation templates found **no** hit mentioning observation mode, the write floor,
or an inability to make changes. The clarification and confirmation templates are
clean. What it did find is per-tool `read-only` phrasing that describes each tool's
**own** capability, not Joe's posture:

- `internal/tools/core/postgres_stat.go:79` — `return "Execute a read-only SELECT query against a PostgreSQL database. " +`
- `internal/tools/core/mysql_stat.go:79` — `return "Execute a read-only SELECT query against a MySQL database. " +`
- `internal/tools/shared/httpreq/httpreq.go:80` — `return "Probe an HTTP/HTTPS endpoint and return status code, response headers, body snippet, and latency. Read-only: only the safe methods GET and HEAD are permitted. Replaces curl for endpoint health checks and debugging. Requests to cloud metadata endpoints (169.254.169.254) are blocked for safety."`
- `internal/tools/shared/httpreq/httpreq.go:93` — `Description: "Read-only HTTP method: GET or HEAD. Default: GET. Mutating methods (POST, PUT, PATCH, DELETE) are rejected — this is a diagnostic probe, not a write tool.",`
- `internal/tools/shared/websearch/websearch.go:40` — `return "Search the web via the operator-configured search engine and return ranked results (title, URL, and snippet only). Use this to DISCOVER URLs and sources; it never fetches page contents. To read a specific result, pass its URL to http_request. Read-only."`

None of these instruct the model to withhold a read, and none reference the posture,
so none was touched. They are listed here because they share vocabulary with the
posture string and a weak model could, in principle, blur the two: the open question
is whether "read-only" in a tool description should be reworded to something that
cannot be misread as a posture statement (e.g. "does not modify the database").
No change until that decision is made.
