Chat session controls — compression, an accurate usage indicator, effort, and model selection
Status: open
Priority: later

Four controls on a chat session, raised together in the maintainer's notes (joe-pm
notes entry `p5z2v.13`), under "Future":

- **Compression** — compact a long conversation so it can continue inside the
  model's context window.
- **An accurate usage indicator.** The indicator should show how much of the
  context window the session has used. **Likely blocked on
  `llmusage-tables-cover-two-models`**: joe knows the real window of exactly two
  models and treats every other as 100,000 tokens, so any indicator computed from
  that table is wrong for most models. That link is an inference made when this
  item was filed, not a measurement; confirm it against how the indicator is
  computed before building.
- **Effort** — a per-session reasoning-effort setting, where the provider has one.
- **Model selection** — choose the model per session rather than per install.

They are one item because they are controls on the same surface and share the
same dependency on knowing each model's real capabilities.
