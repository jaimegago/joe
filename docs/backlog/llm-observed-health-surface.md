# LLM observed-health surface — last-successful-call display and failure recording

Status: open

## User story

As an operator, the LLM Settings page should show whether the currently selected
model is actually working — not just whether a credential is configured —
distinguishing "key present" from "requests silently failing" without opening
Chat and triggering a real call.

## Problem

The Providers tab shows Key present / Absent, which only answers whether a
credential is configured. A key can be present while every request 401s, hits a
quota wall, or the provider is down. No honest signal in the admin surface
reflects observed health of the active model.

## Decision (to be ratified as a DECISIONS.md entry when this item builds, not now)

A live/synthetic connectivity probe is explicitly **rejected**. Reasons:

1. **Stale on arrival** — a probe reflects its own moment, not the next real
   request.
2. **It costs quota and money to render a settings page**, ironic on the
   cost-enforcement subsystem, and page-load probing or polling both burn budget.
3. **A green Connected dot conflates four distinct failure modes** (auth valid,
   network reachable, model available, quota remaining) into one misleading
   signal.

Instead, report **observed reality derived from real calls Joe already makes**.

## Scope, Part 1 — last successful call (cheap, reads existing data)

Derive and display, per active model, the timestamp of the most recent
successful LLM call from existing `llm_usage` records; render as relative time
with absolute timestamp on hover/title; explicit empty state when no successful
call has ever been recorded for that model.

## Scope, Part 2 — failure recording (instrumentation extension, optional follow-up)

`llm_usage` currently records successful calls only, with no failure/error
column. Record failed LLM calls with an error classification (auth, quota,
network, model, other) and surface a recent-failure indicator on the LLM
Settings page derived from those records. Part 1 ships independently; Part 2 is
the only thing that enables detecting the present-key-failing-requests case.

## Acceptance criteria for the eventual build

- LLM Settings shows last-successful-call time for the active model sourced from
  `llm_usage` with no provider call made on page load.
- No new outbound LLM request is triggered by viewing the page, verifiable by no
  new `llm_usage` row appearing solely from opening LLM Settings.
- The never-called state renders explicitly rather than implying health.
- If Part 2 is included, failed calls produce a recorded row with an error class
  and the page reflects the most recent failure for the active model.

## Non-goals

- No live or synthetic connectivity probe or polling.
- No aggregated uptime percentage or SLA-style health scoring.
- No per-provider health-endpoint integration.

## Cross-reference

`docs/backlog/governed-connectivity-check-surface.md` covers component
connectivity checks — a governed, operator-initiated probe surface for
components. That is a different surface with different economics; the probe
rejection here applies to the LLM provider path specifically. Any future work on
either item must not contradict the other's probe posture.

## Notes

Beyond locked Stream G launch scope; same category as the deferred
visual-rhyme-with-Claude-usage-UI polish. Target post-launch unless a concrete
silent-failure incident surfaces the need earlier.
