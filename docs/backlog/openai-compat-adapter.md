Backlog — OpenAI-compatible adapter fast-follows
Status: in-progress

The generic `openai-compat` adapter (`internal/llm/openaicompat/`, D-0048) ships
non-streaming Chat + Embed against any OpenAI Chat Completions endpoint at a
configurable `base_url`, with an optional `OPENAI_API_KEY`.

**Live-verified (2026-06-27, session `openai-compat-adapter-01`).** The adapter is
now live-verified against Google's OpenAI-compatible endpoint
(`https://generativelanguage.googleapis.com/v1beta/openai`, model
`gemini-2.5-flash`) for both **chat** (non-empty content + non-zero token usage)
and **tool_calls** (a `get_weather` tool round-trips: the model emits a
`tool_calls` entry and the adapter parses it back into a `ToolCall` with the
expected name and decoded args). Verification has two layers: a key-gated live
integration test (`internal/llm/openaicompat/openaicompat_live_test.go`, skips
unless `OPENAI_API_KEY` is set) and a full-binary boot — `joe` started with
provider `openai-compat` + the Gemini compat `base_url` answered a chat request
through `POST /api/v1/tasks` end to end. Because the adapter reads its key solely
from `OPENAI_API_KEY`, driving it against Gemini means placing the Gemini key in
`OPENAI_API_KEY`; no Gemini-specific key reading was added to this code path.

The following were deliberately deferred and remain open:

- **Streaming.** The adapter is non-streaming only: it POSTs `/v1/chat/completions`
  and reads a single JSON body. SSE token streaming (`stream: true`) is not
  implemented; the live agent loop consumes the full response. Add streaming when
  the transport surface needs incremental tokens from openai-compat endpoints.

- **Azure-style auth/url variant.** Only the plain OpenAI shape is supported:
  base URL + optional `Authorization: Bearer` header. Azure OpenAI uses a
  different URL layout (`/openai/deployments/{deployment}/chat/completions?api-version=...`)
  and an `api-key` header rather than a Bearer token. Supporting Azure (and
  similar gateway shapes) needs a per-endpoint auth/url strategy, not the single
  generic path shipped here.

- **Embeddings capability negotiation beyond a clear error.** `Embed` posts
  `/v1/embeddings` and returns a clear, actionable error when the endpoint does
  not support embeddings (404 / 501). It does not probe capabilities, fall back
  to an alternate embedding endpoint, or auto-disable embedding-backed features —
  the operator must point `embedding_model` at an embeddings-capable endpoint.
  Richer negotiation (capability discovery, graceful degradation) is deferred.

- **Hardening against stricter generic servers.** Live verification covered
  Gemini's compat endpoint, which is lenient about request shape. Stricter
  generic servers (vLLM, Ollama's `/v1`, and similar) can reject requests
  carrying unexpected or non-applicable fields. Auditing the emitted request
  shape against those servers and trimming/guarding fields they reject — so the
  single generic path stays portable beyond the lenient hosted endpoints — remains
  deferred.
