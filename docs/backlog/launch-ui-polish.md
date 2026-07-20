# Purpose-built dashboard to replace the retired fabricated-data landing page

Status: open
Priority: next
Session: launch-ui-polish
Decision: D-0038 (chat is the default landing surface; fabricated dashboard retired)

## Background

For launch, the web UI's default/index route (`/`) was changed to redirect to the
chat interface, and the prior landing/dashboard page was removed entirely — page,
route, sidebar nav entry, its sub-components (`MetricsCard`, `AlertsList`,
`RecentSessions`, `ComponentsHealth`), and the orphaned `ui/src/api/alerts.ts`
client. The page was retired because it surfaced data that does not exist in the
running system: the "Active Alerts" widgets read `GET /api/v1/alerts`, which is a
stub (`handleGetAlerts` in `internal/api/webui.go`) returning an empty list with a
`TODO` — no Alertmanager/Grafana aggregation is wired — so it misrepresented a
non-functional feature as real. The genuinely-wired widgets (components, recent
sessions) were already independently surfaced on the Components and Sessions
pages, so nothing real was lost.

A proper dashboard remains desirable; it was deferred, not abandoned. This item
tracks building one.

## Deferred work

- Design and build a purpose-built dashboard/overview surface that shows **only
  data backed by real, wired endpoints** — no stub or placeholder widgets. Any
  widget must fail visibly (or be omitted) when its backing feature is not
  implemented, rather than rendering an empty/zero state that reads as "healthy".
- Decide the alerts story before re-introducing any alerts widget: either
  implement real alert aggregation behind `GET /api/v1/alerts` (wire it to the
  Alertmanager/Grafana components via the graph edges, per the existing `TODO`)
  or omit alerts from the dashboard until that exists.
- Decide whether the dashboard becomes the default landing surface again or
  remains a secondary view reached from the sidebar, with chat staying the
  default (the launch posture per D-0038).
- Re-introduce a sidebar navigation entry for the new surface when it ships.

## Notes

- The removed components and their tests can be recovered from git history
  (commit on the `launch-ui-polish` slug) if useful as a starting point, but they
  should not be restored wholesale — the alerts widgets specifically must not
  return without a real backing.
