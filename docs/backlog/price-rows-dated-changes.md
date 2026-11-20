# Two price rows go stale on a date the table cannot hold

Status: open
Priority: next

`internal/llmusage/cost.go` carries price rows for `gemini-3.8-flash` and
`gemini-4-carbon`, added 2026-11-20 at the price in force that day. Google's
pricing page quoted a second price for each, with the date it starts:

| Model | Row, per million input / output tokens | Changes on | To |
|---|---|---|---|
| `gemini-4-carbon` | $2.00 / $10.00 | 2026-12-01 | $4.00 / $20.00 |
| `gemini-3.8-flash` | $0.75 / $3.75 | 2027-01-01 | $1.50 / $7.50 |

Source: `https://ai.google.dev/gemini-api/docs/pricing`, read 2026-11-20. Each
row's comment carries the same figures and the date.

From each date the row records half the real cost. The cost-window gate sums
recorded cost (`internal/llmusage/recorder.go`, `gate`), so the hourly, daily
and monthly limits then fire at twice the spend they name.

## What has to happen

On or after each date, re-read the pricing page and update the row and the
literal in its test (`TestNewCostTable_BuiltinGemini4Carbon`,
`TestNewCostTable_BuiltinGemini38Flash`, `internal/llmusage/cost_test.go`).
Nothing prompts this. The date is in a comment.

## The limit underneath

A price row is one input price, one output price and a currency
(`PerTokenPrice`, `internal/llmusage/cost.go`). It cannot hold:

- **a dated change**, which is why these two rows are right only until a day
  written in a comment;
- **a prompt-size tier.** `gemini-3.1-pro-preview` was quoted on the same page
  at one price for prompts up to 200,000 tokens and another above. A row for
  it would be wrong for one of the two.

Whether the table should learn either, or whether a row should carry an
expiry that makes a stale price loud, is open.
`docs/backlog/llmusage-tables-cover-two-models.md` holds the neighbouring
question of what a model with no row should get.

Filed by joe-pm `threads/joe-model-rows.md`.
