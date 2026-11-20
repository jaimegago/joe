# Thinking tokens are in neither count Joe records or prices

Status: open
Priority: next

For a model that thinks before it answers, Joe records and prices the
answer tokens only. The provider bills the thinking tokens at the output
price. This item states what was read and lists what a design session has to
settle. It settles nothing.

Read at `f9b5146` by joe-pm `threads/joe-model-rows.md`; filed by joe-pm
`threads/thinking-tokens-item.md`. Nothing here was measured on a run.

## What Joe records today

- **The Gemini adapter reads three counts and not the fourth.** Input tokens
  come from `PromptTokenCount`, output tokens from `CandidatesTokenCount`,
  the total from `TotalTokenCount` (`internal/llm/gemini/gemini.go:316-318`).
  `ThoughtsTokenCount` is never read. The SDK documents the total as the sum
  of prompt, candidates, tool-use prompt and thoughts
  (`google.golang.org/genai` v1.70.0, `types.go:3668-3670`), so the
  candidates count leaves thinking out.
- **There is nowhere to put it.** `llm.TokenUsage` has input, output, total
  and the two cache counts (`internal/llm/adapter.go:176-199`). The
  `llm_usage` table has `input_tokens` and `output_tokens` and no total
  (`internal/store/migrations/017_llm_instrumentation.up.sql:65-66`).
- **Thought text is dropped, on purpose**, so reasoning never reaches the
  operator (`gemini.go:334-340`). That is about content, not counts.
- **Joe asks for no thinking setting.** The adapter sets the output cap and
  nothing about thinking (`gemini.go:95`, `:193-197`), so thinking runs at
  the model's default.
- **The Claude and OpenAI-compatible adapters** read the provider's output
  or completion count as it comes (`internal/llm/claude/claude.go:226-228`,
  `internal/llm/openaicompat/openaicompat.go:209-211`). Neither names
  thinking or reasoning anywhere.

## What that does

- **Recorded cost is low.** The recorder prices input and output tokens
  (`internal/llmusage/recorder.go:213`, `:233`). Thinking tokens are in
  neither, and Google's pricing page heads the output column "Output price
  (including thinking tokens)" (read 2026-11-20). The gap is the thinking
  tokens at the output price, the dearer of the two.
- **The cost limits see the low figure.** The hourly, daily and monthly gate
  sums recorded cost (`recorder.go:325-397`), so it fires later than the
  spend it names.
- **Reported token counts leave thinking out.** The task response's
  `total_tokens` carries the session's input and output sums
  (`internal/api/tasks.go:953-958`, `internal/agentloop/session.go:396-397`),
  and anything built from it does too: the OpenTelemetry output counter
  (`internal/llm/instrumented.go:196`, `:199`), and the token figures an
  evaluation's evidence file copies from the response.
- **One meter does count them.** The session token ceiling compares the
  provider's total (`session.go:398`, `internal/agentloop/agent.go:257`), and
  an Auto Joe run's token ceiling is that same check
  (`internal/api/autorun.go:178`). So a run can stop on tokens that appear
  in no count Joe reports.

## Not read, not measured

- **How many thinking tokens any model spends.** No run was made. The size
  of the gap is unknown for every model.
- **Whether the output cap Joe sends bounds thinking.** Every row caps
  output at 4,096 (`internal/llmusage/capabilities.go`), sent as
  `MaxOutputTokens`. Whether a thinking model counts its thinking against
  that cap, and what it does when thinking alone would exceed it, was not
  read.
- **Whether `CandidatesTokenCount` excludes thinking in practice.** The
  reading above rests on the SDK's comment, not on a response.
- **What the Anthropic and OpenAI-compatible counts include** when those
  models reason. Not read for either.
- **`ToolUsePromptTokenCount`**, which the SDK also sums into the total and
  Joe also never reads.
- **`llm_usage` rows already written.** Whether `gemini-2.5-flash` thinks
  by default was not read, so whether its rows are low, and by how much,
  is not known.

## What a design session has to settle

1. **Its own recorded field, or part of output?** A field needs a place in
   `llm.TokenUsage` and a column in `llm_usage`. The cache counts set a
   precedent for "nil when the provider does not report it"
   (`adapter.go:180-199`).
2. **Every provider, or Gemini alone?** And what the field means for a
   provider whose output count may already include reasoning.
3. **What price applies.** The output price, by the vendor's wording; and
   whether the price table says so or the recorder assumes it.
4. **What a reader of an evidence file is shown.** One output figure that
   includes thinking, two figures, or the answer tokens with thinking beside
   them. The task response is the source, so this is its shape.
5. **Whether the three meters should agree.** Today the ceiling counts
   thinking, cost does not, and reported counts do not.
6. **Rows already recorded.** Left as they are, marked, or corrected.
7. **The output cap**, once the unread fact above is known: whether 4,096
   is right for a model that thinks, and whether Joe should set a thinking
   budget.

## Neighbours

- [`price-rows-dated-changes`](price-rows-dated-changes.md): the rows this
  undercount is priced against, and what a price row cannot hold. A row that
  is right about the price still records a low cost while thinking is
  uncounted.
- [`llmusage-tables-cover-two-models`](llmusage-tables-cover-two-models.md):
  a model with no row records no cost at all, so for it this item changes
  nothing until it has one.
