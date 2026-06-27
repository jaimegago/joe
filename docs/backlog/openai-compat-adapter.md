Backlog — OpenAI-compatible adapter fast-follows
Status: in-progress

The generic `openai-compat` adapter (`internal/llm/openaicompat/`, D-0048) ships
non-streaming Chat + Embed against any OpenAI Chat Completions endpoint at a
configurable `base_url`, with an optional `OPENAI_API_KEY`. The following were
deliberately deferred and remain open:

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
