# Joe: Data Flow Deep Dive

## Project Name
**Joe** — Joe Operates Everything

## Design Principle: AI Agnostic
Joe treats LLMs as swappable inference backends. The orchestration, memory, and tooling are Joe's—not the AI provider's.

---

## Two-Binary Architecture

Joe is built as two binaries that communicate via HTTP:

```
┌─────────────────────────────────────────────────────────────────────────────────────────┐
│                                                                                          │
│  joe (Joe Local)                           joecored (Joe Core)                          │
│  ────────────────                          ──────────────────                           │
│                                                                                          │
│  ┌────────────────────────────┐           ┌────────────────────────────────────────────┐│
│  │  User Agent                │           │  HTTP API (:7777)                          ││
│  │                            │           │                                            ││
│  │  REPL ──► Agentic Loop     │           │  GET  /api/v1/graph/query                  ││
│  │               │            │           │  GET  /api/v1/graph/related/:id            ││
│  │               ▼            │           │  GET  /api/v1/k8s/:cluster/...             ││
│  │         tool_call(...)     │           │  POST /api/v1/prom/query                   ││
│  │               │            │           │  GET  /api/v1/clarifications               ││
│  │        ┌──────┴──────┐     │           │                                            ││
│  │        ▼             ▼     │           └──────────────┬─────────────────────────────┘│
│  │   Local Tools    Core Tools│                          │                              │
│  │   (direct)       (HTTP) ───┼──────────────────────────┘                              │
│  │                            │                          │                              │
│  │   • read_file              │           ┌──────────────┴─────────────────────────────┐│
│  │   • write_file             │           │  Core Agent                                ││
│  │   • local_git_diff         │           │                                            ││
│  │   • local_git_status       │           │  • Background refresh (every 5min)         ││
│  │   • run_command            │           │  • .joe/ file discovery                    ││
│  │                            │           │  • Onboarding flow                         ││
│  │  ┌──────────────────────┐  │           │  • Clarification queue                     ││
│  │  │  LLM Adapter         │  │           │                                            ││
│  │  │  (Claude/GPT/Gemini) │  │           │  Uses LLM for:                             ││
│  │  └──────────────────────┘  │           │  • "What is this service?"                 ││
│  │                            │           │  • "What connects to what?"                ││
│  └────────────────────────────┘           │                                            ││
│                                           └──────────────┬─────────────────────────────┘│
│                                                          │                              │
│                                           ┌──────────────┴─────────────────────────────┐│
│                                           │  Core Services                             ││
│                                           │                                            ││
│                                           │  ┌──────────┐ ┌──────────┐ ┌──────────┐   ││
│                                           │  │  Graph   │ │   SQL    │ │ Adapters │   ││
│                                           │  │  Store   │ │  Store   │ │ K8s, Git │   ││
│                                           │  │ (Cayley) │ │ (SQLite) │ │ ArgoCD.. │   ││
│                                           │  └──────────┘ └──────────┘ └──────────┘   ││
│                                           │                                            ││
│                                           │  ┌──────────┐                              ││
│                                           │  │   LLM    │ (for Core Agent reasoning)  ││
│                                           │  │ Adapter  │                              ││
│                                           │  └──────────┘                              ││
│                                           │                                            ││
│                                           └────────────────────────────────────────────┘│
│                                                                                          │
└─────────────────────────────────────────────────────────────────────────────────────────┘
```

**Key points:**
- **joe** (Joe Local): User-facing CLI with User Agent, REPL, local tools
- **joecored** (Joe Core): Daemon with HTTP API, Core Agent, Core Services
- Both agents can use LLMs (User Agent for conversation, Core Agent for discovery)
- HTTP API is the contract between them

---

## LLM Adapter (Used by Both Agents)

```
┌─────────────────────────────────────────────┐
│            LLM Adapter Interface             │
│   (Orchestration, Memory, Tools, Safety)    │
└─────────────────────┬───────────────────────┘
                      │
                      ▼
            ┌───────────────────┐
            │   LLM Adapter     │
            │   (Interface)     │
            └─────────┬─────────┘
                      │
        ┌─────────────┼─────────────┐
        ▼             ▼             ▼
   ┌─────────┐  ┌─────────┐  ┌─────────┐
   │ Claude  │  │  GPT-4  │  │ Gemini  │
   │ Adapter │  │ Adapter │  │ Adapter │
   └─────────┘  └─────────┘  └─────────┘
```

The LLM provides reasoning. Joe provides:
- Infrastructure knowledge (graph)
- Tool execution
- Memory / context
- Safety controls

---

## The Scenario

You type:

```
$ joe

> I have this issue reported by a user:
> "Payment failed for order #12847, getting timeout error on checkout"
>
> I've done some troubleshooting and found this error in payment-service logs:
> 
> 2025-01-30T14:23:15Z ERROR payment-service context deadline exceeded: 
> POST https://api.stripe.com/v1/charges timeout after 30s
> transaction_id=txn_abc123 order_id=12847
>
> Help me figure this out.
```

---

## What Joe Already Knows (Memory)

Before you even typed, Joe has built knowledge through two mechanisms:

### A. Infrastructure Graph (Built by Discovery)

This runs periodically (or on-demand). Here's what Joe discovered about your system:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Joe's Infrastructure Graph                        │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌──────────────┐    sourced_from    ┌─────────────────┐                │
│  │ ArgoCD App   │◄──────────────────│ Git Repo        │                │
│  │ payments-app │                    │ infra/payments  │                │
│  └──────┬───────┘                    │ (helm chart)    │                │
│         │                            └─────────────────┘                │
│         │ deploys                                                        │
│         ▼                                                                │
│  ┌──────────────┐    references     ┌─────────────────┐                │
│  │ Deployment   │─────────────────►│ Secret          │                │
│  │ payment-svc  │                   │ stripe-api-key  │                │
│  │ ns: payments │                   └─────────────────┘                │
│  └──────┬───────┘                                                       │
│         │                                                                │
│         │ exposes            ┌─────────────────┐                        │
│         ▼                    │ ConfigMap       │                        │
│  ┌──────────────┐           │ payment-config  │                        │
│  │ Service      │           │ timeout: 30s    │◄─── configures         │
│  │ payment-svc  │           │ stripe_url: ... │                        │
│  └──────┬───────┘           └─────────────────┘                        │
│         │                                                                │
│         │ routes_to                                                      │
│         ▼                                                                │
│  ┌──────────────┐    calls_external    ┌──────────────────┐            │
│  │ Pods (3)     │─────────────────────►│ External Service │            │
│  │ payment-svc  │                      │ api.stripe.com   │            │
│  │ -7f8b9c-x2k4 │                      └──────────────────┘            │
│  │ -7f8b9c-m3n5 │                                                       │
│  │ -7f8b9c-p8q2 │                                                       │
│  └──────────────┘                                                       │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**How this was built (by Core Agent in joecored):**

```
joecored: Core Agent Discovery Run (last: 5 min ago)
│
├─► K8s Adapter
│   ├─ List Deployments → found payment-service
│   ├─ Get Pod specs → found env refs to Secret, ConfigMap
│   ├─ Analyze container args → found stripe.com dependency
│   └─ Emit nodes + edges
│
├─► ArgoCD Adapter  
│   ├─ List Applications → found payments-app
│   ├─ Get app spec → source is git@gitlab:infra/payments
│   ├─ Get managed resources → maps to k8s resources
│   └─ Emit nodes + edges
│
├─► Git Adapter
│   ├─ Clone/pull repo infra/payments
│   ├─ Found Chart.yaml, values.yaml
│   ├─ Parse values → timeout: 30s, replicas: 3
│   └─ Emit nodes + edges
│
└─► Graph Store
    └─ Merge all nodes/edges into unified graph
```

### B. Conversation Memory (Built from past sessions)

Joe remembers patterns from previous investigations:

```
┌─────────────────────────────────────────────────────────────────────────┐
│                     Joe's Conversation Memory                            │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Session: 2025-01-15                                                     │
│  ─────────────────                                                       │
│  Issue: payment-service timeouts to Stripe                               │
│  Root cause: Stripe API degradation (status.stripe.com showed incident)  │
│  Resolution: Waited for Stripe to recover                                │
│  Tags: [payment-service, stripe, external-dependency, timeout]           │
│                                                                          │
│  Session: 2025-01-22                                                     │
│  ─────────────────                                                       │
│  Issue: checkout flow slow                                               │
│  Root cause: payment-service HPA maxed out, needed limit increase        │
│  Resolution: Increased HPA max from 5 to 10                              │
│  Tags: [payment-service, scaling, hpa]                                   │
│                                                                          │
│  Learned patterns:                                                       │
│  - payment-service timeout → check Stripe status first                   │
│  - payment-service depends on external API with 30s timeout              │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

**How this was built:**

At the end of each session, Joe:
1. Summarizes the conversation (LLM call)
2. Extracts: issue, root cause, resolution, affected components
3. Stores in memory DB with embeddings for semantic search

---

## Data Flow: Your Query

The key insight: **the LLM drives the investigation.** Joe doesn't parse your input or pre-fetch anything. The User Agent gives the LLM tools and lets it decide what to call.

```
┌───────────────────────────────────────────────────────────────────────┐
│ PHASE 1: USER AGENT SENDS TO LLM WITH TOOLS                           │
└───────────────────────────────────────────────────────────────────────┘

You type your message
        │
        ▼
┌───────────────────┐
│  joe (Joe Local)  │
│      REPL         │
└─────────┬─────────┘
          │
          │ User Agent constructs LLM request:
          │
          ▼
┌───────────────────────────────────────────────────────────────────────┐
│                                                                        │
│  System prompt: "You are Joe, an infrastructure copilot. You have     │
│  access to tools to investigate infrastructure issues. Use them."      │
│                                                                        │
│  Available tools:                                                      │
│                                                                        │
│  LOCAL TOOLS (run directly in joe):                                   │
│    • read_file(path)           - read user's local file               │
│    • write_file(path, content) - write to user's local file           │
│    • local_git_diff()          - user's uncommitted changes           │
│    • local_git_status()        - user's working tree status           │
│    • run_command(cmd)          - run shell command locally            │
│                                                                        │
│  CORE TOOLS (call joecored API via HTTP):                             │
│    • graph_query(query)         - search infrastructure graph          │
│    • graph_related(node, depth) - get connected nodes                  │
│    • k8s_get(cluster, resource, ns, name) - get k8s resource          │
│    • k8s_logs(cluster, pod, ns, lines)    - get pod logs              │
│    • argocd_get(instance, app)  - get ArgoCD app status               │
│    • git_read(repo, path)       - read file from cloned repo          │
│    • prom_query(promql)         - query Prometheus metrics            │
│    • memory_search(query)       - find similar past incidents          │
│    • http_get(url)              - fetch URL (status pages, APIs)       │
│                                                                        │
│  User message: "I have this issue reported by a user..."               │
│                                                                        │
└───────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌───────────────────────────────────────────────────────────────────────┐
│ PHASE 2: LLM REASONS AND CALLS TOOLS (agentic loop)                    │
└───────────────────────────────────────────────────────────────────────┘

┌───────────────────┐
│   LLM Adapter     │  ← model: claude-sonnet-4 / gpt-4 / gemini / ollama
└─────────┬─────────┘
          │
          ▼
┌───────────────────────────────────────────────────────────────────────┐
│  LLM thinks and acts:                                                  │
│                                                                        │
│  "User mentions payment-service and Stripe timeout. Let me check."     │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Tool call: graph_query("payment-service")                       │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼ CORE TOOL → HTTP to joecored                                │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ joe ──► GET http://localhost:7777/api/v1/graph/query            │  │
│  │            ?q=payment-service                                   │  │
│  │                                                                  │  │
│  │ joecored ──► queries Graph Store ──► returns nodes:             │  │
│  │   - deployment/payments/payment-service                         │  │
│  │   - configmap/payments/payment-config (timeout: 30s)            │  │
│  │   - secret/payments/stripe-api-key                              │  │
│  │   - argocd-app/payments-app → git repo infra/payments           │  │
│  │   - external/api.stripe.com                                     │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼                                                              │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Tool call: memory_search("payment stripe timeout")              │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼ CORE TOOL → HTTP to joecored                                │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ joe ──► POST http://localhost:7777/api/v1/memory/search         │  │
│  │                                                                  │  │
│  │ joecored ──► queries SQL Store ──► returns:                     │  │
│  │   Session 2025-01-15: Similar issue, root cause was Stripe      │  │
│  │   degradation. Resolved by waiting.                             │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼                                                              │
│  LLM thinks: "Past incident suggests checking Stripe status"           │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Tool call: http_get("https://status.stripe.com/api/v2/status")  │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼ CORE TOOL → HTTP to joecored (proxy)                        │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ joe ──► POST http://localhost:7777/api/v1/http/fetch            │  │
│  │                                                                  │  │
│  │ joecored ──► fetches external URL ──► returns:                  │  │
│  │   { "status": "degraded", "message": "Investigating increased   │  │
│  │     API latency", "started": "2025-01-30T14:15:00Z" }           │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼                                                              │
│  LLM thinks: "Stripe is degraded, started 14:15, user error at 14:23"  │
│                                                                        │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ Tool call: k8s_logs("prod", "payment-service", "payments", 50)  │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼ CORE TOOL → HTTP to joecored                                │
│  ┌─────────────────────────────────────────────────────────────────┐  │
│  │ joe ──► GET http://localhost:7777/api/v1/k8s/prod/logs          │  │
│  │            /payments/payment-service?lines=50                   │  │
│  │                                                                  │  │
│  │ joecored ──► K8s Adapter ──► returns: 47 timeout errors         │  │
│  └─────────────────────────────────────────────────────────────────┘  │
│         │                                                              │
│         ▼                                                              │
│  LLM synthesizes final response                                        │
│                                                                        │
└───────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌───────────────────────────────────────────────────────────────────────┐
│ PHASE 3: RESPONSE TO USER                                              │
└───────────────────────────────────────────────────────────────────────┘

┌───────────────────────────────────────────────────────────────────────┐
│                                                                        │
│  Root cause: Stripe API degradation (external)                         │
│                                                                        │
│  Evidence:                                                             │
│    • status.stripe.com shows "Investigating increased API latency"     │
│      started at 14:15 UTC                                              │
│    • Your error occurred at 14:23 UTC (8 min after incident)           │
│    • 47 timeout errors in payment-service logs since 14:15             │
│    • Matches previous incident on Jan 15 (also Stripe)                 │
│                                                                        │
│  Your payment-service is healthy—the issue is upstream.                │
│                                                                        │
│  Options:                                                              │
│    [1] Wait for Stripe to resolve                                      │
│    [2] Increase timeout from 30s → 60s (temporary)                     │
│    [3] Enable fallback payment processor                               │
│                                                                        │
└───────────────────────────────────────────────────────────────────────┘
          │
          ▼
┌───────────────────────────────────────────────────────────────────────┐
│ PHASE 4: LEARNING (at session end)                                     │
└───────────────────────────────────────────────────────────────────────┘

When you exit or after idle timeout:

┌───────────────────┐
│  Session Summary  │  ← LLM call with structured output
└─────────┬─────────┘
          │
          │  Extracts:
          │  {
          │    "issue": "payment-service timeout to Stripe",
          │    "root_cause": "Stripe API degradation",
          │    "resolution": "waited / increased timeout",
          │    "components": ["payment-service", "stripe"],
          │    "tags": ["timeout", "external-dependency"]
          │  }
          │
          ▼
┌───────────────────┐
│  Memory Store     │
└───────────────────┘

Next time: memory_search("stripe timeout") returns this session.
```

### What Joe Does vs What LLM Does

| Responsibility | Joe | LLM |
|----------------|-----|-----|
| Parse user intent | ✗ | ✓ |
| Decide what to investigate | ✗ | ✓ |
| Execute tool calls | ✓ | ✗ |
| Maintain infrastructure graph | ✓ | ✗ |
| Store/search memory | ✓ | ✗ |
| Safety controls (approval, dry-run) | ✓ | ✗ |
| Synthesize findings | ✗ | ✓ |
| Summarize sessions for memory | ✗ | ✓ |

**Joe is the hands and memory. LLM is the brain.**

---

## How the Graph is Built (Discovery)

**The graph is built collaboratively between Joe, the LLM, and you.**

Joe can pull raw inventory from APIs (what exists), but understanding relationships and semantics requires conversation.

### Two Layers

```
┌─────────────────────────────────────────────────────────────────────┐
│  LAYER 1: Inventory (Deterministic)                                  │
│                                                                      │
│  What Joe CAN reliably extract from APIs:                            │
│  - "This deployment exists"                                          │
│  - "This configmap exists"                                           │
│  - "This deployment mounts this configmap" (explicit ref in spec)    │
│  - "This ArgoCD app points to this git repo"                         │
│  - "This service selects these pods"                                 │
│                                                                      │
│  Raw facts. No interpretation needed.                                │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  LAYER 2: Semantics (LLM + Conversation)                             │
│                                                                      │
│  What needs interpretation:                                          │
│  - "payment-service depends on user-service" (implicit, from code)   │
│  - "This is the checkout flow" (business logic)                      │
│  - "kafka-topic-orders connects order-service to fulfillment"        │
│  - "This custom CRD represents a database connection"                │
│  - "*.payments.svc.cluster.local is the payments tier"               │
│  - "fraud-detector runs before payment-service in the flow"          │
│                                                                      │
│  Built through onboarding + ongoing conversation.                    │
└─────────────────────────────────────────────────────────────────────┘
```

### Why Hybrid?

Real infrastructure is messy:
- Custom CRDs with non-standard semantics
- Naming conventions only your team knows
- Implicit dependencies not declared anywhere
- Services that communicate through message queues, not direct HTTP
- Business logic relationships ("this is the checkout flow")

Trying to deterministically parse all of that is a losing battle. The LLM + you fill in what APIs can't tell us.

---

## Phased Onboarding

Onboarding is optimized to minimize LLM usage while maximizing discovery quality.

```
┌─────────────────────────────────────────────────────────────────────┐
│  PHASE 1: Collect (User provides data, no LLM)                       │
│                                                                      │
│  Goal: Get as much structured input as possible from the user        │
│  LLM cost: Zero                                                      │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  PHASE 2: Parse & Validate (Deterministic + minimal LLM)             │
│                                                                      │
│  Goal: Validate URLs, check auth, extract obvious sources            │
│  LLM cost: Low (only for ambiguous cases)                            │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  PHASE 3: Explore (Timeboxed LLM discovery)                          │
│                                                                      │
│  Goal: Fill gaps, discover relationships                             │
│  LLM cost: Bounded by time/token budget                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Phase 1: Collect

Structured prompts gather maximum information with zero LLM cost.

```
$ joe init

══════════════════════════════════════════════════════════════════════
 Phase 1: Tell me about your infrastructure
══════════════════════════════════════════════════════════════════════

The more you tell me now, the faster I can get started.

KUBERNETES
──────────
Kubeconfig path [~/.kube/config]: 
Contexts to monitor (comma-separated, or 'all'): prod-us, staging-us
App namespaces (skip infra like kube-system): payments, orders, inventory

GITOPS
──────
Do you use ArgoCD or Flux? [argocd/flux/none]: argocd
ArgoCD URL: https://argocd.company.com
ArgoCD auth token: ****

GIT REPOSITORIES
────────────────
Repo URLs or local paths (one per line, empty line to finish):
> https://gitlab.prod.company.com/infra/platform-ops
> https://gitlab.prod.company.com/infra/app-configs
> ~/code/services/payment-service
> 

TELEMETRY
─────────
Prometheus URL (if any): https://prometheus.prod.company.com
Grafana URL (if any): https://grafana.prod.company.com
Loki URL (if any): https://loki.prod.company.com

MESSAGING
─────────
Kafka brokers (if any): kafka.prod.company.com:9092

DATABASES
─────────
PostgreSQL hosts: postgres.prod.company.com
Other databases: redis.prod.company.com (Redis)

EXTERNAL SERVICES
─────────────────
External APIs your services call: 
> api.stripe.com (payments)
> api.twilio.com (notifications)
> 

ENVIRONMENTS
────────────
What environments exist? prod, staging, dev
Naming patterns (how to identify env in URLs/names): 
  prod: *.prod.company.com
  staging: *.staging.company.com

ANYTHING ELSE?
──────────────
Any other context about your setup?
> We use Istio service mesh
> Kafka topics follow pattern: {service}-{event}-{version}
> 
```

**Output:** Structured data stored immediately in `onboarding_input` table.

### Phase 2: Parse & Validate

Deterministic validation with minimal LLM involvement.

```
══════════════════════════════════════════════════════════════════════
 Phase 2: Validating connections
══════════════════════════════════════════════════════════════════════

Checking Kubernetes...
  ├─ prod-us: ✓ connected (52 deployments in app namespaces)
  └─ staging-us: ✓ connected (48 deployments in app namespaces)

Checking ArgoCD...
  └─ argocd.company.com: ✓ connected (23 applications)

Checking Git repos...
  ├─ gitlab.prod.company.com/infra/platform-ops: ✓ cloned
  │   └─ Found .joe/manifest.yaml ← Joe helper file!
  ├─ gitlab.prod.company.com/infra/app-configs: ✓ cloned
  └─ ~/code/services/payment-service: ✓ accessible

Checking telemetry...
  ├─ prometheus.prod.company.com: ✓ connected (247 active targets)
  ├─ grafana.prod.company.com: ⚠ needs API key for dashboards
  └─ loki.prod.company.com: ✓ connected

Checking messaging...
  └─ kafka.prod.company.com:9092: ✓ connected (34 topics)

──────────────────────────────────────────────────────────────────────
Questions:

1. Grafana API key for dashboard discovery? [paste or skip]: skip
```

**Deterministic work:**
- HTTP HEAD requests to validate URLs
- K8s API connection test
- Git clone/pull
- Kafka admin client to list topics
- **Parse .joe/ helper files (no LLM needed!)**

**Minimal LLM:** Only for truly ambiguous cases.

### Phase 3: Explore (Timeboxed)

LLM fills gaps with bounded time and token budget.

```
══════════════════════════════════════════════════════════════════════
 Phase 3: Building infrastructure graph
══════════════════════════════════════════════════════════════════════

Time budget: 2 minutes
Token budget: 50k

Scanning Kubernetes resources...
  ├─ Mapping deployments to ArgoCD apps
  ├─ Extracting service dependencies from configs
  └─ Identifying cross-namespace calls

Reading git repos for infrastructure details...
  ├─ platform-ops: Reading .joe/manifest.yaml (pre-defined!)
  ├─ app-configs: Parsing helm values files
  └─ payment-service: Analyzing code for external calls

Querying Prometheus for service topology...
  └─ Extracting service-to-service call patterns

[████████████████████░░░░] 85% - 1:42 elapsed

──────────────────────────────────────────────────────────────────────
Discovery complete.

Graph summary:
  - 52 deployments
  - 34 kafka topics  
  - 23 ArgoCD apps
  - 156 service relationships
  - 12 external dependencies

Questions I couldn't answer (will ask later as needed):
  - What is 'legacy-adapter' service for?
  - Is 'batch-processor' part of any user flow?

══════════════════════════════════════════════════════════════════════
 Ready! Run 'joe' to start chatting.
══════════════════════════════════════════════════════════════════════
```

---

## .joe/ Helper Files

Convention for pre-digested infrastructure metadata. Like `.github/`, `.vscode/`, but for Joe.

### The Key Insight: LLM-to-LLM Cache

`.joe/` files are **not a format Joe parses with code**. They're a cache of understanding that one LLM (Claude Code, Cursor) creates for another LLM (Joe's reasoning engine) to consume.

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  .joe/ files = "LLM-to-LLM cache"                                    │
│                                                                      │
│  Created by: Coding LLM (Claude Code, Cursor, Copilot)               │
│  Consumed by: Joe's LLM                                              │
│                                                                      │
│  Purpose: Skip the expensive "understand codebase" step              │
│           Joe's LLM still interprets and executes tool calls         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Why LLM parsing instead of code parsing?**

| Factor | Code Parsing | LLM Parsing |
|--------|--------------|-------------|
| Maintenance | High (schema coupling) | Zero |
| Schema evolution | Breaking changes | Graceful handling |
| Format variations | Crashes | Handles naturally |
| Code paths | Two (with/without .joe/) | One (always LLM) |
| Malformed files | Error | Best effort |

The `.joe/` format can evolve freely. Old Joe reads new format, new Joe reads old format—the LLM adapts.

### Cost Comparison

```
┌─────────────────────────────────────────────────────────────────────┐
│  WITHOUT .joe/ files                                                 │
│                                                                      │
│  LLM work:                                                           │
│    1. Read source code files (10-50 files)         ~5000 tokens     │
│    2. Understand code patterns                      ~2000 tokens     │
│    3. Identify infrastructure references            ~1000 tokens     │
│    4. Infer relationships                           ~1000 tokens     │
│    5. Generate structured output                    ~1000 tokens     │
│                                                     ─────────────    │
│    Total: ~10,000 tokens, 30-60 seconds                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│  WITH .joe/ files                                                    │
│                                                                      │
│  LLM work:                                                           │
│    1. Read .joe/ files (3-5 small files)           ~500 tokens      │
│    2. Interpret and execute tool calls             ~500 tokens      │
│                                                     ─────────────    │
│    Total: ~1,000 tokens, 2-3 seconds                                 │
│                                                                      │
│  90% reduction in cost and latency                                   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Hybrid Approach: Hash-Based Caching

To eliminate redundant LLM calls, Joe caches interpretations:

```
┌─────────────────────────────────────────────────────────────────────┐
│  Joe encounters a repo with .joe/ files:                             │
│                                                                      │
│  1. Compute hash of .joe/ directory contents                         │
│                                                                      │
│  2. Check cache: Have we interpreted this exact version before?      │
│     │                                                                │
│     ├─ YES: Replay cached tool calls                                │
│     │       • No LLM call                                           │
│     │       • Instant (~50ms)                                       │
│     │       • Works offline                                          │
│     │                                                                │
│     └─ NO: Send to LLM for interpretation                           │
│            • LLM reads .joe/ files                                  │
│            • LLM executes register_source(), graph_add_edge(), etc. │
│            • Joe caches the tool calls for next time                │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

**Cache schema:**

```sql
CREATE TABLE joe_file_cache (
    repo_id TEXT,                -- "gitlab/company/payment-service"
    joe_dir_hash TEXT,           -- SHA256 of .joe/ directory contents
    tool_calls JSON,             -- [{tool: "register_source", args: {...}}, ...]
    cached_at TIMESTAMP,
    llm_model TEXT,              -- "claude-sonnet-4" (invalidate if model changes significantly)
    PRIMARY KEY (repo_id, joe_dir_hash)
);
```

**Cache invalidation:**
- `.joe/` files change → hash changes → re-interpret
- Major LLM model change → optionally re-interpret all
- Manual: `joe cache clear`

### Single Code Path

With LLM parsing, Joe has one unified flow:

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  Repo with .joe/ files:                                              │
│    .joe/ files ──► LLM interprets ──► Tool calls ──► Update state   │
│                                                                      │
│  Repo without .joe/ files:                                           │
│    Source code ──► LLM analyzes ──► Tool calls ──► Update state     │
│                                                                      │
│  Same downstream path, different input to LLM                        │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

No special parsing code. No schema versioning. No format migration.

### Directory Structure

```
my-repo/
├── .joe/
│   ├── manifest.yaml     # What this repo is and what it contains
│   ├── sources.yaml      # Infrastructure sources referenced
│   ├── topology.yaml     # Service relationships
│   ├── flows.yaml        # Business flows (optional)
│   └── glossary.yaml     # Domain terminology (optional)
├── helm/
│   └── values.yaml
└── src/
    └── ...
```

**Critical rule:** `.joe/` files contain **concepts and pointers**, not values. They tell Joe "look here for X" rather than duplicating config values.

### .joe/manifest.yaml

```yaml
# What this repository is
joe_version: "1.0"

repo:
  type: helm_chart           # helm_chart | kustomize | terraform | application | library
  name: payment-service
  description: "Handles payment processing via Stripe"
  team: payments
  
# Where to find configuration
config:
  helm_values:
    - path: helm/values.yaml
      description: "Base values"
    - path: helm/values-prod.yaml
      description: "Production overrides"
      
# What environments this repo deploys to
environments:
  - name: prod
    values_file: helm/values-prod.yaml
  - name: staging
    values_file: helm/values-staging.yaml
```

### .joe/sources.yaml

```yaml
# Infrastructure sources this repo references
# NO VALUES - only types and pointers

sources:
  - type: postgresql
    reference: "database connection"
    config_location: helm/values.yaml
    config_path: "database.host"         # Where the actual value lives
    environments: [prod, staging]
    used_by: [payment-service, payment-worker]
    
  - type: kafka
    reference: "message broker"
    config_location: helm/values.yaml
    config_path: "kafka.brokers"
    topics:
      - name_pattern: "payment-*"
        defined_in: src/events/topics.go
        
  - type: external_api
    name: Stripe
    reference: "payment processor"
    config_location: helm/values.yaml
    config_path: "stripe.apiUrl"
```

### .joe/topology.yaml

```yaml
# Service relationships - how this service connects to others

this_service: payment-service

calls:
  - service: fraud-detector
    protocol: http
    purpose: "sync fraud check before payment"
    endpoint_defined_in: src/clients/fraud.go
    
  - service: user-service
    protocol: grpc
    purpose: "fetch user payment methods"

publishes_to:
  - type: kafka
    topic_pattern: "payment-events-v*"
    event_types: [PaymentCompleted, PaymentFailed]
    schema_location: src/events/schemas/

subscribes_to:
  - type: kafka
    topic_pattern: "refund-requested-*"
    handler_location: src/handlers/refund_handler.go

external_calls:
  - name: Stripe
    client_location: src/clients/stripe/
```

### .joe/flows.yaml (optional)

```yaml
# Business flows this service participates in

participates_in:
  - flow: checkout
    role: "process payment"
    position: 3
    receives_from: cart-service
    passes_to: order-service
    
  - flow: refund
    role: "process refund via Stripe"
    triggered_by: kafka/refunds-requested
    publishes: kafka/refund-completed
```

### .joe/glossary.yaml (optional)

```yaml
# Domain terms specific to this service/repo

terms:
  PCI:
    meaning: "Payment Card Industry compliance"
    relevance: "This service handles card data"
    
  idempotency_key:
    meaning: "Unique key to prevent duplicate payments"
    implementation: src/middleware/idempotency.go
```

### How Joe Processes .joe/ Files

```
┌─────────────────────────────────────────────────────────────────────┐
│  Joe accesses repo with .joe/ files:                                 │
│                                                                      │
│  1. Compute hash of .joe/ directory                                  │
│     hash = SHA256(.joe/manifest.yaml + .joe/sources.yaml + ...)     │
│                                                                      │
│  2. Check cache                                                      │
│     SELECT tool_calls FROM joe_file_cache                           │
│     WHERE repo_id = ? AND joe_dir_hash = ?                          │
│                                                                      │
│  3a. Cache HIT:                                                      │
│      Execute cached tool calls directly                              │
│      → register_source(...), graph_add_edge(...), etc.              │
│      No LLM needed. Instant.                                         │
│                                                                      │
│  3b. Cache MISS:                                                     │
│      Send .joe/ files to LLM:                                        │
│                                                                      │
│      "Here are the .joe/ files for payment-service repo.            │
│       Interpret them and call the appropriate tools to register      │
│       sources and update the graph."                                 │
│                                                                      │
│      LLM responds with tool calls:                                   │
│      [register_source(type="postgresql", ...)]                       │
│      [register_source(type="kafka", ...)]                            │
│      [graph_add_edge(from="payment-service", to="fraud-detector")]  │
│      ...                                                             │
│                                                                      │
│      Joe executes tool calls AND caches them for next time          │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Workflow Summary

```
┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  Developer in repo                                                   │
│       │                                                              │
│       │ Runs Claude Code / Cursor with joe-prompt.md                │
│       ▼                                                              │
│  Coding LLM analyzes source code                                     │
│       │                                                              │
│       │ Generates .joe/ files (expensive work, done once)           │
│       ▼                                                              │
│  Developer commits .joe/ files                                       │
│       │                                                              │
│       │                                                              │
│       ▼                                                              │
│  Joe encounters repo                                                 │
│       │                                                              │
│       ├─► .joe/ exists?                                             │
│       │   │                                                          │
│       │   ├─► YES + cache hit: Replay tool calls (instant)          │
│       │   ├─► YES + cache miss: LLM interprets .joe/ (fast, cheap)  │
│       │   └─► NO: LLM analyzes source code (slow, expensive)        │
│       │                                                              │
│       ▼                                                              │
│  Sources registered, graph updated                                   │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Benefits Recap

| Scenario | LLM Cost | Latency | Offline |
|----------|----------|---------|---------|
| No .joe/, first scan | ~10k tokens | 30-60s | No |
| With .joe/, first scan | ~1k tokens | 2-3s | No |
| With .joe/, cached | 0 tokens | ~50ms | Yes |

The `.joe/` convention turns expensive code analysis into cheap file interpretation, and caching makes repeated access instant.

---

## Sources vs Graph

Infrastructure sources are stored separately from the graph for rebuild capability.

### The Distinction

```
┌─────────────────────────────────────────────────────────────────────┐
│  SOURCES (persistent, survives rebuild)                              │
│                                                                      │
│  "Where Joe connects to get information"                             │
│                                                                      │
│  - gitlab.mycompany.com (GitLab instance)                            │
│  - argocd.company.com (ArgoCD instance)                              │
│  - ~/.kube/config:prod-us (K8s cluster)                              │
│  - prometheus.monitoring.svc (Prometheus)                            │
│                                                                      │
│  Stored in: SQL table                                                │
│  Lifecycle: Persist until explicitly removed                         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              │ Joe queries sources
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  GRAPH (discovered, rebuildable)                                     │
│                                                                      │
│  "What Joe found by querying sources"                                │
│                                                                      │
│  - gitlab.mycompany.com/myapp_ops (a specific repo)                  │
│  - deployment/payments/payment-service (a k8s resource)              │
│  - argocd-app/payments-app (an ArgoCD application)                   │
│  - kafka/refunds-requested (a topic)                                 │
│                                                                      │
│  Stored in: Graph DB                                                 │
│  Lifecycle: Can be rebuilt from sources + onboarding facts           │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Sources Schema

```sql
CREATE TABLE sources (
    id TEXT PRIMARY KEY,           -- "prometheus/prometheus.prod.company.com"
    type TEXT NOT NULL,            -- "prometheus", "gitlab", "kubernetes", etc.
    url TEXT,
    name TEXT,                     -- Human-friendly: "Prometheus Prod"
    
    -- Classification
    environment TEXT,              -- "prod", "staging", "dev"
    categories JSON,               -- ["telemetry", "metrics"]
    
    -- Connection
    connection_details JSON,       -- Type-specific config
    status TEXT,                   -- "connected", "needs_auth", "unreachable"
    last_connected TIMESTAMP,
    
    -- Discovery lineage
    discovered_from TEXT,          -- "user_input" or source_id
    discovery_context TEXT,        -- "Found in values-prod.yaml"
    
    metadata JSON,
    created_at TIMESTAMP
);

CREATE TABLE source_secrets (
    source_id TEXT PRIMARY KEY REFERENCES sources(id),
    secret_type TEXT,              -- "token", "ssh_key", "password"
    encrypted_value BLOB
);
```

### Source Discovery via LLM

When user provides a starting point, LLM discovers sources using tools:

```
User: "https://gitlab.prod.company.com/infra/some_repo_ops"
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  LLM explores repo, finds infrastructure references                  │
│                                                                      │
│  [tool_call: register_source(                                        │
│    type: "prometheus",                                               │
│    url: "https://prometheus.prod.company.com",                       │
│    name: "Prometheus Prod",                                          │
│    environment: "prod",                                              │
│    categories: ["telemetry", "metrics"],                             │
│    discovered_from: "git_repo/some_repo_ops",                        │
│    discovery_context: "Found in helm/values-prod.yaml at path       │
│                        prometheus.url"                               │
│  )]                                                                  │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│  Joe validates URL (HTTP check) then stores in sources table         │
└─────────────────────────────────────────────────────────────────────┘
```

### Rebuild Process

```
$ joe rebuild-graph

Step 1: Clear graph tables (nodes, edges)
        Sources and onboarding_facts remain intact

Step 2: For each source in sources table:
        ├─ k8s/prod-us → query k8s API, discover resources
        ├─ argocd/argocd.company.com → query ArgoCD, discover apps
        ├─ gitlab/gitlab.mycompany.com → scan repos (use .joe/ files!)
        └─ ... 

Step 3: Replay onboarding_facts through LLM:
        For each fact, re-execute graph operations

Step 4: Done. Graph rebuilt without re-onboarding.
```

---

## Edge Confidence Levels

The graph tracks how relationships were discovered:

```go
type Edge struct {
    From       string
    To         string
    Relation   string
    Confidence ConfidenceLevel
    Source     string           // "k8s_api", "joe_file", "llm_inference", "user_confirmed"
    CreatedAt  time.Time
}

type ConfidenceLevel int
const (
    Explicit      ConfidenceLevel = 3  // From API or .joe/ file
    UserConfirmed ConfidenceLevel = 3  // User said "yes"
    Inferred      ConfidenceLevel = 1  // LLM guessed, not yet confirmed
)
```

### What Triggers Graph Updates

| Trigger | What Happens |
|---------|--------------|
| `joe init` | Bootstrap via phased onboarding |
| Normal conversation | LLM notices gaps, asks questions, you confirm |
| You mention something new | "We also have a Redis cache" → added to graph |
| LLM infers relationship | "Based on this error, X probably calls Y" → asks to confirm |
| Background refresh | New resources found → queues clarification notification |
| .joe/ file change | Repo commit with updated .joe/ files → deterministic update |

---

## Joe's Operating Modes

Joe isn't just a CLI you invoke. It's a daemon that runs continuously.

```
┌─────────────────────────────────────────────────────────────────────┐
│                       Joe Runtime Architecture                       │
└─────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────┐
│                                                                      │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                      Joe Daemon (always running)              │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │  │
│  │  │  Inventory  │  │   Graph     │  │   Watch     │           │  │
│  │  │  Refresh    │  │   Store     │  │   Loop      │           │  │
│  │  │  (periodic) │  │  (Cayley)   │  │  (k8s,argo) │           │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │  │
│  │                                                               │  │
│  │  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐           │  │
│  │  │  Anomaly    │  │ Notification│  │   Memory    │           │  │
│  │  │  Detector   │  │   Queue     │  │   Store     │           │  │
│  │  └─────────────┘  └─────────────┘  └─────────────┘           │  │
│  │                                                               │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                         │              │                             │
│            ┌────────────┘              └────────────┐                │
│            ▼                                        ▼                │
│  ┌───────────────────┐                    ┌───────────────────┐     │
│  │   CLI (joe)       │                    │   Push Channels   │     │
│  │   Interactive     │                    │                   │     │
│  │   sessions        │                    │  • Desktop notif  │     │
│  └───────────────────┘                    │  • Slack/Teams    │     │
│                                           │  • Web UI         │     │
│  ┌───────────────────┐                    │  • Terminal bell  │     │
│  │   Web UI          │                    └───────────────────┘     │
│  │   (future)        │                                              │
│  └───────────────────┘                                              │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

### Three Modes of Operation

| Mode | Description |
|------|-------------|
| **Interactive** | You run `joe` and have a conversation |
| **Background** | Daemon watches infrastructure, updates graph silently |
| **Push** | Joe interrupts you because something needs attention |

---

## Push Notifications

Joe pushes to you in two cases:

### 1. Graph Clarification Needed

Joe found something it can't interpret without your help.

```
┌─────────────────────────────────────────────────────────────────────┐
│  🔔 Joe needs input                                                  │
│                                                                      │
│  I discovered a new deployment in the payments namespace:            │
│  "payment-fraud-detector" (3 replicas)                               │
│                                                                      │
│  Questions:                                                          │
│  • Is this part of the checkout flow?                                │
│  • Does it depend on payment-service or the other way around?        │
│  • What external services does it call (if any)?                     │
│                                                                      │
│  [Answer now]  [Remind me later]  [Ignore]                          │
└─────────────────────────────────────────────────────────────────────┘
```

### 2. Attention Required

Joe detected something you should know about.

```
┌─────────────────────────────────────────────────────────────────────┐
│  ⚠️  Joe detected an issue                                           │
│                                                                      │
│  payment-service error rate jumped from 0.1% to 4.2%                 │
│  Started: 3 minutes ago                                              │
│                                                                      │
│  Preliminary analysis:                                               │
│  • Correlates with ArgoCD sync of payments-app (3 min ago)          │
│  • Recent commit: "Update Stripe SDK to v4.0" by alice@company.com   │
│  • No Stripe status page incidents                                   │
│                                                                      │
│  [Investigate]  [Acknowledge]  [Mute for 1h]                        │
└─────────────────────────────────────────────────────────────────────┘
```

---

## Notification Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                     Notification Flow                                │
└─────────────────────────────────────────────────────────────────────┘

  Event Sources                     Processing                 Delivery
  ─────────────                     ──────────                 ────────

┌─────────────┐                 ┌─────────────────┐
│ K8s Watch   │────────────────▶│                 │
│ (new deploy,│                 │                 │
│  pod crash) │                 │                 │
└─────────────┘                 │                 │
                                │   Notification  │      ┌─────────────┐
┌─────────────┐                 │   Processor     │─────▶│ Desktop     │
│ ArgoCD      │────────────────▶│                 │      │ (notify-send│
│ (sync,      │                 │   • Dedupe      │      │  or osascript)
│  health)    │                 │   • Throttle    │      └─────────────┘
└─────────────┘                 │   • Prioritize  │
                                │   • Route       │      ┌─────────────┐
┌─────────────┐                 │                 │─────▶│ Slack       │
│ Metrics     │────────────────▶│                 │      │ (webhook)   │
│ (threshold  │                 │                 │      └─────────────┘
│  breach)    │                 │                 │
└─────────────┘                 │                 │      ┌─────────────┐
                                │                 │─────▶│ Web UI      │
┌─────────────┐                 │                 │      │ (websocket) │
│ Graph       │────────────────▶│                 │      └─────────────┘
│ (new node,  │                 │                 │
│  uncertain  │                 │                 │      ┌─────────────┐
│  edge)      │                 └─────────────────┘─────▶│ CLI         │
└─────────────┘                                          │ (if active) │
                                                         └─────────────┘
```

### Notification Types

```go
type Notification struct {
    ID        string
    Type      NotificationType
    Priority  Priority
    Title     string
    Body      string
    Context   map[string]any    // Related nodes, metrics, etc.
    Actions   []Action          // What user can do
    CreatedAt time.Time
    ExpiresAt time.Time         // Auto-dismiss after
    Status    NotificationStatus
}

type NotificationType string
const (
    GraphClarification  NotificationType = "graph_clarification"
    AnomalyDetected     NotificationType = "anomaly_detected"
    IncidentLikely      NotificationType = "incident_likely"
    DeploymentComplete  NotificationType = "deployment_complete"
    SyncFailed          NotificationType = "sync_failed"
)

type Priority int
const (
    Low      Priority = 1  // FYI, no rush
    Medium   Priority = 2  // Should look at today
    High     Priority = 3  // Should look soon
    Urgent   Priority = 4  // Interrupt me now
)

type Action struct {
    Label   string
    Command string  // What Joe does if clicked
}
```

### Delivery Channels

```yaml
# ~/.joe/config.yaml

notifications:
  channels:
    - type: desktop
      enabled: true
      priority_threshold: medium  # Only medium+ goes to desktop
      
    - type: slack
      enabled: true
      webhook_url: ${SLACK_WEBHOOK}
      priority_threshold: high    # Only high+ goes to Slack
      channel: "#platform-alerts"
      
    - type: web
      enabled: true
      priority_threshold: low     # Everything goes to web UI
      
  quiet_hours:
    enabled: true
    start: "22:00"
    end: "08:00"
    timezone: "Europe/Madrid"
    exceptions: [urgent]  # Urgent still comes through
    
  throttle:
    max_per_hour: 10
    cooldown_same_issue: 15m
```

---

## Background Graph Updates

The daemon continuously watches and updates:

```
┌─────────────────────────────────────────────────────────────────────┐
│                   Background Update Loop                             │
└─────────────────────────────────────────────────────────────────────┘

Every 5 minutes (configurable):

  1. Inventory Refresh
     ├─ K8s: List all resources, compare to graph
     ├─ ArgoCD: List apps, check sync status
     └─ Git: Check for new commits in tracked repos

  2. Diff Detection
     ├─ New resources? → Add nodes (confidence: explicit)
     ├─ Removed resources? → Mark nodes stale
     ├─ Changed resources? → Update metadata
     └─ New relationships detected? → Add edges (confidence: inferred)

  3. Decision: Notify or Silent?
     │
     ├─ New node, known pattern → Silent (just add to graph)
     │   Example: New pod in existing deployment
     │
     ├─ New node, unknown pattern → Queue clarification
     │   Example: New deployment with unfamiliar name
     │
     ├─ Edge changed, explicit → Silent
     │   Example: ConfigMap reference updated
     │
     └─ Edge inferred, low confidence → Queue clarification
         Example: "I think X calls Y based on this metric"

  4. Anomaly Check
     ├─ Error rate spike? → Queue notification
     ├─ Latency increase? → Queue notification
     ├─ Resource exhaustion? → Queue notification
     └─ Sync failure? → Queue notification
```

### Inference Confidence

When Joe infers relationships, it tracks confidence:

```go
// Joe sees new deployment "fraud-detector" in payments namespace
node := Node{
    ID:        "deployment/payments/fraud-detector",
    Type:      "deployment",
    Namespace: "payments",
    Confidence: Explicit,  // Definitely exists, from K8s API
}

// Joe guesses it might be related to payment-service
edge := Edge{
    From:       "deployment/payments/fraud-detector",
    To:         "deployment/payments/payment-service",
    Relation:   "related_to",  // Vague until confirmed
    Confidence: Inferred,
    Source:     "namespace_proximity",  // Why Joe thinks this
}

// Queue notification to confirm
notification := Notification{
    Type:     GraphClarification,
    Priority: Low,
    Title:    "New deployment discovered",
    Body:     "Is fraud-detector related to payment-service?",
    Actions: []Action{
        {Label: "Yes, it calls payment-service", Command: "confirm_edge --from=fraud-detector --to=payment-service --relation=calls"},
        {Label: "Yes, payment-service calls it", Command: "confirm_edge --from=payment-service --to=fraud-detector --relation=calls"},
        {Label: "No relationship", Command: "delete_edge --id=..."},
        {Label: "Let me explain", Command: "open_chat --context=fraud-detector"},
    },
}
```

---

## CLI vs Daemon

```bash
# Start the daemon (runs in background)
$ joe daemon start
Joe daemon started (PID 12345)
Watching: 3 k8s clusters, 2 ArgoCD instances, 5 git repos

# Check daemon status
$ joe daemon status
Running since: 2h ago
Graph: 342 nodes, 891 edges
Pending notifications: 2
Last refresh: 30s ago

# Interactive session (talks to daemon)
$ joe
Connected to Joe daemon.

> what's happening with payment-service?
...

# Stop daemon
$ joe daemon stop
```

The CLI is a **client** that connects to the daemon. The daemon holds the graph, watches infrastructure, and queues notifications. The CLI (or web UI) is just a way to interact.

---

## Updated Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Joe Architecture                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                      │
│  ┌───────────────────────────────────────────────────────────────┐  │
│  │                     Joe Daemon (joedaemon)                    │  │
│  │                                                               │  │
│  │  ┌─────────────────────────────────────────────────────────┐ │  │
│  │  │                    Core Services                        │ │  │
│  │  │  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────────┐   │ │  │
│  │  │  │  Graph   │ │  Memory  │ │ Inventory│ │ Notifier │   │ │  │
│  │  │  │  Store   │ │  Store   │ │ Watcher  │ │          │   │ │  │
│  │  │  └──────────┘ └──────────┘ └──────────┘ └──────────┘   │ │  │
│  │  └─────────────────────────────────────────────────────────┘ │  │
│  │                                                               │  │
│  │  ┌─────────────────────────────────────────────────────────┐ │  │
│  │  │                    gRPC/HTTP API                        │ │  │
│  │  │  • Chat (streaming)                                     │ │  │
│  │  │  • Graph queries                                        │ │  │
│  │  │  • Tool execution                                       │ │  │
│  │  │  • Notification management                              │ │  │
│  │  └─────────────────────────────────────────────────────────┘ │  │
│  └───────────────────────────────────────────────────────────────┘  │
│                              │                                       │
│         ┌────────────────────┼────────────────────┐                 │
│         ▼                    ▼                    ▼                 │
│  ┌─────────────┐      ┌─────────────┐      ┌─────────────┐         │
│  │   CLI       │      │   Web UI    │      │   Slack     │         │
│  │   (joe)     │      │   (future)  │      │   Bot       │         │
│  └─────────────┘      └─────────────┘      └─────────────┘         │
│                                                                      │
└─────────────────────────────────────────────────────────────────────┘
```

Does this capture what you're thinking?

```
┌─────────────────────────────────────────────────────────────────────────┐
│                           Joe Architecture                               │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                         User Interface                           │    │
│  │  ┌─────────┐  ┌─────────┐  ┌─────────┐                          │    │
│  │  │   CLI   │  │   TUI   │  │   Web   │  (future)                │    │
│  │  └─────────┘  └─────────┘  └─────────┘                          │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                      Agentic Loop                                │    │
│  │                                                                  │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │    │
│  │  │   Prompt     │  │    Tool      │  │   Response   │           │    │
│  │  │   Builder    │  │   Executor   │  │   Handler    │           │    │
│  │  └──────────────┘  └──────────────┘  └──────────────┘           │    │
│  │                                                                  │    │
│  │  ┌──────────────┐                                               │    │
│  │  │   Session    │  User msg → LLM → tool calls → LLM → ...      │    │
│  │  │   Manager    │                                               │    │
│  │  └──────────────┘                                               │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│            ┌───────────────────────┼───────────────────────┐            │
│            ▼                       ▼                       ▼            │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────────┐       │
│  │   LLM Adapter    │  │   Graph Store    │  │  Memory Store    │       │
│  │   (Interface)    │  │   (Cayley)       │  │   (SQLite)       │       │
│  │                  │  │                  │  │                  │       │
│  │  ┌────────────┐  │  │  - Infra nodes   │  │  - Past sessions │       │
│  │  │  Claude    │  │  │  - Relationships │  │  - Embeddings    │       │
│  │  ├────────────┤  │  │  - Traversals    │  │                  │       │
│  │  │  OpenAI    │  │  │                  │  │                  │       │
│  │  ├────────────┤  │  │                  │  │                  │       │
│  │  │  Gemini    │  │  │                  │  │                  │       │
│  │  ├────────────┤  │  │                  │  │                  │       │
│  │  │  Ollama    │  │  │                  │  │                  │       │
│  │  └────────────┘  │  │                  │  │                  │       │
│  └──────────────────┘  └──────────────────┘  └──────────────────┘       │
│                                    │                                     │
│                                    ▼                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                      Discovery Layer                             │    │
│  │                                                                  │    │
│  │  Runs periodically (or on-demand), builds infrastructure graph   │    │
│  │  NO LLM involved — deterministic parsing of APIs and configs     │    │
│  │                                                                  │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                      Executor Layer                              │    │
│  │                                                                  │    │
│  │  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐           │    │
│  │  │   Safety     │  │   Approval   │  │   Audit      │           │    │
│  │  │   Policy     │  │   Handler    │  │   Logger     │           │    │
│  │  └──────────────┘  └──────────────┘  └──────────────┘           │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                    │                                     │
│  ┌─────────────────────────────────────────────────────────────────┐    │
│  │                      Adapter Layer (Tools)                       │    │
│  │                                                                  │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐        │    │
│  │  │  K8s   │ │ ArgoCD │ │  Git   │ │  Helm  │ │ Prom   │        │    │
│  │  └────────┘ └────────┘ └────────┘ └────────┘ └────────┘        │    │
│  │                                                                  │    │
│  │  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐                   │    │
│  │  │ Loki   │ │  HTTP  │ │  MCP   │ │ Custom │                   │    │
│  │  │        │ │        │ │ Bridge │ │        │                   │    │
│  │  └────────┘ └────────┘ └────────┘ └────────┘                   │    │
│  └─────────────────────────────────────────────────────────────────┘    │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## What Makes This Different

| Aspect | Claude Code / Copilot | Joe |
|--------|----------------------|-----|
| **Scope** | Single repo | Entire distributed system |
| **Memory** | None between sessions | Learns from incidents |
| **Live data** | None | Queries infra in real-time |
| **Pre-enrichment** | None | Gathers context before LLM |
| **AI lock-in** | Claude only / OpenAI only | Swappable backends |
| **Actions** | File edits | kubectl, argocd, git, etc. |

The key innovation: **Joe does work before and after the LLM call.** 

- Before: builds context from graph + memory + live queries
- After: learns patterns for next time

The LLM is the reasoning engine, but Joe is the system that makes it useful for infrastructure.

---

## Memory Deep Dive

### Types of Memory

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Joe's Memory System                              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  1. INFRASTRUCTURE GRAPH (ephemeral, rebuilt)                            │
│     ─────────────────────────────────────────                            │
│     What: Current state of your systems                                  │
│     Lifespan: Refreshed every 5 min (configurable)                       │
│     Storage: In-memory graph + SQLite checkpoint                         │
│                                                                          │
│  2. CONVERSATION MEMORY (persistent)                                     │
│     ────────────────────────────────                                     │
│     What: Past investigation sessions                                    │
│     Lifespan: Permanent (with optional retention policy)                 │
│     Storage: SQLite + embeddings for semantic search                     │
│     Contains:                                                            │
│       - Issue summary                                                    │
│       - Root cause                                                       │
│       - Resolution steps                                                 │
│       - Components involved                                              │
│       - Tags for search                                                  │
│                                                                          │
│  3. LEARNED PATTERNS (persistent, derived)                               │
│     ─────────────────────────────────────                                │
│     What: Extracted heuristics from many sessions                        │
│     Examples:                                                            │
│       - "payment-service timeout" → check Stripe status first            │
│       - "OOMKilled in namespace X" → usually caused by batch jobs        │
│       - "ArgoCD sync failed" → check Git credentials                     │
│     Storage: Rules table, updated after each session                     │
│                                                                          │
│  4. USER PREFERENCES (persistent, explicit)                              │
│     ─────────────────────────────────────                                │
│     What: Your stated preferences                                        │
│     Examples:                                                            │
│       - "Always check monitoring-prod namespace first"                   │
│       - "Never auto-apply changes to production"                         │
│       - "Preferred LLM: Claude"                                          │
│     Storage: Config + user commands                                      │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### How Memory Is Created

```
Session Start
     │
     ▼
┌─────────────┐
│ Load from   │
│ - Graph DB  │
│ - Memory DB │
│ - Config    │
└─────────────┘
     │
     ▼
[... conversation happens ...]
     │
     ▼
Session End (explicit or timeout)
     │
     ▼
┌─────────────────────────────┐
│ Session Summarizer          │
│ (LLM call with structured   │
│  output)                    │
│                             │
│ Input: Full conversation    │
│                             │
│ Output:                     │
│ {                           │
│   "issue": "...",           │
│   "root_cause": "...",      │
│   "resolution": "...",      │
│   "components": [...],      │
│   "tags": [...],            │
│   "pattern": "if X then Y"  │
│ }                           │
└─────────────┬───────────────┘
              │
              ▼
┌─────────────────────────────┐
│ Memory Store                │
│                             │
│ 1. Store session summary    │
│ 2. Generate embedding       │
│ 3. Update pattern rules     │
│    if confidence > threshold│
└─────────────────────────────┘
```

---

## UX Flow: Complete Session

```
# Terminal 1: joecored is running
$ joecored
Joe Core v0.1.0 starting...
API listening on :7777
Core Agent started (refresh interval: 5m)
[14:20:01] Graph refresh complete: 247 nodes, 892 edges
[14:23:15] API: GET /api/v1/graph/query?q=payment-service

# Terminal 2: User interacts with joe
$ joe

  ╭─────────────────────────────────────────────────────────────╮
  │  Joe v0.1.0 — Joe Operates Everything                       │
  │  Connected: joecored at localhost:7777                      │
  │  Sources: k8s/prod-cluster, argocd/prod, 3 git repos        │
  │  Model: claude-sonnet-4                                     │
  ╰─────────────────────────────────────────────────────────────╯

> I have this issue reported by a user:
  "Payment failed for order #12847, getting timeout error on checkout"
  
  I've done some troubleshooting and found this error in payment-service:
  2025-01-30T14:23:15Z ERROR payment-service context deadline exceeded...

⠋ Gathering context...
  ├─ Found payment-service in namespace payments
  ├─ Checking pod health: 3/3 running
  ├─ Fetching recent logs: 47 timeout errors in last hour
  ├─ Checking external dependencies...
  └─ Found similar incident from Jan 15

⠋ Analyzing...

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

Root cause: Stripe API degradation (external)

Evidence:
  • status.stripe.com shows "Investigating increased API latency" 
    started at 14:15 UTC
  • Your error occurred at 14:23 UTC (8 minutes after incident began)
  • 47 similar timeout errors in payment-service logs since 14:15
  • This matches a previous incident on Jan 15 (also Stripe)

Your payment-service is healthy—the issue is upstream.

Options:
  [1] Wait for Stripe to resolve (monitor status page)
  [2] Increase timeout from 30s → 60s (temporary mitigation)
  [3] Enable fallback payment processor (if configured)

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

> can you show me how to do option 2?

To increase the timeout, you'd update the ConfigMap:

  kubectl patch configmap payment-config -n payments \
    --type merge -p '{"data":{"STRIPE_TIMEOUT":"60s"}}'

Then restart the deployment to pick up the change:

  kubectl rollout restart deployment/payment-service -n payments

Want me to:
  [a] Run these commands (dry-run first)
  [b] Show the diff in values.yaml for a permanent GitOps change
  [c] Just watch the Stripe status page for you

> a

Executing dry-run...

  $ kubectl patch configmap payment-config -n payments \
      --type merge -p '{"data":{"STRIPE_TIMEOUT":"60s"}}' --dry-run=client

  configmap/payment-config patched (dry run)

This will:
  • Update payment-config ConfigMap
  • Change STRIPE_TIMEOUT from "30s" to "60s"
  • Requires pod restart to take effect

Proceed with actual change? [y/N]: y

  ✓ ConfigMap updated
  ✓ Deployment restarted
  ✓ Rollout status: 3/3 pods updated

Change complete. The new timeout is now active.

Note: This is a runtime change. For permanent fix, update 
~/code/infra/payments/values.yaml and push to Git.

> /exit

Session saved. Learned pattern:
  "payment-service timeout to stripe" → check status.stripe.com first

$
```

---

## Implementation: Two-Binary Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        Development Workflow                              │
├─────────────────────────────────────────────────────────────────────────┤
│                                                                          │
│  Terminal 1:                          Terminal 2:                       │
│  $ joecored                           $ joe                             │
│  API listening on :7777               Connecting to joecored...         │
│  Core Agent started                   Connected.                        │
│  Background refresh active                                              │
│                                       > why is payment failing?         │
│  [logs: graph refresh]                [queries joecored, responds]      │
│  [logs: API request]                                                    │
│                                       > look at my local changes        │
│                                       [reads local files directly]      │
│                                                                          │
└─────────────────────────────────────────────────────────────────────────┘
```

### Phase 1: Foundation (Two Binaries)
- `cmd/joe/` + `cmd/joecored/` structure
- HTTP API skeleton (joecored)
- HTTP client skeleton (joe)
- LLM Adapter interface + Claude implementation

### Phase 2: User Agent Loop
- Tool interface + executor + registry
- User Agent with agentic loop
- Basic local tools: `echo`, `ask_user`
- REPL

### Phase 3: Core Services + API
- SQL Store + Graph Store (joecored)
- API handlers for graph queries
- Core tools in joe calling API

### Phase 4: Infrastructure
- K8s, Git adapters + API endpoints + tools
- Local file tools

### Phase 5: Core Agent
- Background refresh
- .joe/ file discovery
- Clarification queue

### Phase 6+: Extensions
- ArgoCD, Prometheus, notifications
- Web UI, VS Code extension
