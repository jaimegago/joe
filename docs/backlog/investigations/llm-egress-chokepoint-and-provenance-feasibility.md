> **Status: OPEN** — live finding; feasibility-and-shape only, no work started.

```
# Investigation — LLM-egress chokepoint, OpenAI-compatible adapter, fail-closed egress, and sensitivity provenance

Date: 2026-06-09
Scope: feasibility-and-shape only. No design proposed, no code changed. Every
claim below was re-derived against the live tree at the cited file:line.

================================================================================
VERDICT (one paragraph per sub-question)
================================================================================

1. Single egress chokepoint — FEASIBLE-WITH-LOCALIZED-CHANGE.
   There is a single SEND seam by construction: every Chat caller holds the
   one llmusage.RecorderAdapter instance (directly, or via the SwappableAdapter
   that wraps it), and that recorder is the only thing standing in front of the
   one raw provider client. But "single seam" is a wiring convention guarded by
   one structural test, NOT a type-system guarantee — the factory
   (llmfactory.NewAdapter) is freely callable and would return an un-wrapped raw
   client. Also note: SEND is single; ASSEMBLY is not — seven callers each build
   their own llm.ChatRequest. So one send chokepoint exists today; making it
   bypass-proof is a localized hardening, not a refactor.

2. LLM adapter shape / OpenAI-compatible adapter — FEASIBLE-WITH-LOCALIZED-CHANGE.
   Two providers implemented (Claude native Anthropic SDK, Gemini native genai
   SDK). Both hardwire the endpoint to the vendor public API (neither SDK is
   given a base URL; both take only an API key from a fixed env var). The
   llm.LLMAdapter interface is protocol-agnostic (Chat/Embed over neutral
   structs) and would NOT need to widen — case (a). The cost lands entirely in
   the surrounding plumbing: config.ModelConfig carries only {Provider, Model}
   with no BaseURL/auth/headers field, and llmfactory.NewAdapter switches on
   provider name. Adding an OpenAI-compatible adapter = new impl behind the
   unchanged interface + widen ModelConfig + add a factory case + extend two
   key-presence/validation switches. Localized, additive.

3. Fail-closed-by-default — FEASIBLE-WITH-LOCALIZED-CHANGE.
   Today Joe is fail-OPEN-if-keyed: startup hard-fails only when NO provider key
   is present (config.AutoSelectProvider), but whenever a key exists Joe builds
   an adapter and will attempt a provider call. There is no "egress off unless a
   trusted endpoint + verifier are configured" notion anywhere. BUT provider
   selection and chain construction are already centralized
   (core.Services.BuildLLMChain is the SINGLE chain site; the recorder already
   runs a pre-call gate that can refuse before the inner call). So a fail-closed
   egress gate could be enforced at that one seam without dispersing logic.

4. Sensitivity/provenance through to assembly — REQUIRES-STRUCTURAL-REFACTOR
   (the hidden one, as flagged). A tool result enters context as BARE content:
   tools.ToolCallResult is {ID, Name, Result, Error} and ResultToMessage emits
   llm.Message{Role, Content, ToolResultID, ToolName, IsError}. Neither type has
   any provenance/zone/sensitivity field. The zone/component classification IS
   known at the executor (it drives the allow/deny scope check) but is discarded
   from the result — left behind in the RBAC layer exactly as hypothesized.
   Threading a sensitivity class to the assembly seam means changing the
   context-entry types (ToolCallResult AND llm.Message) and every producer of
   context entries (the executor, all seven ChatRequest assemblers, both
   provider adapters that re-marshal Message). This is the widest blast radius
   of the five.

5. Do the three flows converge — FEASIBLE-AS-IS (for the agentic path).
   For the agentic loop, (a) tool results, (b) user input, and (c) Joe's own
   state all converge into the single llm.ChatRequest assembled in
   agentloop.Agent.Run before the one Chat call — a verifier at that assembly
   point would see all three. Caveat: the six NON-agentic one-shot callers
   assemble their own ChatRequest and do NOT pass through the agentloop
   assembly; they DO still pass through the recorder SEND seam, so a verifier at
   the send seam sees every flow's raw payload (system + messages + tools), just
   without the agentloop's structure or any provenance tags.

================================================================================
SECTION 1 — The egress chokepoint
================================================================================

The provider interface and the outbound call:
  - llm.LLMAdapter interface: internal/llm/adapter.go:7-13 (Chat + Embed).
  - The actual outbound HTTP call is made inside the concrete provider clients:
      * Claude: internal/llm/claude/claude.go:131 — c.client.Messages.New(ctx, params)
        (client built at claude.go:55 via anthropic.NewClient(option.WithAPIKey(...))).
      * Gemini: internal/llm/gemini/gemini.go:177 — chat.SendMessage(ctx, lastParts...)
        (client built at gemini.go:65 via genai.NewClient(ctx, option.WithAPIKey(...))).

All Chat callers (non-test), found via grep on `.Chat(`:
  1. internal/agentloop/agent.go:237        a.llm.Chat        (agentic loop)
  2. internal/coreagent/joefile_service.go:154  s.llm.Chat    (Core Agent .joe synthesis)
  3. internal/observe/translator.go:36      t.llm.Chat        (NL→query translation)
  4. internal/knowledge/learning/extractor.go:107  e.llm.Chat (knowledge extraction)
  5. internal/knowledge/drafts/generator.go:135    g.llm.Chat (doc drafting)
  6. internal/review/agent.go:130           a.llm.Chat        (code-review agent)
  7. internal/api/sessiontitle.go:79        services.LLM.Chat (session title gen)
  Plus the decorator/forwarder layers:
     internal/llm/swappable.go:47           (SwappableAdapter → inner)
     internal/llmusage/recorder.go:199      (RecorderAdapter → inner, after gate)
     internal/llm/instrumented.go:156       (InstrumentedAdapter — see "dead" note)
     internal/observability/llm_middleware.go:116 (LLMMiddleware — see "dead" note)

What each caller's adapter actually is (the convergence proof), from the single
boot wiring site cmd/joe/server.go:
  - raw client built once:                  server.go:578 (deps.newLLMAdapter = llmfactory.NewAdapter, server.go:204)
  - wrapped in the recorder/gate exactly once: server.go:621
        llmAdapter = services.BuildLLMChain(llmAdapter, currentModelCfg)
    (BuildLLMChain wraps inner in llmusage.NewRecorderAdapter — internal/core/llmchain.go:52-74;
     the recorder wraps the RAW client directly, nothing in between.)
  - services.LLM = SwappableAdapter(recorder):  server.go:629  → used by the agentic
    loop (passed at internal/api/tasks.go:393) and the observe translator
    (internal/api/observe.go:77) and session-title gen (sessiontitle.go:79).
  - the SAME recorder instance (llmAdapter) handed BY NAME to the one-shot consumers:
        embedder:    server.go:636
        DocDrafter:  server.go:638  (→ generator.go Chat)
        ReviewAgent: server.go:650  (→ review/agent.go Chat)
        CoreAgent:   server.go:670  (→ coreagent joefile_service Chat)

Conclusion: there is exactly ONE RecorderAdapter instance and ONE raw client
instance at runtime; every Chat path terminates at RecorderAdapter.Chat
(recorder.go:194) → inner raw client. The recorder is therefore the de-facto
single SEND chokepoint. The model-swap HTTP handlers preserve this: both
internal/api/models.go:134 and internal/api/llmsettings.go:267 rebuild through
the same BuildLLMChain and install into the SwappableAdapter, so a hot swap
cannot produce an un-recorded adapter.

Caveats that keep this at FEASIBLE-WITH-LOCALIZED-CHANGE rather than
FEASIBLE-AS-IS:
  (i)  Bypass is not type-prevented. llmfactory.NewAdapter (factory.go:20) returns
       a bare raw client; any future caller could hold one and skip the recorder.
       The single-wrap property is asserted only by a structural test named in
       the boot comment, TestPhaseG2_LLMAdapterConstructorWrappedOnce
       (server.go:591-593), not by the compiler.
  (ii) "One seam" is about SEND, not ASSEMBLY. The payload (ChatRequest) is
       marshaled independently by all seven callers; there is no shared
       assembly function. A verifier wanting structured, pre-marshal context
       would need to sit at each assembler or inspect the assembled ChatRequest
       at the send seam.
  (iii) The OTel InstrumentedAdapter (internal/llm/instrumented.go:53) and
       observability.LLMMiddleware (internal/observability/llm_middleware.go:37)
       are defined but have NO live constructor call site (grep for
       `NewInstrumentedAdapter(` / `NewLLMMiddleware(` outside their own defs and
       tests returns nothing). They are not in the live egress chain; the live
       chain is exactly Swappable → Recorder → raw.

================================================================================
SECTION 2 — The LLM adapter shape
================================================================================

Interface (internal/llm/adapter.go:7-13):
    type LLMAdapter interface {
        Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
        Embed(ctx context.Context, text string) ([]float32, error)
    }
ChatRequest/ChatResponse/Message/ToolDefinition (adapter.go:16-73) are all
vendor-neutral plain structs — the interface carries no Anthropic- or
Gemini-specific shape.

Providers implemented: TWO.
  - Claude — native Anthropic SDK (github.com/anthropics/anthropic-sdk-go),
    internal/llm/claude/claude.go. Endpoint: HARDWIRED to the vendor default.
    Client construction passes ONLY option.WithAPIKey (claude.go:55); there is
    no option.WithBaseURL and no config plumbing for one. Key source is the
    fixed env var env.AnthropicAPIKey (claude.go:50).
  - Gemini — native Google genai SDK (github.com/google/generative-ai-go),
    internal/llm/gemini/gemini.go. Endpoint: HARDWIRED. Client construction
    passes ONLY option.WithAPIKey (gemini.go:65); key from env.GeminiAPIKey or
    env.GoogleAPIKey (gemini.go:52-55).

No provider speaks OpenAI-compatible chat-completions today. No adapter accepts
a configurable base URL or configurable auth/headers.

config.ModelConfig (internal/config/config.go:241-244):
    type ModelConfig struct {
        Provider string  // "claude", "gemini"
        Model    string
    }
No BaseURL, no AuthScheme, no Headers. The factory switches purely on Provider
(internal/llmfactory/factory.go:26-31), defaulting unknown→gemini after
ValidateAPIKeys rejects unknown providers (validation.go:94-110,
factory.go:22).

Assessment — which of (a)/(b)/(c):
  - The INTERFACE: case (a). Chat/Embed are protocol-agnostic; an
    OpenAI-compatible client implementing LLMAdapter drops in behind it with no
    interface change. (Compile-time conformance is already the pattern, e.g.
    `var _ llm.LLMAdapter = (*RecorderAdapter)(nil)` recorder.go:293.)
  - The surrounding plumbing: localized widening, NOT a re-centering:
      * add BaseURL + auth/headers fields to config.ModelConfig (config.go:241);
      * add an "openai"/"openai-compatible" case to llmfactory.NewAdapter
        (factory.go:26) and to HasProviderAPIKey (factory.go:48);
      * add the provider to config.ValidateAPIKeys (validation.go:94) and the
        AutoSelectProvider key map (validation.go:43-44) if it should
        participate in auto-selection;
      * the recorder's cost/identity table lookups (llmusage) tolerate unknown
        provider/model already (recorder.go:248-256 records zero-cost + warns),
        so no hard dependency there.
  Net: (a) for the abstraction; a contained, additive change for config + factory.
  No existing caller or the LLMAdapter contract has to move.

================================================================================
SECTION 3 — Fail-closed-by-default feasibility
================================================================================

What governs whether a provider call happens at all, today:
  - Startup provider/key resolution: config.AutoSelectProvider
    (internal/config/validation.go:38-70). It HARD-FAILS at boot only when
    NEITHER key is present (validation.go:46-48). If exactly one key is present
    it selects that provider; if both, it keeps Claude. There is no notion of a
    "trusted endpoint" or "verifier configured" precondition.
  - Per-model key gate: config.ValidateAPIKeys (validation.go:94-110), invoked by
    the factory before client construction (factory.go:22). This only checks
    that the env key exists — presence, not trust.
  - Boot adapter build: cmd/joe/server.go:578-629. Once a key exists, Joe ALWAYS
    builds an adapter and proceeds to serve; nothing here is conditioned on an
    egress policy.
  - There IS one precedent for refusing a call at the seam: the recorder's
    pre-call cost-window gate returns a typed error BEFORE invoking the inner
    adapter (internal/llmusage/recorder.go:196-198 and .gate at recorder.go:325-397),
    refusing with no tokens consumed.

Where selection + key resolution live: config.AutoSelectProvider /
ValidateAPIKeys (centralized in internal/config) + llmfactory.NewAdapter +
the single BuildLLMChain site (internal/core/llmchain.go:52). Provider-calling
is NOT dispersed — it funnels through BuildLLMChain → recorder → raw.

Feasibility: a fail-closed default ("no trusted endpoint AND no verifier ⇒ no
external call") COULD be enforced at the section-1 send seam, because:
  - the recorder already demonstrates a pre-inner-call refusal pattern that
    every Chat path is forced through (recorder.go:196-204);
  - chain construction is single-site (BuildLLMChain), so the policy object can
    be threaded once the way Limits/Audit already are (llmchain.go:62-69);
  - the boot path already has a clean "refuse to proceed" exit
    (server.go:579-582 returns non-zero on adapter init failure), so a
    fail-closed boot posture has somewhere to live too.
Caveat: today's default is the opposite (fail-open-if-keyed), so this is a
behavioral inversion to ADD, but it is a localized addition at one seam, not a
de-dispersal effort.

================================================================================
SECTION 4 — Context assembly and sensitivity provenance  (the likely hidden one)
================================================================================

The context-entry types, end to end, carry NO provenance/sensitivity:

  - Tool execution result type — internal/tools/executor.go:388-393:
        type ToolCallResult struct { ID string; Name string; Result any; Error error }
    No component_id, no zone, no sensitivity class.

  - Result → context message — internal/tools/executor.go:355-378 (ResultToMessage):
    marshals Result to JSON into Content and returns
        llm.Message{ Role:"user", Content:..., ToolResultID:..., ToolName:..., IsError:... }
    The classification is dropped here; only id/name/error survive.

  - The message type itself — internal/llm/adapter.go:31-38 (llm.Message):
        Role, Content, ToolCalls, ToolResultID, ToolName, IsError
    No field that could carry a sensitivity/provenance class.

The classification EXISTS at execution time but is used only for allow/deny and
then discarded:
  - The executor holds component→zone and namespace→zone maps and the allowed
    sets: internal/tools/executor.go:35-39 (allowedComponents, sourceZoneMap,
    namespaceZoneMap, scopeZoneNames).
  - It reads the target component_id / namespace from the tool args to make the
    scope decision (e.g. namespace check at executor.go:237-259; the component
    scope check immediately above it), producing a deny ERROR on violation — but
    on the ALLOW path the zone/component is never attached to the returned
    ToolCallResult (executor.go:280-301, 320-325). So provenance is "left behind
    in the RBAC/zone layer," confirming the prompt's hypothesis.

Refactor distance for "context entries carry their sensitivity class through to
the assembly seam": WIDE. Minimum touch set:
  1. add a sensitivity/provenance field to tools.ToolCallResult (executor.go:388)
     and populate it where the executor already knows component/zone
     (executor.go:35-39 + the per-call scope logic);
  2. add a matching field to llm.Message (adapter.go:31) — the shared
     context-entry type — and carry it through ResultToMessage (executor.go:355);
  3. every producer of context entries must set/preserve it:
       - the agentic assembler agentloop.Agent.Run (user msg agent.go:201,
         assistant msg agent.go:266/292, tool results via
         executor.ResultsToMessages agent.go:355);
       - the six one-shot ChatRequest assemblers (section 1 list);
  4. both provider adapters re-marshal llm.Message into vendor structs
     (claude.go:70-92, gemini.go:118-165) and would need to preserve/ignore the
     new field deliberately (and a verifier reading at the send seam would read
     it off ChatRequest before this point).
Because llm.Message and ToolCallResult are the two universal carriers and they
are produced in ~8 places and consumed by 2 adapters, this is the broadest
change of the five and the one most likely to be under-scoped.

================================================================================
SECTION 5 — The three egress flows: do they converge?
================================================================================

For the AGENTIC path (the primary chat/task path), all three converge into the
single llm.ChatRequest built once per iteration in agentloop.Agent.Run
(internal/agentloop/agent.go:219-224):
    req := llm.ChatRequest{ SystemPrompt: a.systemPrompt, Messages: session.Messages,
                            Tools: toolDefs, MaxTokens: a.maxOutputTokens }
then sent at agent.go:237 (a.llm.Chat).

  (a) Tool / observed-data results — enter via
      executor.ResultsToMessages (agent.go:355) → session.AddMessages
      (agent.go:362) → session.Messages → ChatRequest.Messages.
  (b) User input prompt — enters via session.AddMessage at agent.go:201
      (Role:"user") → session.Messages → ChatRequest.Messages.
  (c) Joe's own state pulled into context:
      - graph summary, zone scope, write-floor posture, and skills are
        concatenated into the system prompt in buildTaskRun
        (internal/api/tasks.go:327-350; graph summary specifically at
        tasks.go:340-346) → passed to NewAgent (tasks.go:396) → a.systemPrompt →
        ChatRequest.SystemPrompt (agent.go:220).
      - prior turns live in session.Messages (the same slice as (a)/(b)), so
        they too enter via ChatRequest.Messages. (Note: knowledge-base and graph
        DETAIL are pulled in-band as TOOL results — flow (a) — not pre-injected;
        only the graph summary counts are pre-injected into the system prompt.)

So a verifier placed at the agentic ASSEMBLY seam (agent.go:219) sees all three
flows together, with the system-prompt-borne state (c) and the message-borne
flows (a)/(b) distinguishable by ChatRequest field.

For the NON-agentic one-shot callers (review/agent.go:130, drafts/generator.go:135,
knowledge/learning/extractor.go:107, coreagent/joefile_service.go:154,
api/sessiontitle.go:79, observe/translator.go:36): each constructs its OWN
ChatRequest inline and does NOT route through the agentloop assembler. They
therefore BYPASS the agentic assembly seam — but they still reach the single
SEND seam (the recorder), so a verifier at the SEND seam (recorder.go:194) would
still observe their full assembled payload (SystemPrompt + Messages + Tools),
just without agentloop structuring and without any provenance tags (section 4).

Conclusion: a verifier at the SEND seam sees 100% of flows for 100% of callers
(raw payload). A verifier at the agentloop ASSEMBLY seam sees all three flows
but only for the agentic path. Neither location sees a sensitivity class today,
because none is carried (section 4).

================================================================================
REFACTOR-DISTANCE SUMMARY (cheapest → most expensive to evolve)
================================================================================

1. (cheapest) Single send chokepoint, bypass-proof — Section 1.
   The chokepoint already exists at one RecorderAdapter instance; hardening is
   making the recorder the only constructible path (e.g. funnel/forbid bare
   llmfactory.NewAdapter use) and is already partly enforced by a structural
   test. Localized.

2. Fail-closed-by-default egress — Section 3.
   Centralized selection + a single chain site + an existing pre-call refusal
   pattern (cost gate) mean a fail-closed policy is an ADD at one seam. Behavior
   inversion, contained blast radius.

3. OpenAI-compatible adapter w/ configurable base URL + auth/headers — Section 2.
   Interface unchanged (case a). Cost is additive plumbing: widen ModelConfig,
   add a factory case, extend two validation/key switches, write one new
   LLMAdapter impl.

4. A verifier seeing all three flows — Section 5.
   For the agentic path it is feasible as-is at the assembly seam, and feasible
   for ALL callers at the send seam. The only real work is choosing the seam and
   the fact that one-shot callers bypass the agentic assembler — no type changes
   needed to merely OBSERVE the flows.

5. (most expensive) Sensitivity/provenance carried to the assembly seam —
   Section 4. Requires changing the two universal context-entry types
   (tools.ToolCallResult and llm.Message), populating provenance where the
   executor already knows it, and updating ~8 producers + 2 adapters. Widest
   blast radius; the hidden refactor.

================================================================================
APPENDIX — primary citations
================================================================================
internal/llm/adapter.go:7-13,16-73           LLMAdapter interface + neutral payload structs
internal/llm/claude/claude.go:50,55,131       Claude key env, client (no base URL), outbound call
internal/llm/gemini/gemini.go:52-55,65,177     Gemini key env, client (no base URL), outbound call
internal/llm/swappable.go:26-62               SwappableAdapter (agentic + chat contact point)
internal/llm/instrumented.go:53               InstrumentedAdapter (defined, NOT wired live)
internal/observability/llm_middleware.go:37    LLMMiddleware (defined, NOT wired live)
internal/llmusage/recorder.go:194-205,196-198,293  Recorder = single send seam + pre-call gate
internal/core/llmchain.go:52-74               BuildLLMChain = single chain construction site
internal/llmfactory/factory.go:20-32,48-57     factory switch on provider; key-presence map
internal/config/config.go:241-244             ModelConfig {Provider, Model} only
internal/config/validation.go:38-70,94-110     AutoSelectProvider, ValidateAPIKeys
cmd/joe/server.go:578,621,629,636,638,650,670  boot wiring: raw→recorder→swappable + by-name consumers
internal/api/models.go:134 / llmsettings.go:267  model-swap handlers reuse BuildLLMChain
internal/api/tasks.go:327-350,393,396         system-prompt assembly (state) + agent wiring
internal/agentloop/agent.go:201,219-224,237,266,292,355,362  agentic assembly + single Chat
internal/tools/executor.go:35-39,237-259,355-378,388-393  zone maps, scope check, result→message, result type
```
