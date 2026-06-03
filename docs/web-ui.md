# Joe Web UI Specification

This document specifies the web frontend for Joe. It is designed to be used as implementation instructions for Claude Code.

## Tech Stack

| Layer | Choice | Version |
|-------|--------|---------|
| Language | TypeScript | 5.x |
| Framework | React | 18.x |
| Build | Vite | 5.x |
| Styling | Tailwind CSS | 3.x |
| Components | shadcn/ui | latest |
| Graph Visualization | React Flow | 11.x |
| Charts | Recharts | 2.x |
| Server State | TanStack Query | 5.x |
| Routing | React Router | 6.x |
| HTTP Client | Native fetch (wrapped) | - |

## Project Structure

```
ui/
├── src/
│   ├── api/                      # API client layer
│   │   ├── client.ts             # Fetch wrapper with auth, error handling
│   │   ├── types.ts              # TypeScript types matching joecored API
│   │   ├── graph.ts              # Graph API calls
│   │   ├── sources.ts            # Sources API calls
│   │   ├── security.ts           # Zones, policies API calls
│   │   └── chat.ts               # Chat/session API calls
│   │
│   ├── components/
│   │   ├── ui/                   # shadcn/ui components (auto-generated)
│   │   │   ├── button.tsx
│   │   │   ├── card.tsx
│   │   │   ├── dialog.tsx
│   │   │   ├── input.tsx
│   │   │   ├── select.tsx
│   │   │   ├── table.tsx
│   │   │   └── ...
│   │   │
│   │   ├── layout/               # Layout components
│   │   │   ├── AppShell.tsx      # Main layout with sidebar
│   │   │   ├── Sidebar.tsx       # Navigation sidebar
│   │   │   ├── Header.tsx        # Top header with user info
│   │   │   └── PageContainer.tsx # Page wrapper
│   │   │
│   │   ├── graph/                # Infrastructure graph components
│   │   │   ├── InfraGraph.tsx    # Main graph canvas
│   │   │   ├── nodes/            # Custom node types
│   │   │   │   ├── ServiceNode.tsx
│   │   │   │   ├── DatabaseNode.tsx
│   │   │   │   ├── K8sNode.tsx
│   │   │   │   └── GenericNode.tsx
│   │   │   ├── edges/            # Custom edge types
│   │   │   │   └── DependencyEdge.tsx
│   │   │   ├── NodeDetails.tsx   # Selected node detail panel
│   │   │   ├── GraphControls.tsx # Zoom, filter, layout controls
│   │   │   └── GraphLegend.tsx   # Node type legend
│   │   │
│   │   ├── dashboard/            # Dashboard components
│   │   │   ├── MetricsCard.tsx   # Single metric display
│   │   │   ├── AlertsList.tsx    # Active alerts
│   │   │   ├── SourcesHealth.tsx # Sources status overview
│   │   │   └── RecentSessions.tsx# Recent chat sessions
│   │   │
│   │   ├── admin/                # Admin UI components
│   │   │   ├── ZonesTable.tsx    # Security zones list
│   │   │   ├── ZoneForm.tsx      # Create/edit zone
│   │   │   ├── SourceZoneAssign.tsx # Assign source to zone
│   │   │   ├── PoliciesTable.tsx # RBAC policies list
│   │   │   ├── PolicyForm.tsx    # Create/edit policy
│   │   │   └── UnassignedSources.tsx # Sources needing assignment
│   │   │
│   │   ├── chat/                 # Chat interface components
│   │   │   ├── ChatWindow.tsx    # Main chat container
│   │   │   ├── MessageList.tsx   # Message history
│   │   │   ├── MessageBubble.tsx # Single message
│   │   │   ├── ChatInput.tsx     # Input with send button
│   │   │   └── ToolCallDisplay.tsx # Show tool executions
│   │   │
│   │   └── common/               # Shared components
│   │       ├── LoadingSpinner.tsx
│   │       ├── ErrorBoundary.tsx
│   │       ├── EmptyState.tsx
│   │       └── ConfirmDialog.tsx
│   │
│   ├── pages/                    # Route pages
│   │   ├── GraphPage.tsx         # Infrastructure graph view
│   │   ├── DashboardPage.tsx     # Overview dashboard
│   │   ├── SourcesPage.tsx       # Sources management
│   │   ├── AdminPage.tsx         # Security zones & policies
│   │   ├── ChatPage.tsx          # Web REPL / chat interface
│   │   ├── SessionsPage.tsx      # Session history
│   │   └── SettingsPage.tsx      # User settings
│   │
│   ├── hooks/                    # Custom React hooks
│   │   ├── useGraph.ts           # Graph data fetching
│   │   ├── useSources.ts         # Sources data
│   │   ├── useZones.ts           # Security zones
│   │   ├── usePolicies.ts        # RBAC policies
│   │   ├── useChat.ts            # Chat/session state
│   │   └── useAuth.ts            # Authentication state
│   │
│   ├── lib/                      # Utilities
│   │   ├── utils.ts              # General utilities (cn, etc.)
│   │   ├── graph-layout.ts       # Graph layout algorithms
│   │   └── constants.ts          # App constants
│   │
│   ├── App.tsx                   # Root component with providers
│   ├── main.tsx                  # Entry point
│   └── index.css                 # Global styles + Tailwind
│
├── public/
│   └── favicon.ico
│
├── package.json
├── tsconfig.json
├── tailwind.config.js
├── postcss.config.js
├── vite.config.ts
├── components.json               # shadcn/ui config
└── .env.example
```

---

## API Client

### Base Client

```typescript
// src/api/client.ts

const API_BASE = import.meta.env.VITE_API_URL || 'http://localhost:7777';

interface ApiError {
  error: string;
  message: string;
  details?: Record<string, unknown>;
}

class ApiClient {
  private token: string | null = null;

  setToken(token: string) {
    this.token = token;
  }

  clearToken() {
    this.token = null;
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const headers: HeadersInit = {
      'Content-Type': 'application/json',
      ...options.headers,
    };

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`;
    }

    const response = await fetch(`${API_BASE}${path}`, {
      ...options,
      headers,
    });

    if (!response.ok) {
      const error: ApiError = await response.json();
      throw new Error(error.message || 'API request failed');
    }

    return response.json();
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: 'GET' });
  }

  post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: 'POST',
      body: JSON.stringify(body),
    });
  }

  put<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: 'PUT',
      body: JSON.stringify(body),
    });
  }

  delete<T>(path: string): Promise<T> {
    return this.request<T>(path, { method: 'DELETE' });
  }
}

export const apiClient = new ApiClient();
```

### Type Definitions

```typescript
// src/api/types.ts

// Graph types
export interface GraphNode {
  id: string;
  kind: string;           // "Deployment", "Service", "Pod", "Database", etc.
  name: string;
  namespace?: string;
  cluster?: string;
  metadata: Record<string, unknown>;
  labels?: Record<string, string>;
  status?: 'healthy' | 'degraded' | 'unhealthy' | 'unknown';
}

export interface GraphEdge {
  id: string;
  source: string;         // Node ID
  target: string;         // Node ID
  type: string;           // "depends_on", "runs_on", "stores_in", etc.
  metadata?: Record<string, unknown>;
}

export interface Graph {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

// Source types
export interface Source {
  id: string;             // "grafana/xyz-prod"
  type: string;           // "grafana", "kubernetes", "prometheus", etc.
  zone?: string;          // Security zone ID, null if unassigned
  config: Record<string, unknown>;
  status: 'connected' | 'disconnected' | 'error';
  lastChecked?: string;   // ISO timestamp
  error?: string;
}

// Security types
export interface SecurityZone {
  id: string;
  description: string;
  actions: ('Read' | 'Query' | 'Mutate' | 'Delete')[];
  constraints?: {
    require_approval?: ('Mutate' | 'Delete')[];
  };
  sourceCount?: number;   // Number of sources in this zone
}

export interface SourceZoneAssignment {
  sourceId: string;
  zoneId: string;
  assignedBy: string;
  assignedAt: string;
  reason?: string;
}

export interface RbacPolicy {
  id: string;
  principal: string;      // User email or group name
  zones: string[];        // Zone IDs
  createdBy: string;
  createdAt: string;
}

// Chat/Session types
export interface ChatMessage {
  id: string;
  role: 'user' | 'assistant';
  content: string;
  toolCalls?: ToolCall[];
  timestamp: string;
}

export interface ToolCall {
  id: string;
  name: string;
  arguments: Record<string, unknown>;
  result?: string;
  status: 'pending' | 'success' | 'error';
}

export interface Session {
  id: string;
  startedAt: string;
  endedAt?: string;
  summary?: string;
  issue?: string;
  resolution?: string;
  messageCount: number;
}

// Dashboard types
export interface Alert {
  id: string;
  severity: 'critical' | 'warning' | 'info';
  source: string;
  message: string;
  timestamp: string;
  acknowledged: boolean;
}
```

---

## Pages Specification

### 1. Graph Page (Primary View)

The infrastructure graph is the main visualization. Users can explore service dependencies, see health status, and click nodes for details.

**URL:** `/graph`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  ┌─────┐  Infrastructure Graph            [Refresh] [Filter▼] [Layout▼]│
│  │ Nav │                                                                │
│  │     │ ┌─────────────────────────────────────────────────────────────┐│
│  │     │ │                                                             ││
│  │     │ │                    React Flow Canvas                        ││
│  │     │ │                                                             ││
│  │     │ │     ┌─────────┐        ┌─────────┐                         ││
│  │     │ │     │ service │────────│  redis  │                         ││
│  │     │ │     └────┬────┘        └─────────┘                         ││
│  │     │ │          │                                                  ││
│  │     │ │          ▼                                                  ││
│  │     │ │     ┌─────────┐                                            ││
│  │     │ │     │postgres │                                            ││
│  │     │ │     └─────────┘                                            ││
│  │     │ │                                                             ││
│  │     │ └─────────────────────────────────────────────────────────────┘│
│  │     │                                                                │
│  │     │ ┌─────────────────────────────────────────────────────────────┐│
│  │     │ │ Selected: payment-service                                   ││
│  │     │ │ Kind: Deployment | Namespace: prod | Replicas: 3/3         ││
│  │     │ │ Dependencies: redis-cache, postgres-db                      ││
│  │     │ │ [View Logs] [View Metrics] [View in Graph]                 ││
│  │     │ └─────────────────────────────────────────────────────────────┘│
│  └─────┘                                                                │
└─────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Interactive pan/zoom graph with React Flow
- Custom node rendering based on `kind` (different icons/colors)
- Node status indicators (green/yellow/red dot)
- Click node to select and show details panel
- Edge labels showing relationship type
- Filter by: namespace, kind, cluster, status
- Layout options: hierarchical, force-directed, manual
- Minimap for navigation
- Search/highlight specific nodes

**API Calls:**
- `GET /api/v1/graph` → full graph
- `GET /api/v1/graph/node/{id}` → node details
- `GET /api/v1/graph/node/{id}/related` → related nodes

### 2. Dashboard Page

Overview of system health, recent activity, and quick stats.

**URL:** `/` or `/dashboard`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  ┌─────┐  Dashboard                                                     │
│  │ Nav │                                                                │
│  │     │  ┌──────────────┐ ┌──────────────┐ ┌──────────────┐           │
│  │     │  │ Sources      │ │ Alerts       │ │ Sessions     │           │
│  │     │  │ 12 connected │ │ 2 active     │ │ 5 today      │           │
│  │     │  │ 1 error      │ │ 0 critical   │ │              │           │
│  │     │  └──────────────┘ └──────────────┘ └──────────────┘           │
│  │     │                                                                │
│  │     │  ┌────────────────────────────────┐ ┌─────────────────────────┐│
│  │     │  │ Active Alerts                  │ │ Recent Sessions         ││
│  │     │  │ ┌────────────────────────────┐ │ │ • Payment timeout debug ││
│  │     │  │ │ ⚠ High latency: api-gw    │ │ │   2 hours ago           ││
│  │     │  │ │ ⚠ Pod restart: worker-3   │ │ │ • Deploy review         ││
│  │     │  │ └────────────────────────────┘ │ │   5 hours ago           ││
│  │     │  └────────────────────────────────┘ └─────────────────────────┘│
│  │     │                                                                │
│  │     │  ┌─────────────────────────────────────────────────────────────┐│
│  │     │  │ Sources Health                                              ││
│  │     │  │ ● k8s/prod-main    ● grafana/main   ○ prometheus/prod      ││
│  │     │  │ ● k8s/staging      ● loki/prod      ● argocd/main          ││
│  │     │  └─────────────────────────────────────────────────────────────┘│
│  └─────┘                                                                │
└─────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Summary cards with key metrics
- Active alerts list with severity indicators
- Recent sessions with quick access
- Sources health status grid
- Auto-refresh every 30 seconds

**API Calls:**
- `GET /api/v1/sources` → sources list with status
- `GET /api/v1/alerts` → active alerts
- `GET /api/v1/sessions?limit=5` → recent sessions

### 3. Admin Page (Security Zones)

Manage security zones, source assignments, and RBAC policies. Requires admin authentication.

**URL:** `/admin`

**Tabs:** Zones | Sources | Policies

**Zones Tab:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Security Zones                                          [+ Create Zone]│
├─────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ Zone            │ Actions              │ Sources │ Actions         ││
│  ├─────────────────────────────────────────────────────────────────────┤│
│  │ prod-readonly   │ Read, Query          │ 8       │ [Edit] [Delete] ││
│  │ prod-write      │ Read, Query, Mutate  │ 3       │ [Edit] [Delete] ││
│  │ dev-full        │ Read, Query, Mutate, │ 12      │ [Edit] [Delete] ││
│  │                 │ Delete               │         │                 ││
│  │ unassigned ⚠️    │ Read                 │ 2       │ [View]          ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ ⚠️ 2 Unassigned Sources (require admin action)                      ││
│  │                                                                     ││
│  │ grafana/xyz-experiment       [Assign Zone ▼]                       ││
│  │ k8s/new-cluster              [Assign Zone ▼]                       ││
│  └─────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────┘
```

**Sources Tab:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Source Zone Assignments                              [Filter by Zone ▼]│
├─────────────────────────────────────────────────────────────────────────┤
│  │ Source              │ Type       │ Zone          │ Assigned By      ││
│  ├─────────────────────────────────────────────────────────────────────┤│
│  │ k8s/prod-main       │ kubernetes │ prod-readonly │ admin@co.com     ││
│  │ grafana/main        │ grafana    │ prod-readonly │ admin@co.com     ││
│  │ k8s/dev-cluster     │ kubernetes │ dev-full      │ admin@co.com     ││
│  │ prometheus/prod     │ prometheus │ prod-readonly │ admin@co.com     ││
└─────────────────────────────────────────────────────────────────────────┘
```

**Policies Tab:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  RBAC Policies                                        [+ Create Policy] │
├─────────────────────────────────────────────────────────────────────────┤
│  │ Principal            │ Zones                        │ Actions       ││
│  ├─────────────────────────────────────────────────────────────────────┤│
│  │ sre-team             │ prod-readonly, prod-write,   │ [Edit] [Del]  ││
│  │                      │ dev-full                     │               ││
│  │ senior-engineers     │ prod-readonly, dev-full      │ [Edit] [Del]  ││
│  │ junior-engineers     │ dev-full                     │ [Edit] [Del]  ││
│  │ security-team        │ prod-readonly                │ [Edit] [Del]  ││
└─────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- CRUD for security zones
- Assign/reassign sources to zones
- CRUD for RBAC policies
- Highlight unassigned sources with warning
- Confirmation dialogs for destructive actions
- Audit log of changes (who changed what, when)

**API Calls (Admin API):**
- `GET /api/v1/admin/zones`
- `POST /api/v1/admin/zones`
- `PUT /api/v1/admin/zones/{id}`
- `DELETE /api/v1/admin/zones/{id}`
- `GET /api/v1/admin/source-zones`
- `GET /api/v1/admin/source-zones/unassigned`
- `POST /api/v1/admin/source-zones`
- `DELETE /api/v1/admin/source-zones/{sourceId}`
- `GET /api/v1/admin/policies`
- `POST /api/v1/admin/policies`
- `DELETE /api/v1/admin/policies/{id}`

### 4. Chat Page (Web REPL)

Web-based chat interface to interact with Joe.

**URL:** `/chat` or `/chat/{sessionId}`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  ┌─────┐  Chat with Joe                              [New Session]      │
│  │ Nav │                                                                │
│  │     │  ┌─────────────────────────────────────────────────────────────┐│
│  │     │  │                                                             ││
│  │     │  │  ┌─────────────────────────────────────────────────────┐   ││
│  │     │  │  │ 👤 Why is payment-service timing out?               │   ││
│  │     │  │  └─────────────────────────────────────────────────────┘   ││
│  │     │  │                                                             ││
│  │     │  │  ┌─────────────────────────────────────────────────────┐   ││
│  │     │  │  │ 🤖 Let me investigate...                            │   ││
│  │     │  │  │                                                     │   ││
│  │     │  │  │ ┌─────────────────────────────────────────────────┐ │   ││
│  │     │  │  │ │ 🔧 k8s_logs(payment-service, tail=100)          │ │   ││
│  │     │  │  │ │ → Found connection timeout errors to postgres   │ │   ││
│  │     │  │  │ └─────────────────────────────────────────────────┘ │   ││
│  │     │  │  │                                                     │   ││
│  │     │  │  │ The payment-service is experiencing connection     │   ││
│  │     │  │  │ timeouts to postgres-db. The connection pool       │   ││
│  │     │  │  │ appears exhausted...                               │   ││
│  │     │  │  └─────────────────────────────────────────────────────┘   ││
│  │     │  │                                                             ││
│  │     │  └─────────────────────────────────────────────────────────────┘│
│  │     │  ┌─────────────────────────────────────────────────────────────┐│
│  │     │  │ Type a message...                              [Send]       ││
│  │     │  └─────────────────────────────────────────────────────────────┘│
│  └─────┘                                                                │
└─────────────────────────────────────────────────────────────────────────┘
```

**Features:**
- Message history with user/assistant distinction
- Tool call display (collapsible, shows name + result)
- Streaming responses (if supported by backend)
- Session persistence
- New session button
- Session history sidebar (optional)
- Markdown rendering in assistant messages
- Code syntax highlighting

**API Calls:**
- `POST /api/v1/tasks/stream` → run an agentic turn, streaming `step`/`final` SSE events
- `GET /api/v1/sessions/{id}/messages` → load session history
- `POST /api/v1/sessions` → create new session

### 5. Sources Page

View and manage connected infrastructure sources.

**URL:** `/sources`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Sources                                    [+ Add Source] [Refresh]    │
├─────────────────────────────────────────────────────────────────────────┤
│  Filter: [All Types ▼] [All Zones ▼] [All Status ▼]                    │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ Source              │ Type       │ Zone          │ Status │ Actions ││
│  ├─────────────────────────────────────────────────────────────────────┤│
│  │ ● k8s/prod-main     │ kubernetes │ prod-readonly │ ● OK   │ [View]  ││
│  │ ● grafana/main      │ grafana    │ prod-readonly │ ● OK   │ [View]  ││
│  │ ○ prometheus/prod   │ prometheus │ prod-readonly │ ● Err  │ [View]  ││
│  │ ● k8s/dev-cluster   │ kubernetes │ dev-full      │ ● OK   │ [View]  ││
│  │ ◐ k8s/new-cluster   │ kubernetes │ unassigned    │ ● OK   │ [View]  ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ Selected: prometheus/prod                                           ││
│  │ Type: prometheus                                                    ││
│  │ URL: https://prometheus.prod.internal                               ││
│  │ Zone: prod-readonly                                                 ││
│  │ Status: Error - Connection refused                                  ││
│  │ Last checked: 2 minutes ago                                         ││
│  │                                                                     ││
│  │ [Test Connection] [Edit] [Remove]                                   ││
│  └─────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────┘
```

**API Calls:**
- `GET /api/v1/sources`
- `GET /api/v1/sources/{id}`
- `POST /api/v1/sources` (register new)
- `DELETE /api/v1/sources/{id}`
- `POST /api/v1/sources/{id}/test` (test connection)

---

## Component Specifications

### Node Types (React Flow)

Each node kind has a distinct visual style:

| Kind | Icon | Color | Shape |
|------|------|-------|-------|
| Deployment | 🚀 | Blue | Rounded rect |
| Service | 🔗 | Purple | Rounded rect |
| Pod | 📦 | Light blue | Small rect |
| Database | 🗄️ | Orange | Cylinder-ish |
| Cache | ⚡ | Yellow | Rounded rect |
| Queue | 📬 | Green | Parallelogram |
| External | 🌐 | Gray | Cloud shape |
| Secret | 🔐 | Red | Small rect |

Node structure:
```
┌─────────────────────┐
│ 🚀 payment-service  │  ← Icon + name
│ ─────────────────── │
│ prod / payments     │  ← namespace / cluster
│ ● 3/3 replicas      │  ← status line
└─────────────────────┘
```

### Edge Types

| Type | Style | Label |
|------|-------|-------|
| depends_on | Solid arrow | "depends on" |
| runs_on | Dashed | "runs on" |
| stores_in | Solid, thick | "stores in" |
| uses_secret | Dotted, red | "uses" |
| managed_by | Dashed, gray | "managed by" |

### Status Indicators

| Status | Color | Icon |
|--------|-------|------|
| healthy | Green | ● |
| degraded | Yellow | ◐ |
| unhealthy | Red | ○ |
| unknown | Gray | ◌ |

---

## Authentication Flow

1. User visits app → check for token in localStorage
2. If no token → redirect to `/login`
3. Login page → POST credentials to `/api/v1/auth/login`
4. On success → store token in localStorage, redirect to `/`
5. API client includes `Authorization: Bearer {token}` on all requests
6. On 401 response → clear token, redirect to `/login`

For admin routes (`/admin/*`):
- Check if user has admin role
- If not admin → show "Access Denied" or redirect

---

## Environment Variables

```bash
# .env.example
VITE_API_URL=http://localhost:7777
VITE_WS_URL=ws://localhost:7777  # For future WebSocket support
```

---

## Setup Commands

```bash
# From joe repo root
cd ui

# Install dependencies
npm install

# Add shadcn/ui components
npx shadcn-ui@latest init
npx shadcn-ui@latest add button card dialog input select table tabs

# Development
npm run dev

# Build for production
npm run build

# Preview production build
npm run preview
```

---

## Implementation Order

1. **Foundation** (Day 1)
   - Vite + React + TypeScript setup
   - Tailwind + shadcn/ui configuration
   - API client with types
   - App shell with routing
   - Basic layout (sidebar, header)

2. **Dashboard** (Day 2)
   - Dashboard page with mock data
   - Metric cards
   - Sources health grid
   - Connect to real API

3. **Graph Page** (Day 3-4)
   - React Flow integration
   - Custom node components
   - Node details panel
   - Graph controls (zoom, filter)
   - Connect to graph API

4. **Sources Page** (Day 5)
   - Sources list with filtering
   - Source detail view
   - Status indicators

5. **Admin Page** (Day 6-7)
   - Zones management
   - Source zone assignments
   - Policies management
   - Unassigned sources alert

6. **Chat Page** (Day 8)
   - Message list
   - Chat input
   - Tool call display
   - Session management

7. **Polish** (Day 9-10)
   - Error handling
   - Loading states
   - Empty states
   - Responsive design
   - Testing

---

## Notes for Implementation

- Use TanStack Query for all API calls (caching, refetching, loading states)
- Prefer server state over client state (Zustand only if truly needed)
- All components should handle loading, error, and empty states
- Use React Flow's built-in controls for minimap, zoom, pan
- Keep bundle size in mind — lazy load pages with React.lazy()
- Write JSDoc comments for complex components
- Use absolute imports (`@/components/...`) via tsconfig paths
