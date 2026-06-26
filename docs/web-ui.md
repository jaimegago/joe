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

This map reflects the as-built `ui/src` tree (not the original implementation
sketch — the build added several API clients, hooks, and pages the sketch omitted).

```
ui/
├── src/
│   ├── api/                      # API client layer + typed wire schemas
│   │   ├── client.ts             # Fetch wrapper: session cookie + break-glass bearer, 401 handling
│   │   ├── schemas.ts            # Zod wire schemas (source of truth for shapes)
│   │   ├── types.ts              # Types derived from the Zod schemas (z.infer)
│   │   ├── graph.ts              # Graph API calls
│   │   ├── components.ts         # Components API calls (register / test / promote)
│   │   ├── security.ts           # Zones, component-zones, policies, principals, admins, read-promotions
│   │   ├── chat.ts               # Chat/session API calls
│   │   ├── taskStream.ts         # POST /api/v1/tasks/stream SSE client (step/final frames)
│   │   ├── adminSessions.ts      # Admin session-archive / retention API
│   │   ├── currentUser.ts        # GET /api/v1/me (who am I / admin / rbac-enabled)
│   │   ├── authConfig.ts         # GET /api/v1/auth/config (public OIDC capability)
│   │   ├── tokenStorage.ts       # break-glass bearer persistence (sessionStorage only)
│   │   ├── regime.ts             # incident / operational-regime API
│   │   ├── panic.ts              # panic / safe-mode status API
│   │   ├── mutateStatus.ts       # write-floor / mutate-enabled status
│   │   ├── credentialStatus.ts   # per-component credential arming status
│   │   ├── skills.ts             # skills registry API
│   │   └── llm.ts                # LLM provider/usage settings API
│   │
│   ├── auth/                     # Authentication context + route gating
│   │   ├── AuthContext.tsx       # useAuth(): OIDC + break-glass state, /me, global 401 handling
│   │   ├── AuthGate.tsx          # loading / authed / unauthed gate
│   │   ├── LoginPage.tsx         # logged-out shell (OIDC button + break-glass token)
│   │   └── RequireAdmin.tsx      # admin-only route guard
│   │
│   ├── components/
│   │   ├── ui/                   # shadcn/ui components (auto-generated)
│   │   │
│   │   ├── layout/               # AppShell, Sidebar, Header, PageContainer,
│   │   │                         #   IncidentBanner, ObservationBanner, SafeModeBanner
│   │   │
│   │   ├── graph/                # InfraGraph, NodeDetails, GraphControls, GraphLegend,
│   │   │                         #   nodes/GenericNode, edges/DependencyEdge
│   │   │
│   │   ├── admin/                # ZonesTable, ZoneForm, ComponentZoneAssign, UnassignedComponents,
│   │   │                         #   ComponentRegisterForm, PromoteComponentForm, PoliciesTable,
│   │   │                         #   PolicyForm, PrincipalsTable, AdminsTable, AdminForm,
│   │   │                         #   CredentialStatusTable, ReadPromotionsTable, SkillsTable,
│   │   │                         #   AdminSessionsPanel, RetentionPolicyEditor
│   │   │
│   │   ├── chat/                 # ChatWindow, MessageList, UserBubble, AssistantTurnView,
│   │   │                         #   ChatInput, ToolCallDisplay, Markdown, ZeroZoneEmptyState
│   │   │
│   │   ├── sessions/             # SessionRow, SessionListControls, IncidentClusterList
│   │   │
│   │   ├── incident/             # DeclareIncidentButton
│   │   │
│   │   ├── llm/                  # ProvidersTab, SettingsTab, UsageTab, UsageTable
│   │   │
│   │   └── common/               # LoadingSpinner, ErrorBoundary, EmptyState, ConfirmDialog
│   │
│   ├── pages/                    # Route pages
│   │   ├── GraphPage.tsx         # Infrastructure graph view
│   │   ├── ComponentsPage.tsx    # Components management
│   │   ├── ChatPage.tsx          # Web REPL / chat interface
│   │   ├── SessionsPage.tsx      # Session history
│   │   ├── UsersPage.tsx         # Identity registry (principals)
│   │   ├── CredentialStatusPage.tsx # Per-component credential status
│   │   ├── LLMSettingsPage.tsx   # LLM providers / usage
│   │   └── admin/                # ZonesAdminPage, PoliciesAdminPage, AdminsAdminPage,
│   │                             #   AutonomousReadsAdminPage, SkillsAdminPage
│   │
│   ├── hooks/                    # Custom React hooks (TanStack Query)
│   │   ├── useGraph.ts           # Graph data
│   │   ├── useComponents.ts      # Components data
│   │   ├── useZones.ts           # Security zones
│   │   ├── usePolicies.ts        # RBAC policies
│   │   ├── usePrincipals.ts      # Identity registry
│   │   ├── useChat.ts            # Chat/session state
│   │   ├── useSession.ts         # Single-session metadata
│   │   ├── useCurrentUser.ts     # GET /api/v1/me
│   │   ├── useAuthConfig.ts      # public OIDC capability
│   │   ├── useRegime.ts          # incident regime
│   │   ├── usePanicStatus.ts     # panic / safe-mode
│   │   ├── useMutateStatus.ts    # write-floor status
│   │   ├── useCredentialStatus.ts# credential arming
│   │   ├── useReadPromotions.ts  # autonomous-read toggles
│   │   ├── useAdminSessions.ts   # admin session archive
│   │   ├── useSkills.ts          # skills registry
│   │   └── useLLM.ts             # LLM settings
│   │
│   ├── lib/                      # Utilities
│   │   ├── utils.ts              # General utilities (cn, etc.)
│   │   ├── graph-layout.ts       # Graph layout algorithms
│   │   ├── constants.ts          # App constants
│   │   ├── sessionFilterSort.ts  # Session list filter/sort
│   │   ├── sessionGrouping.ts    # Incident clustering
│   │   ├── sessionLabel.ts       # Session display labels
│   │   ├── incidentAffordance.ts # Incident UI affordances
│   │   ├── lastSession.ts        # Last-session restore
│   │   ├── principals.ts         # Principal helpers
│   │   ├── llm-limits.ts         # LLM limit helpers
│   │   └── usage.ts              # Usage helpers
│   │
│   ├── App.tsx                   # Root component with providers + routing
│   ├── main.tsx                  # Entry point
│   └── index.css                 # Global styles + Tailwind
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

// Component types
export interface Component {
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
  name: string;
  description: string;
  allowed_actions: ('Read' | 'Mutate')[];  // binary action axis (D-0020)
  sourceCount?: number;   // Number of components in this zone
}

export interface ComponentZoneAssignment {
  component_id: string;
  zone_id: string;
  assigned_by: string;
  assigned_at: string;
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
│  │     │  │ Components    │ │ Alerts       │ │ Sessions     │           │
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
│  │     │  │ Components Health                                           ││
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
- Components health status grid
- Auto-refresh every 30 seconds

**API Calls:**
- `GET /api/v1/components` → components list with status
- `GET /api/v1/alerts` → active alerts
- `GET /api/v1/sessions?limit=5` → recent sessions

### 3. Admin Page (Security Zones)

Manage security zones, component assignments, and RBAC policies. Requires admin authentication.

**URL:** `/admin`

**Tabs:** Zones | Components | Policies

**Zones Tab:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Security Zones                                          [+ Create Zone]│
├─────────────────────────────────────────────────────────────────────────┤
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ Zone            │ Allowed Actions      │ Comps   │ Actions         ││
│  ├─────────────────────────────────────────────────────────────────────┤│
│  │ prod-readonly   │ Read                 │ 8       │ [Edit] [Delete] ││
│  │ prod-write      │ Read, Mutate         │ 3       │ [Edit] [Delete] ││
│  │ dev-full        │ Read, Mutate         │ 12      │ [Edit] [Delete] ││
│  │ unassigned ⚠️    │ Read                 │ 2       │ [View]          ││
│  └─────────────────────────────────────────────────────────────────────┘│
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ ⚠️ 2 Unassigned Components (require admin action)                   ││
│  │                                                                     ││
│  │ grafana/xyz-experiment       [Assign Zone ▼]                       ││
│  │ k8s/new-cluster              [Assign Zone ▼]                       ││
│  └─────────────────────────────────────────────────────────────────────┘│
└─────────────────────────────────────────────────────────────────────────┘
```

**Components Tab:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Component Zone Assignments                           [Filter by Zone ▼]│
├─────────────────────────────────────────────────────────────────────────┤
│  │ Component           │ Type       │ Zone          │ Assigned By      ││
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
- Assign/reassign components to zones
- CRUD for RBAC policies
- Highlight unassigned components with warning
- Confirmation dialogs for destructive actions
- Audit log of changes (who changed what, when)

**API Calls (Admin API):**
- `GET /api/v1/admin/zones`
- `POST /api/v1/admin/zones`
- `PUT /api/v1/admin/zones/{id}`
- `DELETE /api/v1/admin/zones/{id}`
- `GET /api/v1/admin/component-zones`
- `GET /api/v1/admin/unassigned`
- `POST /api/v1/admin/component-zones`
- `DELETE /api/v1/admin/component-zones/{componentID}`
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

### 5. Components Page

View and manage connected infrastructure components.

**URL:** `/components`

**Layout:**
```
┌─────────────────────────────────────────────────────────────────────────┐
│  Components                              [+ Add Component] [Refresh]    │
├─────────────────────────────────────────────────────────────────────────┤
│  Filter: [All Types ▼] [All Zones ▼] [All Status ▼]                    │
│                                                                         │
│  ┌─────────────────────────────────────────────────────────────────────┐│
│  │ Component           │ Type       │ Zone          │ Status │ Actions ││
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
- `GET /api/v1/components`
- `GET /api/v1/components/{id}`
- `POST /api/v1/components` (register new)
- `DELETE /api/v1/components/{id}`
- `POST /api/v1/components/{id}/test` (test connection)

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

Authentication is OIDC-based (authorization-code + PKCE) with a server-side
session. The human session is carried by an HttpOnly cookie — the browser never
holds a bearer token in `localStorage` for that flow.

1. The logged-out shell reads the public `GET /api/v1/auth/config` to learn whether
   OIDC is configured; that flag decides whether the login view offers the OIDC button.
2. Sign-in is a full-page navigation to `GET /api/v1/auth/login`, which 302-redirects
   the browser to the configured IdP (not a `fetch` — the IdP round-trip cannot happen
   inside one).
3. The IdP redirects back to `GET /api/v1/auth/callback`, which verifies the ID token,
   mints a server-side session, and sets the HttpOnly session cookie.
4. The API client sends that cookie on every request (`credentials: 'include'`). There
   is no `Authorization: Bearer` header and no token in `localStorage` for the human
   session.
5. Current-user state (principal, `is_admin`, `rbac_enabled`, reachable zones) comes
   from `GET /api/v1/me`; a 401 from any request transitions the app to logged-out.
6. Logout is `POST /api/v1/auth/logout`, which revokes the server-side session and
   clears the cookie.

A separate dev / break-glass bearer-token login also exists: that token is held in
memory on the API client and persisted in `sessionStorage` only (never `localStorage`,
never a cookie) and is sent as `Authorization: Bearer`. The cookie (human) and bearer
(break-glass) paths coexist on the same client.

For admin routes (`/admin/*`):
- Admin authority comes from the `is_admin` flag on `GET /api/v1/me`.
- Non-admins are kept out of admin surfaces (the `RequireAdmin` route guard).

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
   - Components health grid
   - Connect to real API

3. **Graph Page** (Day 3-4)
   - React Flow integration
   - Custom node components
   - Node details panel
   - Graph controls (zoom, filter)
   - Connect to graph API

4. **Components Page** (Day 5)
   - Components list with filtering
   - Component detail view
   - Status indicators

5. **Admin Page** (Day 6-7)
   - Zones management
   - Component zone assignments
   - Policies management
   - Unassigned components alert

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
