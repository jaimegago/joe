# Joe — Phase 2 Implementation Notes (Single Agentic Runtime)

Status: PLANNING — awaiting review. **No code changes accompany this
document.** It is the implementation plan for Phase 2 as scoped by
[docs/PLAN-OF-RECORD-RECONCILED.md](PLAN-OF-RECORD-RECONCILED.md) §3 (Phase 2)
and indexed as D-0001 in [docs/DECISIONS.md](DECISIONS.md). Phase 0's
session-model design ([docs/PHASE-0-SESSION-MODEL.md](PHASE-0-SESSION-MODEL.md))
is **closed and not reopened** here.

Per the agreed sequencing, this is the first Phase 2 commit (notes only). Code
begins only after review approval.

---

## 0. Goal restated

Collapse to a single agentic runtime:

- **One LLM contact point** — joe-core's `services.LLM` only.
- **One agentic loop** — joe-core's server-side loop only.
- **The CLI becomes a thin client** — sends user input to joe-core over HTTP,
  streams the loop's output back, renders it, and services local-tool
  callbacks against the operator's own machine.
- `/model`, `joe mcp serve`, and oasisctl's `/api/v1/tasks` use all keep
  working.

Phase 2 completion gate (Plan-of-Record §3): exactly one adapter instantiated
process-wide; exactly one loop implementation reachable at runtime; the CLI
performs no LLM calls and runs no loop of its own.

---

## 1. Code-truth findings (verified, before planning)

The Plan-of-Record §1 says "joe-core's Core Agent (`internal/coreagent/agent.go`)
calls `a.llm.Chat()` directly and loops." **That is not what the code does**, and
the difference matters for Phase 2. Recorded here as a code-truth correction; it
does not reopen any Phase 0 design decision.

What is actually in the tree:

- **The conversational agentic loop lives in one place:** `useragent.Agent.Run`
  ([internal/useragent/agent.go:94](../internal/useragent/agent.go#L94)) — the
  LLM→tools→LLM loop. It is **instantiated in two processes**:
  1. **CLI process** — [cmd/joe/main.go:857](../cmd/joe/main.go#L857) builds a
     `useragent.Agent` with its own adapter
     ([cmd/joe/main.go:765](../cmd/joe/main.go#L765),
     `deps.newAdapter`) and the full local+shared+core registry
     (`tools.NewDefaultRegistryWithClient`,
     [internal/tools/default.go:190](../internal/tools/default.go#L190)). Driven
     by the REPL ([internal/repl/repl.go:91](../internal/repl/repl.go#L91)).
  2. **joe-core process** — `POST /api/v1/tasks`
     ([internal/api/tasks.go:97](../internal/api/tasks.go#L97)) builds a
     `useragent.Agent` per request with `services.LLM` and a **core-only**
     registry (`tools.NewCoreRegistry`,
     [internal/tools/default.go:69](../internal/tools/default.go#L69) — shared +
     core tools, **no local tools**) plus an internal loopback HTTP client for
     core tools.
- **`internal/coreagent.Agent`** ([internal/coreagent/agent.go](../internal/coreagent/agent.go))
  is **not** a conversational loop. It owns graph-mutation tools and runs
  background refresh/discovery/onboarding. Its only LLM use is single-shot
  (`joefile_service.go`, refresher, discovery engine). It does **not** loop
  LLM→tools→LLM for user turns. So "the single loop lives in the Core Agent" is
  aspirational language; the real server-side loop is the `/api/v1/tasks`
  handler.
- **Two adapters today:** CLI `deps.newAdapter`
  ([cmd/joe/main.go:765](../cmd/joe/main.go#L765)) and joe-core
  `deps.newLLMAdapter` → `services.LLM`
  ([cmd/joe-core/main.go:443](../cmd/joe-core/main.go#L443)).
- **`services.LLM` is set once and never swapped**
  ([cmd/joe-core/main.go:448](../cmd/joe-core/main.go#L448)); read by the task
  handler and the Web UI chat handler
  ([internal/api/webui.go:340](../internal/api/webui.go#L340)). It is a bare
  field with no concurrency guard for replacement.
- **No model-management HTTP API exists.** `/model` is entirely CLI-local today:
  it lists `cfg.LLM.ModelNames()` and hot-swaps the CLI adapter via
  `useragent.Agent.SwitchModel`
  ([internal/repl/repl.go:134](../internal/repl/repl.go#L134),
  [internal/useragent/agent.go:61](../internal/useragent/agent.go#L61)).
- **Local vs core tools are already cleanly partitioned.** Local tools
  (`read_file`, `write_file`, `run_command`, `git_status`, `git_diff`,
  `ask_user`) only make sense on the operator's machine; `NewCoreRegistry`
  already omits them ([internal/tools/default.go:64-68](../internal/tools/default.go#L64-L68)).
  `ask_user` ([internal/tools/local/askuser/askuser.go:62](../internal/tools/local/askuser/askuser.go#L62))
  reads `os.Stdin` — it is inherently a CLI-process tool.
- **MCP** (`internal/mcp/dispatcher.go`) calls category client methods
  (`GraphQuery`, `QueryK8s`, `QueryMetrics`, …), **not** `/api/v1/tasks` and not
  the loop. Removing the CLI loop cannot affect it.
- **oasisctl** is an external consumer of `POST /api/v1/tasks` (no in-repo Go
  caller). Keeping that endpoint's request/response contract unchanged is what
  preserves it.
- **`/api/v1/tasks` is effectively single-turn:** it builds a fresh
  `useragent.NewSession` each request and does not preload prior messages from
  the store (contrast the Web UI handler, which preloads via
  `GetMessages`, [internal/api/webui.go:300](../internal/api/webui.go#L300)). A
  multi-turn REPL therefore needs server-side history continuity (§5).

Net consequence for Phase 2: we are **removing one of two instantiations of an
existing loop**, not building a new server loop from scratch. The server already
runs the loop; it just (a) returns one JSON blob instead of streaming, (b) has no
local tools, and (c) is single-turn.

---

## 2. Decisions to record in DECISIONS.md (new entry D-0003)

### 2a. Streaming protocol: **Server-Sent Events (SSE)**

joe-core → CLI streaming is **SSE** (`Content-Type: text/event-stream`,
`event:`/`data:` framing), not chunked newline-JSON.

Rationale:
- Self-describing named event types (`step`, `local_tool_call`, `final`,
  `error`) map cleanly onto the existing observer step shape
  (`useragent.StepRecord`, surfaced today as `taskResponse.Steps`).
- The repo already ships a React Web UI (`ui/`). SSE is consumable by the
  browser's native `EventSource`, so the **same** joe-core streaming endpoint can
  later back both the CLI and a streaming Web UI chat — one protocol, not two.
- Plain HTTP/1.1, no new dependencies; trivial to parse in Go with
  `bufio.Scanner` on the client.
- The control direction (CLI → joe-core, for local-tool results) is a separate
  ordinary `POST` (§3), so SSE's unidirectionality is not a limitation.

### 2b. Tool-execution boundary: **local tools execute in the CLI (callback path)**

Local tools (`read_file`, `write_file`, `run_command`, `git_status`,
`git_diff`, `ask_user`) **continue to execute in the `joe` CLI process** and are
delegated from the server loop via a callback over the streaming protocol. Shared
(Go-native diagnostic) and core (graph/k8s/observability/…) tools continue to
execute inside joe-core.

Rationale (the security property, as the task states):
- The CLI's filesystem and shell access is bounded by the **operator's own
  shell/user**, not by joe-core's process (which may run as a daemon, in a
  container, or on another host). Moving `write_file`/`run_command` into joe-core
  would (a) break the security boundary and (b) be functionally wrong — joe-core
  cannot see the operator's working directory.
- `ask_user` is definitionally a CLI-side prompt against the operator's TTY.

Token-usage note (per the task, **not implemented in Phase 2**): with a single
loop, token accounting collapses to one place — joe-core's loop already
aggregates `resp.Usage` per step (`useragent.Session.AddTokenUsage`) and the task
response already carries `total_tokens`. After Phase 2 there is no second
(CLI-side) tally to reconcile, so future token-visibility work has a single
authoritative source. Recorded in DECISIONS.md; no token UI is built here.

---

## 3. Streaming protocol shape

Two **additive** endpoints (existing `/api/v1/tasks` is untouched, preserving
oasisctl):

**`POST /api/v1/tasks/stream`** — start/continue a streamed turn.
- Request body extends `taskRequest`:
  - `message`, `session_id` (as today; `session_id` now drives multi-turn
    history continuity, §5),
  - `client_tools`: `[]{name, description, parameters}` — the local-tool
    definitions the CLI can service. The server registers these as **delegating
    stubs** in the loop's registry so the LLM sees them as available but their
    execution is delegated back to the client.
- Response: `Content-Type: text/event-stream`. Event types:
  - `event: step` — one loop iteration finished. `data:` is the existing
    `taskStep` JSON (assistant content, tool calls, in-process tool results) for
    rendering.
  - `event: local_tool_call` — the loop needs a local tool run.
    `data: {call_id, name, args}`. The loop **suspends** until the matching
    result arrives.
  - `event: final` — `data:` is the existing `taskResponse` JSON (final answer,
    status, tools used, token totals).
  - `event: error` — `data: {message}`.

**`POST /api/v1/tasks/stream/{taskID}/tool-results`** — CLI → joe-core callback
delivering a delegated local-tool result.
- Body: `{call_id, result, error}`.
- The handler routes the result to the suspended loop (per-task in-flight
  registry keyed by `taskID`, holding a result channel per `call_id`). The loop
  resumes.

Server-side mechanics:
- A **delegating executor** wraps the loop's executor. For a delegated
  (client-side) tool name it emits a `local_tool_call` SSE event and blocks on a
  per-`call_id` result channel; for any other tool it executes in-process as
  today. This keeps `useragent.Agent.Run` unchanged — delegation lives entirely
  in the executor + a per-task coordinator.
- Single-loop invariant preserved: the loop still runs on one goroutine; the
  delegating executor **blocks** the caller goroutine awaiting the callback
  (it does not spawn a concurrent loop). The SSE writer goroutine only writes
  events; it does not run loop logic. This must be asserted in tests (no
  `go func()` around `Agent.Run`).

Middleware/transport caveat to verify during implementation: the joe-core
middleware chain wraps the `ResponseWriter`
([cmd/joe-core/main.go:538-545](../cmd/joe-core/main.go#L538-L545):
CORS → rate-limit → metrics → auth → identity → RBAC → size-limit → mux). The
metrics/size-limit wrappers must pass through `http.Flusher` or SSE will buffer.
If they don't, add a `Flush()` passthrough to the wrapping `ResponseWriter`
(small, isolated fix). Auth/identity/RBAC apply to the streaming endpoints
exactly as to `/api/v1/tasks` — no auth bypass.

---

## 4. `/model` after the refactor

Model selection becomes an operation against the single runtime (Plan-of-Record
§3), exposed over the existing CLI→core HTTP channel:

- **`GET /api/v1/models`** → `{available: []string, current: string}` from
  joe-core's `cfg.LLM` ([internal/config/config.go:106](../internal/config/config.go#L106)).
- **`POST /api/v1/models/current`** → `{name}`; joe-core validates the model
  exists + API keys are present (server-side `config.ValidateAPIKeys`), builds a
  new adapter via `llmfactory.NewAdapter`, and **hot-swaps `services.LLM`**.

To swap `services.LLM` safely while the task handler / Web UI read it
concurrently, introduce a small **swappable adapter** in joe-core: an
`llm.LLMAdapter` implementation holding the inner adapter under an `RWMutex` with
a `Swap(newInner)` method. `services.LLM` is set to this wrapper at startup; the
model API swaps the inner adapter. (This mirrors the existing
`useragent.Agent.SwitchModel` mutex pattern, lifted to the service level.)

CLI side: `/model` ([internal/repl/repl.go:134](../internal/repl/repl.go#L134))
calls `GET /api/v1/models` for the list and `POST /api/v1/models/current` to
switch. No CLI-side adapter exists anymore.

**Documented consequence (semantics change):** the model is now a property of the
single runtime, so `/model` changes joe-core's active model **globally** (it
affects subsequent Web UI / Slack / other-CLI turns too). This is the direct,
intended consequence of "one LLM contact point." For Joe's current embedded
single-operator deployment this is indistinguishable from today. A per-session
model override is **not** built (it would require per-request adapter creation,
which conflicts with "exactly one adapter instantiated process-wide" — Plan-of-
Record §3 / Invariant 6). Noted as a deferred enhancement.

---

## 5. Multi-turn continuity

The REPL is multi-turn; `/api/v1/tasks` is single-turn (§1). The streaming
handler will preload prior conversation for `session_id` from the existing
`store.Sessions` (the pattern already used by the Web UI handler,
[internal/api/webui.go:300](../internal/api/webui.go#L300)), run the turn, stream
it, and persist user + assistant messages (as `/api/v1/tasks` already does,
[internal/api/tasks.go:306-321](../internal/api/tasks.go#L306-L321)). State lives
in the store between turns; no long-lived per-connection server state beyond the
ephemeral in-flight tool-callback registry for the duration of a single turn.

(The Phase-1 `agent_sessions`/`agent_runs` durable model is **not** adopted as
the REPL transport here — Phase 1 explicitly left the loops uncollapsed and built
that substrate for the post-refactor Core Agent. Wiring the REPL onto the durable
run model is a clean follow-on, not Phase 2's collapse step. Phase 2 reuses the
existing `store.Sessions` continuity to keep scope to the loop collapse.)

---

## 6. Files that will change

**Added (joe-core):**
- `internal/api/tasks_stream.go` — SSE streaming handler, delegating executor,
  per-task in-flight callback registry, `tool-results` callback endpoint.
- `internal/api/models.go` — `GET /api/v1/models`, `POST /api/v1/models/current`.
- `internal/llm/swappable.go` (or `internal/api/`) — `RWMutex`-guarded swappable
  adapter wrapper; small `Swap` surface.

**Added (CLI/client):**
- `internal/client/tasks.go` — `StreamTask(...)` SSE consumer + `tool-results`
  poster; `ListModels`, `SetModel`.
- New thin-REPL driver (likely reworking `internal/repl/repl.go` and/or a new
  `internal/repl/stream.go`) that: sends input, renders `step`/`final` events,
  and services `local_tool_call` events by executing the local tool registry in
  the CLI process and POSTing results back.

**Modified:**
- `cmd/joe/main.go` — **remove** `deps.newAdapter` usage, the instrumented
  adapter, `adapterFactory`, and `useragent.NewAgent`
  ([cmd/joe/main.go:765-864](../cmd/joe/main.go#L765-L864)). The CLI keeps:
  config load, joe-core connectivity (Ping), safety policy load, and a
  **local-only** tool registry (local + shared tools the CLI executes for
  callbacks). It no longer builds a core registry for an in-process loop (core
  tools now run in joe-core).
- `internal/repl/repl.go` — drive the stream instead of `agent.Run`; `/model`
  via the model API; keep `/panic`, `/help`, `/exit`.
- `internal/api/server.go` — register the two new route groups.
- `cmd/joe-core/main.go` — set `services.LLM` to the swappable wrapper; wire the
  model API's swap handle.
- `internal/api/tasks.go` — minor refactor to share the agent/registry/prompt
  setup between the existing `/tasks` and the new streaming handler (extract a
  helper; **do not** change `/tasks`'s contract).
- `docs/DECISIONS.md` (D-0003), `docs/PLAN-OF-RECORD-RECONCILED.md` (mark Phase 2
  complete).

**Removed / relocated — see §7 (the one open decision).**

---

## 7. Open decision for review: where the loop implementation lives

The acceptance criterion "No code path in `internal/useragent/` or `cmd/joe/`
instantiates an LLM adapter or runs an agentic loop" can be read two ways. This
is the one structural choice I want confirmed before coding the final cleanup
commit.

- **Option A (recommended) — relocate the loop to a joe-core-owned package.**
  Move `Agent`/`Session`/`Observer`/constants out of `internal/useragent/` into a
  neutral package (proposed `internal/agentloop/`; alternatively fold into
  `internal/coreagent/`). Update the one server-side importer
  (`internal/api/tasks.go`) and tests. After this, `internal/useragent/` no
  longer exists, so the criterion is satisfied literally and by intent, and the
  package name stops implying a CLI-side agent. Larger but mechanical diff.

- **Option B (lighter) — keep `internal/useragent/` as the shared loop
  implementation**, used only by joe-core; the CLI simply stops importing it.
  Satisfies the Plan-of-Record gate ("one loop implementation reachable;" the CLI
  runs none) but leaves a loop implementation physically in a package named
  `useragent`, which reads against the literal task wording.

I recommend **Option A** and will sequence it as an isolated final commit (§8,
commit 7) that can be dropped if you prefer B. Either way, the substantive
collapse (commits 2–6) is identical.

---

## 8. Commit sequence (each leaves build green + `go test ./...` passing)

1. **(this commit)** `docs/PHASE-2-IMPLEMENTATION-NOTES.md` — notes only. **Stop
   for review.**
2. **Model API + swappable adapter** — `internal/llm/swappable.go`,
   `internal/api/models.go`, routes, client `ListModels`/`SetModel`, joe-core
   wiring. No CLI behavior change; `/tasks` + Web UI unaffected. Tests: swap under
   concurrent reads; list/current; bad-model 4xx; missing-API-key rejection.
3. **SSE streaming endpoint (core+shared tools only, no callback yet)** —
   `internal/api/tasks_stream.go`, shared setup helper extracted from `tasks.go`,
   server-side history preload by `session_id`, client SSE consumer. Tests: SSE
   framing/event types; multi-turn continuity; existing `/tasks` unchanged
   (golden contract test).
4. **Local-tool callback path** — delegating executor, `local_tool_call` events,
   `tool-results` endpoint, per-task registry. Tests: delegated tool round-trip;
   `ask_user` round-trip; single-goroutine assertion (no concurrent loop);
   error propagation from a failed local tool.
5. **Thin CLI** — rewire `internal/repl` to the stream; `/model` via API; service
   local-tool callbacks against a local-only registry; **remove** `deps.newAdapter`,
   `adapterFactory`, `useragent.NewAgent` from `cmd/joe/main.go`. Tests: thin REPL
   renders streamed steps; services a callback; `/model` end-to-end against a fake
   core; assert `cmd/joe` constructs no adapter and no `useragent.Agent`.
6. **End-to-end + guard** — integration test: CLI (thin) ↔ joe-core streams a
   tool-using conversation end-to-end (one core tool + one delegated local tool);
   plus a structural guard test asserting no LLM adapter / loop instantiation
   remains under `cmd/joe/` and `internal/useragent/` (or its relocation target).
7. **Relocate loop + docs (Option A)** — move loop out of `internal/useragent/`;
   update `tasks.go`/tests; write DECISIONS.md D-0003 and mark Phase 2 complete in
   the Plan-of-Record. (If Option B is chosen, this commit only does the DECISIONS
   /Plan-of-Record updates plus deleting now-dead CLI-only code.)

DECISIONS.md D-0003 may instead land with commit 3 (when the protocol first
appears) if you prefer the record to precede the dependent code; flagged for
review.

---

## 9. Acceptance-criteria mapping

| Criterion | Where satisfied |
|---|---|
| No adapter/loop in `internal/useragent/` or `cmd/joe/` | Commits 5 (remove instantiation) + 6 (guard) + 7 (relocate, Option A) |
| CLI starts, connects, streams a tool-using conversation E2E | Commits 3–6; E2E test in 6 |
| `/model` works | Commit 2 (API) + 5 (CLI rewire); E2E in 5 |
| `joe mcp serve` still works | Unaffected (uses category endpoints, not the loop); regression test stays green |
| oasisctl still works via existing HTTP API | `/api/v1/tasks` contract untouched; golden test in commit 3 |
| All existing tests pass; new tests cover streaming | Every commit; new tests in 2–6 |
| DECISIONS.md updated (protocol + tool boundary) | Commit 3 or 7 (D-0003) |
| Plan-of-Record marks Phase 2 complete | Commit 7 |

---

## 10. Risks / watch-items

- **SSE through the middleware chain** — verify `http.Flusher` passthrough
  (§3). Isolated fix if needed.
- **Suspended-loop vs single-loop invariant** — the delegating executor must
  block the caller goroutine, never spawn one; asserted by test (§3, commit 4).
- **Local-tool security boundary** — only local + shared tools are serviceable by
  the CLI; the CLI must refuse to "service" any tool name it didn't advertise in
  `client_tools`, and joe-core must only delegate advertised names. Both sides
  guard the boundary.
- **Cancellation** — Ctrl-C / context cancel mid-turn must tear down the SSE
  stream and unblock any suspended `local_tool_call` wait (close the result
  channel with a cancellation error) so the loop exits cleanly.
- **`/model` global semantics** — documented in §4; confirm this is acceptable
  vs a per-session override (which is out of scope and in tension with the single-
  adapter invariant).
- **Scope discipline** — Phase 2 collapses the loops; it does not adopt the
  Phase-1 durable run model for the REPL transport (§5), does not touch RBAC, and
  does not build token visibility (§2b).
