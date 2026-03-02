# Web UI Implementation Plan (Phase 12)

Executable step-by-step plan. Each step ends with a build/lint/run check.
Reference spec: `docs/web-ui.md`

---

## Step 1 — Scaffold Vite + React + TypeScript

**Goal:** Empty working app with correct toolchain.

```bash
cd /Users/jaimegago/joe
npm create vite@latest ui -- --template react-ts
cd ui
npm install
```

Verify:
```bash
npm run dev   # opens on localhost:5173
```

---

## Step 2 — Install Core Dependencies

```bash
cd ui
npm install \
  react-router-dom@6 \
  @tanstack/react-query@5 \
  reactflow@11 \
  recharts@2 \
  tailwindcss@3 postcss autoprefixer \
  clsx tailwind-merge \
  lucide-react
```

Init Tailwind:
```bash
npx tailwindcss init -p
```

Update `tailwind.config.js`:
```js
content: ["./index.html", "./src/**/*.{ts,tsx}"]
```

Add to `src/index.css`:
```css
@tailwind base;
@tailwind components;
@tailwind utilities;
```

Verify: `npm run build` — no errors.

---

## Step 3 — Add shadcn/ui

```bash
cd ui
npx shadcn-ui@latest init
# Answers: TypeScript=yes, style=Default, base color=Slate, CSS variables=yes, aliases=yes
npx shadcn-ui@latest add button card dialog input select table tabs badge separator
```

Verify: `src/components/ui/` directory exists with generated files.

---

## Step 4 — Configure tsconfig Paths

In `tsconfig.json`, add:
```json
"baseUrl": ".",
"paths": { "@/*": ["./src/*"] }
```

In `vite.config.ts`:
```ts
import path from 'path'
// inside defineConfig:
resolve: { alias: { "@": path.resolve(__dirname, "./src") } }
```

Install type for path resolution:
```bash
npm install -D @types/node
```

Verify: `npm run build` — no errors.

---

## Step 5 — API Client Layer

Create these files exactly as spec'd in `docs/web-ui.md`:

- `src/api/client.ts` — `ApiClient` class + singleton `apiClient`
- `src/api/types.ts` — all TypeScript interfaces (`GraphNode`, `GraphEdge`, `Graph`, `Source`, `SecurityZone`, `SourceZoneAssignment`, `RbacPolicy`, `ChatMessage`, `ToolCall`, `Session`, `Alert`)
- `src/api/graph.ts` — functions: `fetchGraph()`, `fetchNode(id)`, `fetchRelated(id, depth)`
- `src/api/sources.ts` — functions: `fetchSources()`, `fetchSource(id)`, `testSource(id)`, `deleteSource(id)`
- `src/api/security.ts` — functions: `fetchZones()`, `createZone()`, `updateZone()`, `deleteZone()`, `fetchSourceZones()`, `fetchUnassigned()`, `assignZone()`, `removeZone()`, `fetchPolicies()`, `createPolicy()`, `deletePolicy()`
- `src/api/chat.ts` — functions: `sendMessage(sessionId, content)`, `fetchMessages(sessionId)`, `createSession()`
- `src/lib/utils.ts` — `cn()` helper using `clsx` + `tailwind-merge`
- `src/lib/constants.ts` — node kind → color/icon map, edge type → style map, status → color map
- `.env.example` — `VITE_API_URL=http://localhost:7777`

Verify: `npm run build` — no TypeScript errors.

---

## Step 6 — App Shell + Routing

Files to create:

**`src/components/layout/Sidebar.tsx`**
- Nav links: Dashboard (`/`), Graph (`/graph`), Sources (`/sources`), Chat (`/chat`), Admin (`/admin`)
- Use `NavLink` from react-router-dom for active styling
- Icons from lucide-react

**`src/components/layout/Header.tsx`**
- Page title (passed as prop)
- "Joe" logo/wordmark on left

**`src/components/layout/AppShell.tsx`**
- Sidebar (fixed left, 240px) + main content area
- Wraps `<Outlet />` from react-router

**`src/components/layout/PageContainer.tsx`**
- Padding wrapper for page content

**`src/App.tsx`**
```tsx
<QueryClientProvider client={queryClient}>
  <BrowserRouter>
    <Routes>
      <Route path="/" element={<AppShell />}>
        <Route index element={<DashboardPage />} />
        <Route path="graph" element={<GraphPage />} />
        <Route path="sources" element={<SourcesPage />} />
        <Route path="chat" element={<ChatPage />} />
        <Route path="chat/:sessionId" element={<ChatPage />} />
        <Route path="admin" element={<AdminPage />} />
      </Route>
    </Routes>
  </BrowserRouter>
</QueryClientProvider>
```

**`src/pages/`** — create stub pages for each route (just renders title):
`DashboardPage.tsx`, `GraphPage.tsx`, `SourcesPage.tsx`, `ChatPage.tsx`, `AdminPage.tsx`

**`src/main.tsx`** — mount `<App />`

Verify: `npm run dev` — all routes render without crash.

---

## Step 7 — Common Components

**`src/components/common/LoadingSpinner.tsx`** — animated spinner, accepts `size` prop
**`src/components/common/ErrorBoundary.tsx`** — class component, shows error message + retry
**`src/components/common/EmptyState.tsx`** — icon + message + optional action button
**`src/components/common/ConfirmDialog.tsx`** — shadcn Dialog with title, description, Confirm/Cancel

Verify: `npm run build` — no errors.

---

## Step 8 — Dashboard Page

**Hooks:**
- `src/hooks/useSources.ts` — `useQuery` wrapping `fetchSources()`, 30s refetch interval
- (alerts + sessions hooks inline in DashboardPage for now)

**Components:**
- `src/components/dashboard/MetricsCard.tsx` — shadcn Card with title, value, sub-label, optional color indicator
- `src/components/dashboard/SourcesHealth.tsx` — grid of source dots (color = status), name label
- `src/components/dashboard/AlertsList.tsx` — list of `Alert` items with severity badge
- `src/components/dashboard/RecentSessions.tsx` — list of `Session` items with relative timestamp

**`src/pages/DashboardPage.tsx`**
- 3 MetricsCards: Sources (connected/error counts), Alerts (active/critical), Sessions (today count)
- AlertsList + RecentSessions side by side
- SourcesHealth grid below
- `useQuery` for `/api/v1/sources`, `/api/v1/alerts`, `/api/v1/sessions?limit=5`
- Loading spinners while fetching
- Error states with retry

Verify: `npm run dev` — dashboard renders with loading/empty states.

---

## Step 9 — Graph Page

**Hooks:**
- `src/hooks/useGraph.ts` — `useQuery` wrapping `fetchGraph()`, `fetchNode()`, `fetchRelated()`

**Node components (in `src/components/graph/nodes/`):**
- `GenericNode.tsx` — base React Flow custom node, shows icon + name + namespace/cluster + status dot
- `ServiceNode.tsx`, `DatabaseNode.tsx`, `K8sNode.tsx` — extend GenericNode with kind-specific icon/color from `constants.ts`

**Edge components:**
- `src/components/graph/edges/DependencyEdge.tsx` — labeled edge, style varies by type from `constants.ts`

**Supporting components:**
- `src/components/graph/GraphLegend.tsx` — node type color legend
- `src/components/graph/GraphControls.tsx` — filter dropdowns (namespace, kind, status) + layout toggle
- `src/components/graph/NodeDetails.tsx` — right-side panel when node selected; shows all metadata fields + related nodes list

**`src/components/graph/InfraGraph.tsx`**
- Wrap React Flow `<ReactFlow>` with custom nodeTypes + edgeTypes
- Include `<MiniMap>`, `<Controls>`, `<Background>`
- On node click → set selected node → show NodeDetails
- Apply namespace/kind/status filters to nodes
- Include `src/lib/graph-layout.ts` with a simple dagre layout function

**`src/pages/GraphPage.tsx`**
- Header row: "Infrastructure Graph" + Refresh button + filter/layout controls
- `InfraGraph` takes full remaining height
- `NodeDetails` panel slides in from right when node selected
- `useGraph` hook for data

Verify: `npm run dev` — graph page renders React Flow canvas with controls; nodes appear when API has data.

---

## Step 10 — Sources Page

**`src/pages/SourcesPage.tsx`**
- Filter bar: type dropdown, zone dropdown, status dropdown
- shadcn Table: columns = Source, Type, Zone, Status, Actions
  - Status cell uses colored dot + text
  - Zone cell shows "⚠ unassigned" in amber for unassigned sources
  - Actions: "View" button
- Click row or "View" → open detail panel (right side or bottom)
- Detail panel: all source fields + "Test Connection" / "Remove" buttons
  - "Test Connection" calls `testSource(id)`, shows success/error toast
  - "Remove" shows ConfirmDialog then calls `deleteSource(id)`, invalidates query
- `useQuery` with `fetchSources()`

Verify: `npm run dev` — sources page renders table with filters.

---

## Step 11 — Admin Page (Zones, Sources, Policies)

**Hooks:**
- `src/hooks/useZones.ts` — fetch zones + unassigned sources
- `src/hooks/usePolicies.ts` — fetch policies

**Components:**

`src/components/admin/ZonesTable.tsx`
- shadcn Table: Zone, Actions (comma-joined), Sources count, Edit/Delete
- Edit → opens ZoneForm dialog
- Delete → ConfirmDialog → `deleteZone(id)` → invalidate

`src/components/admin/ZoneForm.tsx`
- shadcn Dialog with inputs: id (text), description (text), actions (multi-select checkboxes: Read, Query, Mutate, Delete)
- Submit calls `createZone` or `updateZone`

`src/components/admin/UnassignedSources.tsx`
- Amber warning banner if unassigned count > 0
- Lists each unassigned source with "Assign Zone" dropdown (zone options from zones query)
- On select → calls `assignZone(sourceId, zoneId)`

`src/components/admin/SourceZoneAssign.tsx`
- Table of all source-zone assignments
- Filter by zone
- Remove button → `removeZone(sourceId)`

`src/components/admin/PoliciesTable.tsx`
- Table: Principal, Zones (comma-joined), Delete
- Delete → ConfirmDialog → `deletePolicy(id)`

`src/components/admin/PolicyForm.tsx`
- Dialog: principal (text), zones (multi-select from zones list)
- Submit calls `createPolicy`

**`src/pages/AdminPage.tsx`**
- shadcn Tabs: "Zones" | "Sources" | "Policies"
- Zones tab: UnassignedSources banner + ZonesTable + "Create Zone" button
- Sources tab: SourceZoneAssign
- Policies tab: PoliciesTable + "Create Policy" button

Verify: `npm run dev` — admin page renders all 3 tabs with CRUD controls.

---

## Step 12 — Chat Page

**Hooks:**
- `src/hooks/useChat.ts` — session state, message list, send function, pending state

**Components:**

`src/components/chat/ToolCallDisplay.tsx`
- Collapsible row: `🔧 tool_name(args preview)` → expand to show full result
- Status badge: pending (spinner), success (green), error (red)

`src/components/chat/MessageBubble.tsx`
- User message: right-aligned, blue bubble, plain text
- Assistant message: left-aligned, gray bubble, markdown rendered (use a lightweight markdown lib or simple pre)
- ToolCall items shown inline under assistant message as ToolCallDisplay

`src/components/chat/MessageList.tsx`
- Scrollable list of MessageBubble
- Auto-scroll to bottom on new message
- Empty state: "Ask Joe anything about your infrastructure"

`src/components/chat/ChatInput.tsx`
- Textarea that grows with content (max 5 rows)
- Send button (disabled while pending)
- Enter sends (Shift+Enter = newline)

`src/components/chat/ChatWindow.tsx`
- MessageList fills available height
- ChatInput pinned to bottom
- Loading indicator when waiting for response

**`src/pages/ChatPage.tsx`**
- Header: "Chat with Joe" + "New Session" button
- Uses `useParams` for optional `sessionId`
- On mount: if sessionId → load history; else create new session
- Wire ChatWindow with useChat hook

Verify: `npm run dev` — chat page renders empty state + input; can type messages.

---

## Step 13 — Polish Pass

1. **Loading states** — every `useQuery` shows `<LoadingSpinner />` while `isLoading`
2. **Error states** — every `useQuery` shows error message + retry button while `isError`
3. **Empty states** — every list/table shows `<EmptyState />` when data is empty array
4. **Toast notifications** — add a toast library (e.g. `sonner`) for mutation feedback
5. **Responsive** — test sidebar collapse at < 768px (hamburger menu or auto-collapse)
6. **Page titles** — `document.title` updated per page
7. **Lazy loading** — wrap each page in `React.lazy()` + `<Suspense>` in App.tsx
8. **Auth guard** — `useAuth` hook checks localStorage token; redirect to `/login` on 401
9. **Login page** — simple form at `/login`, POST to `/api/v1/auth/login`, store token

Verify: `npm run build` — zero TypeScript errors, bundle < 2MB.

---

## Step 14 — joecored Integration

Check which API endpoints exist in joecored. Add any missing ones:

**Graph:**
- `GET /api/v1/graph` — returns `{ nodes: [], edges: [] }` (check `internal/api/`)
- `GET /api/v1/graph/node/{id}` — single node
- `GET /api/v1/graph/node/{id}/related` — subgraph

**Sources:**
- `GET /api/v1/sources` — list all sources with status
- `GET /api/v1/sources/{id}` — single source detail
- `POST /api/v1/sources/{id}/test` — trigger connection test

**Chat/Sessions:**
- `POST /api/v1/chat` — body `{ session_id, message }` → response `{ message, tool_calls }`
- `GET /api/v1/sessions` — session list
- `GET /api/v1/sessions/{id}/messages` — message history
- `POST /api/v1/sessions` — create session

**Alerts:**
- `GET /api/v1/alerts` — aggregate active alerts from graph

For each missing endpoint: add handler in `internal/api/`, wire in `cmd/joecored/main.go`.

Verify: `go build ./...` + `go test ./...` still pass after any Go changes.

---

## Step 15 — Final Verification

```bash
# Frontend
cd ui
npm run build     # zero errors
npm run preview   # manual smoke test all pages

# Backend
cd ..
go build ./...
go test ./...
go vet ./...
gofmt -s -w .
```

Run both together:
```bash
# Terminal 1
joecored

# Terminal 2
cd ui && npm run dev

# Open http://localhost:5173
# Verify: Dashboard loads, Graph renders, Sources list, Admin tabs, Chat input
```

Update CLAUDE.md Phase 12 checklist when complete.

---

## Dependency Map

```
Step 1 → Step 2 → Step 3 → Step 4
                                  ↓
Step 5 (API layer) ───────────────┤
                                  ↓
Step 6 (App shell) ───────────────┤
                                  ↓
Step 7 (Common components) ───────┤
                                  ↓
Steps 8–12 (Pages, parallel) ─────┤
                                  ↓
Step 13 (Polish) ─────────────────┤
                                  ↓
Step 14 (Go API gaps) ────────────┤
                                  ↓
Step 15 (Final check)
```

Steps 8–12 (Dashboard, Graph, Sources, Admin, Chat) are independent once Step 7 is done — they can be built in any order.
