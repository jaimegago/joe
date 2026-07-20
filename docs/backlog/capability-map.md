Capability-map Concepts page — deferred follow-ups
Status: open
Priority: later

The public Concepts capability page shipped in session `capability-map` (D-0066): a
single explanation page mapping Joe's full capability surface, with observation mode
presented as an available posture rather than the boot default and the web_search
narrative moved out of Configuration (which now carries keys plus a cross-link). The
`docs-public-refit` session (D-0069) reworked that page's spine — it is no longer "every
capability is a Read"; the page is now `concepts/action-model.md`, titled "The action
model", built around the binary Read/Mutate classification and documenting the shipped
external-collaboration mutation surface. The read-surface inventory, web_search
placement, and observe-mode available-not-default stance survive that rework. The
following threads are deliberately deferred and keep this item open.

## Deferred items

- **joeagent.dev landing copy — "ships in observe mode" → "can run in observe mode."** The
  marketing landing copy in the separate `joeagent.dev` repository still states Joe *ships*
  in observe mode. That is the same false-default claim corrected here in `docs/public`,
  but it lives in a **different repository** and was not touched by this session. Soften it
  to "can run in observe mode" for consistency with the corrected default truth.

- **JOE_MODE-default wiring would upgrade the doc wording from "can run" to "by default."**
  The docs deliberately say Joe **can run** read-only via `JOE_MODE=observation`, because
  the write floor defaults DOWN today (D-0018 impl note; `ResolveWriteFloor` returns a
  down floor when neither panic nor `JOE_MODE=observation` is set). D-0019 (default
  observation) is still pending. If/when the boot default is changed to observation, the
  action-model page, the Overview page, and the observation-mode Concepts page should be
  upgraded from "can run read-only" to "boots read-only by default."

- **Document keyed search providers (Tavily, Brave) when added.** The action-model page and
  the Configuration keys currently describe SearXNG as the only implemented web-search
  backend. When keyed hosted providers land (tracked as an implementation item in
  `web-search-tool.md`), the action-model narrative and the Configuration `web_search`
  keys should be extended to name them. This is the documentation half of that deferred
  feature; the code half stays in `web-search-tool.md`.
