# Joe Web UI

Browser-based dashboard for Joe. Built with React 18, Vite, Tailwind CSS, and shadcn/ui.

## Setup

```bash
npm install
npm run dev
```

Opens at `http://localhost:5173`. Requires joe-core running on `:7777`.

Or from the repo root:

```bash
make run-stack   # starts joe-core + UI together
make run-ui      # starts UI only
```

## Pages

- **Dashboard** -- source health, active alerts, recent sessions
- **Graph** -- interactive infrastructure graph (React Flow)
- **Admin** -- RBAC zones, policies, source-zone assignments
- **Chat** -- conversational interface with tool call display

## Scripts

```bash
npm run dev       # dev server with HMR
npm run build     # production build to dist/
npm run lint      # ESLint (recommendedTypeChecked + Prettier)
npm run test      # Vitest + Testing Library
```

## Architecture

See [docs/web-ui.md](../docs/web-ui.md) for the full specification.

Key directories:

- `src/api/` -- ApiClient, typed fetch functions, Zod schemas
- `src/components/layout/` -- AppShell, Sidebar, Header
- `src/components/graph/` -- InfraGraph (React Flow), NodeDetails
- `src/components/dashboard/` -- MetricsCard, SourcesHealth, AlertsList
- `src/components/admin/` -- ZonesTable, PoliciesTable, zone/policy forms
- `src/components/chat/` -- ChatWindow, MessageList, ChatInput
