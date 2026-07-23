# Capture the register-kubernetes guide's screenshots

Status: open
Priority: next

`docs/public/guides/register-kubernetes.md` was published without its screenshots.
The `quickstart-corrections` session (2026-07-23) neutralized the page — removed the
five inline "📷 Screenshot:" placeholder blockquotes and the "Screenshots to capture"
TODO section — so the published page reads as complete prose with no visible
placeholder markers or broken image references. The screenshots themselves are the
remaining work, and must be captured against a live running `joe` binary + web UI,
which is an operator task, not something this session (or any docs-only session) can
produce.

## Screenshots needed

1. **Components page** — the **+ Register Component** button, with the column layout
   (type, zone, connection status, arming state) visible, corresponding to the guide's
   Step 1.
2. **Register dialog** — Type selector set to `kubernetes`, ID/Name fields filled,
   corresponding to Step 2.
3. **Zones page** — the unassigned-components panel with the Assign Zone dropdown
   open, corresponding to Step 3.
4. **Promote dialog** — the cluster-coordinates fields plus the authentication-method
   selector showing both static-bearer and Entra-exchange field sets, corresponding to
   Step 4.
5. **Component detail card** — after a successful Test Connection: status
   **connected**, arming **armed**, corresponding to Step 5.

## Re-adding them

Once captured, re-add inline screenshot references (image + short caption) at the
corresponding step in `docs/public/guides/register-kubernetes.md`, under
`images/guides/register-kubernetes/` (the path the removed placeholders already
named), and remove this backlog item.
