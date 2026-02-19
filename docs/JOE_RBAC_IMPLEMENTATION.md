JOE RBAC IMPLEMENTATION REFERENCE

This document provides implementation requirements for Joe's RBAC layer. Implement the described interfaces and behaviors without deviation from the architecture described here.

CRITICAL ARCHITECTURAL DECISION: RBAC AS MIDDLEWARE

RBAC is implemented as middleware, not as a standalone enforcer component. This means:

1. RBAC is a cross-cutting concern alongside audit, rate limiting, observability, etc.
2. RBAC middleware intercepts requests before they reach the LLM
3. RBAC middleware intercepts tool calls before they execute
4. Middleware is composable, configurable, and testable in isolation
5. Middleware can be enabled/disabled per environment
6. Middleware execution order is configurable

This architectural choice provides:
- Clean separation of concerns
- Easy testing and mocking
- Flexible deployment (dev vs prod pipelines)
- Extensibility for future cross-cutting concerns
- Performance optimization through middleware ordering

RBAC components (auth providers, policy engine) remain independent. Middleware is simply the integration layer that enforces RBAC decisions in the request pipeline.

WHERE MIDDLEWARE RUNS:

Middleware (including RBAC) runs ONLY in joecored, NOT in joe local.

joecored (daemon/server):
- HTTP API endpoints that joe local calls
- Full middleware stack on API handlers
- Middleware protects infrastructure tools (K8s, Prometheus, ArgoCD, etc.)
- Enforces authorization before executing infrastructure operations

joe local (CLI client):
- NO middleware stack
- NO RBAC enforcement
- Local tools execute directly (filesystem, local git, etc.)
- Calls joecored HTTP API with user's token when infrastructure tools needed
- joecored's middleware validates these API calls

This separation is critical:
- Local operations don't need RBAC (user already has OS permissions)
- Infrastructure operations require RBAC (shared resources, multi-user access)
- Security boundary is at the API, not in the local CLI

===============================================================================
ARCHITECTURE OVERVIEW
===============================================================================

Joe RBAC consists of three main components:

1. Authentication Layer - Validates user identity via pluggable providers
2. Authorization Layer - Evaluates policies to allow/deny requests  
3. Middleware Layer - Integrates RBAC into request and tool pipelines

All components live in internal/rbac/ package structure.
Middleware integration lives in internal/middleware/ package structure.

===============================================================================
PACKAGE STRUCTURE
===============================================================================

joecored packages (daemon/server with middleware):

internal/rbac/
  auth/           - Authentication interfaces and providers
    interface.go  - Core auth interfaces
    ldap/         - LDAP/AD provider
    entra/        - Azure Entra ID provider
    aws/          - AWS IAM provider
    gcp/          - GCP Identity provider
    oidc/         - Generic OIDC provider
    apikey/       - API key provider
    mtls/         - Certificate-based auth
    local/        - No-auth dev mode
  
  authz/          - Authorization engine
    interface.go  - Policy interfaces
    engine.go     - Policy evaluation engine
    policy.go     - Policy data structures
    loader.go     - Policy loading from config/git
  
  middleware/     - RBAC middleware implementations
    rbac.go       - Request-level RBAC middleware
    tool.go       - Tool-level RBAC middleware
    context.go    - Request context with identity
    
  cache/          - Token and policy caching
    cache.go      - TTL cache implementation
    
  audit/          - Audit logging
    logger.go     - Audit event logging

internal/middleware/
  interface.go    - Middleware interface definition
  chain.go        - Middleware chain builder
  audit.go        - Audit middleware
  ratelimit.go    - Rate limiting middleware
  sanitize.go     - Input sanitization middleware
  observability.go - Tracing and metrics middleware
  timeout.go      - Request timeout middleware

cmd/joecored/
  api/            - HTTP API handlers
    handlers.go   - API endpoint handlers with middleware
    middleware.go - HTTP middleware wrapper
  main.go         - Daemon entry point

joe local packages (CLI client without middleware):

cmd/joe/
  main.go         - CLI entry point
  repl.go         - Interactive prompt

internal/client/
  client.go       - HTTP client for joecored API
  auth.go         - Token management for CLI

internal/tools/
  local/          - Local tools (no API calls)
    git.go        - Local git operations
    file.go       - Local filesystem operations
  
  core/           - Infrastructure tools (API wrappers)
    k8s.go        - Kubernetes tools (call joecored)
    prom.go       - Prometheus tools (call joecored)
    graph.go      - Graph query tools (call joecored)

Note: joe local has NO middleware packages, NO RBAC packages. All authorization happens in joecored.

===============================================================================
DEPLOYMENT BOUNDARIES
===============================================================================

CRITICAL: Understand where each component runs

joecored (daemon/server):
- Runs as background service on infrastructure
- Has access to Kubernetes clusters, Prometheus, ArgoCD, etc.
- Exposes HTTP API on localhost or internal network
- Contains ALL middleware components
- Contains ALL RBAC components (auth providers, policy engine)
- Enforces authorization before executing infrastructure tools
- Multi-user service with shared resources

joe local (CLI client):
- Runs on user's workstation
- Has access to user's local filesystem, local git repos
- Makes HTTP calls to joecored for infrastructure operations
- Contains NO middleware components
- Contains NO RBAC components
- Sends user's token with API requests to joecored
- Single-user client with local resources

Security boundary:
- Local operations: User's OS permissions (filesystem ACLs, etc.)
- Infrastructure operations: Joe's RBAC policies (evaluated in joecored)

Example flow showing boundary:

User: "compare my local changes to prod deployment"

joe local executes:
  1. local_git_diff tool → reads user's filesystem directly
     Security: OS file permissions
     RBAC: None
  
  2. Call joecored API: GET /api/tool/execute
     Headers: Authorization: Bearer <token>
     Body: {tool: "k8s_get", params: {env: "prod", resource: "deployment"}}

joecored receives API call:
  3. Middleware stack executes:
     - TokenExtractor: parse token from header
     - RBAC: check if user can read prod deployments
     - If denied: return 403
     - If allowed: continue
  
  4. Tool execution:
     - RBACTool: verify params match authorization
     - Execute: kubectl get deployment in prod
     - Return result

joe local receives result:
  5. LLM compares local diff to prod deployment
  6. Display comparison to user

The RBAC check happens at step 3 in joecored, NOT in joe local.

Browser and IDE clients:
- Same model: clients make API calls to joecored
- Middleware runs in joecored on API endpoints
- All clients protected by same RBAC policies
- No middleware in browser JavaScript or VS Code extension

===============================================================================
AUTHENTICATION LAYER
===============================================================================

IDENTITY PROVIDER INTERFACE

The core authentication interface that all providers must implement.

Type: IdentityProvider (interface)

Methods required:

Authenticate - Validates credentials and returns identity
  Input: Credentials struct containing provider-specific auth data
  Returns: Identity struct, error
  Behavior: Contact IdP, validate credentials, extract user info and groups
  Error cases: Invalid credentials, IdP unavailable, network timeout
  
ValidateToken - Validates existing token and returns identity
  Input: Token string (JWT, API key, or provider-specific)
  Returns: Identity struct, error  
  Behavior: Parse token, verify signature/expiry, extract claims
  Caching: Results should be cached by caller using token as key
  Error cases: Invalid token, expired token, signature mismatch
  
GetGroups - Retrieves group memberships for identity
  Input: Identity struct
  Returns: Slice of Group structs, error
  Behavior: Query IdP for group memberships, handle nested groups
  Note: May be called separately or during Authenticate/ValidateToken
  Error cases: User not found, IdP unavailable

RefreshToken - Refreshes an expiring token (optional, not all providers)
  Input: Existing token string
  Returns: New token string, error
  Behavior: Use refresh token to get new access token
  Error cases: Refresh token invalid/expired

IDENTITY DATA STRUCTURE

Type: Identity (struct)

Fields required:
- UserID: Unique identifier from IdP (string)
- Email: User email address (string)
- DisplayName: Human-readable name (string)  
- Groups: List of group names/IDs user belongs to (slice of strings)
- Metadata: Provider-specific additional data (map of string to string)
- TokenExpiry: When the current token expires (time.Time)

Usage: Passed through entire request chain for authorization decisions

CREDENTIALS DATA STRUCTURE

Type: Credentials (struct)

Fields:
- Type: Credential type constant (string: "password", "apikey", "token", "cert")
- Username: For password auth (string, optional)
- Password: For password auth (string, optional)
- Token: For token-based auth (string, optional)
- Certificate: For mTLS (x509 cert, optional)
- Metadata: Provider-specific data (map of string to string)

Usage: Input to Authenticate method, contains auth material

PROVIDER IMPLEMENTATIONS

Each identity provider adapter must implement IdentityProvider interface.

LDAP Provider Requirements:
- Connect via LDAPS (port 636)
- Support bind DN for service account
- Search for users by uid or email
- Recursively resolve group memberships
- Map LDAP groups to Joe group names
- Connection pooling for performance
- Retry logic for transient failures
- Configuration: URL, bind DN, base DNs for users and groups

Entra ID Provider Requirements:
- OAuth 2.0 authorization code flow
- Token validation using Microsoft public keys
- Group claims from JWT or Graph API
- Handle nested groups in Azure AD
- Refresh token support
- Configuration: tenant ID, client ID, client secret

AWS IAM Provider Requirements:
- Assume role workflow
- Validate caller identity via STS GetCallerIdentity
- Map IAM roles to Joe groups
- Support both IAM users and roles
- Session token handling
- Configuration: Region, role ARN

GCP Identity Provider Requirements:
- Google OAuth 2.0
- Service account impersonation
- Workspace group membership
- Token validation with Google certs
- Configuration: Project ID, client credentials

OIDC Provider Requirements:
- Generic OpenID Connect flow
- Discovery endpoint support
- JWT validation with provider keys
- Extract groups from claims or userinfo endpoint
- Configurable claim mappings
- Configuration: Issuer URL, client ID, client secret, scopes

API Key Provider Requirements:
- Hash-based key storage (bcrypt or similar)
- Key-to-identity mapping in config or database
- Rotation tracking
- Rate limiting per key
- Configuration: Key store location, hash algorithm

mTLS Provider Requirements:
- Certificate validation via CA bundle
- Extract identity from cert subject/SAN
- Map cert attributes to Joe identity
- Revocation checking (CRL or OCSP)
- Configuration: Trusted CA bundle, field mappings

Local Provider Requirements:
- No actual authentication
- Returns fixed identity for development
- Environment variable to enable/disable
- Warning logs when active
- Configuration: Default user/groups for dev mode

TOKEN CACHING

Type: IdentityCache (component)

Purpose: Reduce IdP load by caching validated identities

Cache Key: Token string (hash if sensitive)
Cache Value: Identity struct
TTL: Token expiry time or configurable max
Eviction: Automatic on TTL expiry

Methods required:

Get - Retrieve cached identity
  Input: Token string
  Returns: Identity struct (or nil if not cached), bool (found)
  
Set - Cache identity
  Input: Token string, Identity struct, TTL duration
  Returns: error
  Behavior: Store with automatic expiry
  
Invalidate - Remove cached identity
  Input: Token string
  Returns: error
  Use case: Explicit logout, token revocation

Implementation notes:
- Thread-safe concurrent access
- Memory-efficient (limit max entries)
- Metrics: cache hit rate, evictions

===============================================================================
AUTHORIZATION LAYER  
===============================================================================

POLICY DATA STRUCTURES

Type: Policy (struct)

Fields:
- Name: Human-readable policy identifier (string)
- Subjects: Who this policy applies to (slice of Subject)
- Permissions: What is allowed (slice of Permission)
- Constraints: Additional requirements (slice of Constraint)

Type: Subject (struct)

Fields:
- Type: "user" or "group" (string constant)
- Identifiers: User IDs or group names (slice of strings)

Example: Subject for group "sre" or user "jaime@company.com"

Type: Permission (struct)

Fields:
- Environments: Allowed environments (slice of strings)
- Namespaces: Allowed namespaces (slice of strings)  
- Resources: Allowed resource types (slice of strings)
- Actions: Allowed action types (slice of Action constants)

Wildcards: Use "*" for all in a category

Example: Permission allowing Query and Read on all resources in dev environment

Type: Action (constant)

Values:
- ActionRead: View current state
- ActionQuery: Execute read-only queries
- ActionMutate: Modify configuration  
- ActionDelete: Remove resources

Type: Constraint (struct)

Fields:
- Type: Constraint type (string constant)
- Config: Type-specific configuration (map of string to interface)

Constraint types:
- RequireApproval: Requires additional confirmation for specified actions
- TimeWindow: Restrict to certain hours/days
- IPWhitelist: Restrict to certain source IPs
- MFARequired: Require multi-factor authentication

Type: Request (struct)

Fields:
- Environment: Target environment (string)
- Namespace: Target namespace (string)  
- Resource: Resource type being accessed (string)
- Action: Action being performed (Action constant)
- Metadata: Additional context (map of string to string)

Usage: Represents a user's intended operation for policy evaluation

POLICY ENGINE

Type: PolicyEngine (component)

Purpose: Evaluate whether a request is allowed based on loaded policies

Fields:
- policies: Map of group name to policies (map of string to slice of Policy)
- policyLoader: Component to load policies from storage
- identityCache: Reference to identity cache

Methods required:

Authorize - Main authorization decision
  Input: Identity struct, Request struct
  Returns: Decision (allow/deny), matched Policy, error
  Behavior:
    1. Load all policies for user's groups
    2. Iterate policies in priority order
    3. Check if policy subject matches identity
    4. Check if request matches permission
    5. Evaluate constraints if permission matches
    6. Return first allow or final deny
  Performance: In-memory evaluation, no external calls
  Error cases: Policy load failure, constraint evaluation error

LoadPolicies - Load/reload policies
  Input: Source location (file path, git repo, etc)
  Returns: error
  Behavior: Parse policies, validate, store in memory
  Thread safety: Safe to call during operation
  
EvaluateConstraints - Check if constraints are satisfied
  Input: Slice of Constraint, Request context
  Returns: bool (satisfied), error
  Behavior: Check time windows, IP restrictions, MFA status, etc
  
PolicyEngine instantiation requires:
- PolicyLoader implementation
- Identity cache reference
- Configuration for policy refresh interval

POLICY EVALUATION ALGORITHM

For each policy applicable to user's groups:
  1. Match subject: Does policy apply to this user/group?
  2. Match environment: Is request environment in policy's environments?
  3. Match namespace: Is request namespace in policy's namespaces?
  4. Match resource: Is request resource in policy's resources?
  5. Match action: Is request action in policy's actions?
  6. Evaluate constraints: Are all constraints satisfied?
  
If all match: ALLOW
If none match after all policies: DENY

Wildcard handling:
- "*" in environments matches any environment
- "*" in namespaces matches any namespace
- "*" in resources matches any resource
- Action wildcards not supported (explicit action required)

POLICY LOADER

Type: PolicyLoader (interface)

Methods required:

Load - Load policies from source
  Input: Source configuration
  Returns: Slice of Policy, error
  Behavior: Parse policy definitions, validate structure
  
Watch - Monitor source for changes (optional)
  Input: Callback function for changes
  Returns: error
  Behavior: Poll or subscribe to changes, call callback on update

Implementations needed:

FileLoader:
- Read YAML/JSON from local file
- File path from configuration
- Optional file watching with fsnotify

GitLoader:
- Clone/pull git repository
- Read policy files from repo
- Periodic refresh on timer
- Configuration: Repo URL, branch, path to policies

ConfigMapLoader (future):
- Read from Kubernetes ConfigMap
- Watch for ConfigMap updates
- Configuration: Namespace, ConfigMap name

POLICY STORAGE FORMAT

Policies stored as YAML files with this structure:

Top level is list of policy objects

Each policy has:
- name: string identifying the policy
- subjects: list of subject objects
  - type: "user" or "group"  
  - identifiers: list of user IDs or group names
- permissions: list of permission objects
  - environments: list of environment names or ["*"]
  - namespaces: list of namespace names or ["*"]
  - resources: list of resource types or ["*"]
  - actions: list of action names (Read, Query, Mutate, Delete)
- constraints: list of constraint objects (optional)
  - type: constraint type name
  - config: map of constraint-specific settings

Example structure (describe in implementation without code):
Policy named junior-engineers applies to group engineering-junior, allows Read and Query actions on all resources in dev and staging environments, no constraints.

===============================================================================
MIDDLEWARE ARCHITECTURE
===============================================================================

MIDDLEWARE INTERFACE

Type: Middleware (interface)

Purpose: Provide composable request and tool processing pipeline

Methods required:

ProcessRequest - Handle request-level concerns
  Input: context.Context, Request, next Handler function
  Returns: Response, error
  Behavior:
    - Perform middleware logic (auth check, rate limit, etc)
    - If logic succeeds: call next(ctx, req) to continue chain
    - If logic fails: return error, short-circuit chain
    - Can modify context before calling next
    - Can modify response after next returns
  Thread safety: Must be safe for concurrent calls

ProcessTool - Handle tool-level concerns  
  Input: context.Context, ToolCall, next ToolHandler function
  Returns: ToolResult, error
  Behavior:
    - Validate tool execution against policies
    - Add observability (traces, metrics)
    - Apply safety controls
    - Call next(ctx, toolCall) if allowed
  Thread safety: Must be safe for concurrent calls

Name - Identifier for logging/debugging
  Returns: string (middleware name)

Type: Handler (function)

Signature: func(context.Context, Request) (Response, error)
Purpose: Core request handler (agent processing)

Type: ToolHandler (function)

Signature: func(context.Context, ToolCall) (ToolResult, error)
Purpose: Core tool executor

MIDDLEWARE CHAIN

Type: Chain (component)

Purpose: Build and execute middleware pipeline

Methods required:

Use - Add middleware to chain
  Input: Middleware instance
  Returns: Chain (for method chaining)
  Behavior: Append middleware to ordered list
  
Execute - Run request through middleware chain
  Input: context.Context, Request, final Handler
  Returns: Response, error
  Behavior:
    - Execute middlewares in order added
    - Each middleware calls next in chain
    - Final handler called after all middlewares
    - Returns flow back through middleware stack
  
ExecuteTool - Run tool call through middleware chain
  Input: context.Context, ToolCall, final ToolHandler
  Returns: ToolResult, error
  Behavior: Same as Execute but for tool pipeline

Example chain construction:

Request chain for production:
  Chain.Use(RBACMiddleware)
      .Use(RateLimitMiddleware)
      .Use(InputSanitizationMiddleware)
      .Use(AuditMiddleware)
      .Use(ObservabilityMiddleware)
      .Execute(ctx, req, agentHandler)

Tool chain for production:
  Chain.Use(RBACToolMiddleware)
      .Use(DryRunSafetyMiddleware)
      .Use(ObservabilityMiddleware)
      .ExecuteTool(ctx, toolCall, toolExecutor)

Request chain for development:
  Chain.Use(InputSanitizationMiddleware)
      .Use(ObservabilityMiddleware)
      .Execute(ctx, req, agentHandler)

RBAC MIDDLEWARE IMPLEMENTATION

Type: RBACMiddleware (component)

Purpose: Request-level authorization middleware

Fields:
- authProvider: IdentityProvider implementation
- policyEngine: PolicyEngine instance
- auditLogger: AuditLogger instance
- identityCache: Identity cache

ProcessRequest implementation:
  Input: context, Request, next handler
  Behavior:
    1. Extract token from context (added by earlier auth middleware or from request metadata)
    2. Validate token and get identity (use cache)
    3. Extract authorization request from user message
    4. Call policyEngine.Authorize(identity, authzRequest)
    5. If denied:
       - Log audit event (denied)
       - Return authorization error
       - Do NOT call next
    6. If allowed:
       - Attach RequestContext to context with identity and decision
       - Log audit event (allowed)
       - Call next(ctx, request)
       - Return result from next
  Performance: <1ms with cached identity
  Error handling: All errors logged and returned to user

Name implementation:
  Returns: "RBAC"

Type: RBACToolMiddleware (component)

Purpose: Tool-level authorization middleware

Fields:
- policyEngine: PolicyEngine instance
- auditLogger: AuditLogger instance

ProcessTool implementation:
  Input: context, ToolCall, next tool handler
  Behavior:
    1. Get RequestContext from context (set by RBACMiddleware)
    2. Verify tool parameters match authorized request:
       - Check environment matches
       - Check namespace matches
       - Check resource type matches
       - Check action type matches
    3. If mismatch:
       - Log security violation
       - Return error "Tool parameters exceed authorized scope"
       - Do NOT call next
    4. If match:
       - Call next(ctx, toolCall)
       - Return result from next
  Purpose: Prevent LLM from escalating beyond user's authorization
  Example: User authorized for dev, LLM tries to query prod → blocked

Name implementation:
  Returns: "RBACTool"

TOKEN EXTRACTION MIDDLEWARE

Type: TokenExtractorMiddleware (component)

Purpose: Extract authentication token from request and attach to context

Runs BEFORE RBACMiddleware in chain

ProcessRequest implementation:
  Input: context, Request, next
  Behavior:
    1. Check request metadata for token:
       - CLI: Read from ~/.joe/token file
       - Web: Extract from Authorization header or cookie
       - API: Extract from Authorization header
    2. If no token found:
       - Trigger authentication flow based on configured provider
       - OIDC: Return redirect URL
       - API key: Return prompt for key
       - Local dev: Use default identity
    3. Attach token string to context
    4. Call next(ctx, request)
    5. Return result

Name implementation:
  Returns: "TokenExtractor"

REQUEST CONTEXT

Type: RequestContext (struct)

Purpose: Carry identity and authorization info through request chain

Fields:
- Identity: Validated user identity
- Token: Original auth token
- Decision: Authorization decision
- MatchedPolicy: Policy that allowed request (if any)
- RequestID: Unique request identifier for audit
- Timestamp: Request timestamp
- SourceIP: Client IP address
- UserAgent: Client identifier

Usage: Attached to context.Context by RBACMiddleware, read by downstream components

Methods:

FromContext - Extract RequestContext from context.Context
  Input: context.Context
  Returns: RequestContext pointer, error
  Error: If RequestContext not found in context
  
WithRequestContext - Add RequestContext to context.Context
  Input: context.Context, RequestContext
  Returns: context.Context with RequestContext attached
  
NewRequestContext - Create new RequestContext
  Input: Identity, token string
  Returns: RequestContext with generated request ID and timestamp

INTEGRATION WITH JOECORED API

joecored exposes an HTTP API that joe local calls for infrastructure operations.

API endpoint structure:
- POST /api/tool/execute - Execute infrastructure tool
- POST /api/query - Query infrastructure graph
- GET /api/status - Health check (no auth required)

Middleware integration in joecored:

Add HTTP middleware wrapper in cmd/joecored/api/handlers.go:

Build middleware chains at joecored startup:
  Request chain (for API endpoints):
    If RBAC enabled:
      chain.Use(TokenExtractorMiddleware)
          .Use(RBACMiddleware)
          .Use(RateLimitMiddleware)
          .Use(InputSanitizationMiddleware)
          .Use(AuditMiddleware)
          .Use(ObservabilityMiddleware)
    Else (dev mode):
      chain.Use(InputSanitizationMiddleware)
          .Use(ObservabilityMiddleware)
  
  Tool chain (for tool execution):
    If RBAC enabled:
      chain.Use(RBACToolMiddleware)
          .Use(DryRunSafetyMiddleware)
          .Use(ObservabilityMiddleware)
    Else:
      chain.Use(DryRunSafetyMiddleware)
          .Use(ObservabilityMiddleware)

API handler for tool execution:
  func HandleToolExecution(w http.ResponseWriter, r *http.Request):
    Extract request body (tool name, parameters, user message context)
    
    Execute through request middleware chain:
      result = requestChain.Execute(ctx, request, handleToolRequest)
    
    If middleware denies (RBAC, rate limit, etc):
      Return 403 Forbidden or 429 Too Many Requests
      Include error message for user
    
    If middleware allows:
      handleToolRequest executes, which internally:
        Calls toolChain.ExecuteTool(ctx, toolCall, tool.Execute)
        Returns tool result
    
    Return result to joe local

joe local integration:

joe local does NOT have middleware. It simply calls joecored's API:

In joe local's tool registry:
  Infrastructure tools are API wrappers:
    func (t *K8sGetTool) Execute(ctx, params):
      Make HTTP POST to joecored: /api/tool/execute
      Include user's token in Authorization header
      joecored middleware validates request
      Return result or error from API
  
  Local tools execute directly:
    func (t *LocalGitDiffTool) Execute(ctx, params):
      Run git diff on local filesystem
      No API call, no middleware
      Return diff output

User workflow:

User types: "show prod payment logs"
  │
  ▼
joe local User Agent:
  - Processes message locally
  - LLM picks tool: k8s_logs
  - k8s_logs is infrastructure tool
  │
  ▼
joe local calls joecored API:
  POST /api/tool/execute
  Headers:
    Authorization: Bearer <user-token>
  Body:
    tool: "k8s_logs"
    params: {namespace: "prod", pod: "payment-*"}
  │
  ▼
joecored API handler:
  Request middleware chain executes:
    1. TokenExtractor: Extract token from header
    2. RBAC: Validate user has prod read access
    3. RateLimit: Check user within limits
    4. Audit: Log request
    5. Observability: Create trace span
  │
  If RBAC denies:
    Return 403: "Access denied: You don't have permissions 
                 to query logs in prod environment"
  │
  If RBAC allows:
    ▼
  Tool middleware chain executes:
    1. RBACTool: Verify params match authorized scope
    2. DryRun: Check if approval needed (not for read-only)
    3. Observability: Create tool span
    ▼
  Execute tool: kubectl logs in prod
    ▼
  Return logs to joe local
    ▼
joe local displays logs to user

OTHER MIDDLEWARE IMPLEMENTATIONS

Type: AuditMiddleware

ProcessRequest:
  - Log request start with request ID
  - Call next
  - Log request completion with duration and result
  - Log errors if any
  - Always succeeds (logging failures don't block requests)

Type: RateLimitMiddleware

ProcessRequest:
  - Check rate limit for user identity (from context)
  - If limit exceeded: return rate limit error
  - If within limit: increment counter, call next
  - Use sliding window or token bucket algorithm

Type: InputSanitizationMiddleware

ProcessRequest:
  - Scan request for injection patterns
  - Remove or escape dangerous characters
  - Validate message length limits
  - Check for prompt injection attempts
  - Call next with sanitized request

Type: ObservabilityMiddleware

ProcessRequest:
  - Start distributed trace span
  - Record request metrics (counter, duration histogram)
  - Attach trace context to outgoing context
  - Call next
  - End span with result status
  - Record latency metrics

ProcessTool:
  - Create child span for tool execution
  - Record tool-specific metrics
  - Call next
  - Record tool latency and result

Type: TimeoutMiddleware

ProcessRequest:
  - Create context with timeout from config
  - Call next with timeout context
  - If timeout exceeded: cancel context, return timeout error
  - Otherwise return result

Type: CircuitBreakerMiddleware (future)

ProcessRequest:
  - Check circuit breaker state for backend
  - If open: return fast failure
  - If closed: call next
  - Track failures, open circuit after threshold

MIDDLEWARE CONFIGURATION

Configuration in Joe config file:

middleware section with ordered list of middlewares to enable:

Fields:
- request_pipeline: List of middleware names for requests
- tool_pipeline: List of middleware names for tools
- middleware_config: Map of middleware name to configuration

Example structure:
Request pipeline with rbac, ratelimit, sanitize, audit, observability in order
Tool pipeline with rbac-tool, dryrun, observability in order
Each middleware has its own config section

Middleware can be enabled/disabled per environment:
- Development: Minimal middleware (sanitize, observability)
- Staging: Most middleware except rate limiting
- Production: Full middleware stack

MIDDLEWARE EXECUTION FLOW

Request flow through middleware chain:

User sends message
  │
  ▼
TokenExtractor middleware:
  - Extract token from request
  - Attach to context
  - Call next
  │
  ▼
RBAC middleware:
  - Validate token (cached)
  - Check authorization
  - If denied → return error (chain stops)
  - If allowed → attach RequestContext, call next
  │
  ▼
RateLimit middleware:
  - Check user's rate limit
  - If exceeded → return error
  - Otherwise call next
  │
  ▼
InputSanitization middleware:
  - Clean request message
  - Call next with sanitized input
  │
  ▼
Audit middleware:
  - Log request start
  - Call next
  - Log request completion
  │
  ▼
Observability middleware:
  - Start trace span
  - Call next
  - End span, record metrics
  │
  ▼
Agent handler (core processing):
  - Process user message
  - LLM reasoning
  - Select tools
  - Return response
  │
  ▼
Response flows back through middleware (reverse order):
Observability → Audit → InputSanitization → RateLimit → RBAC → TokenExtractor
  │
  ▼
Response returned to user

Tool execution flow through middleware:

LLM selects tool
  │
  ▼
RBACTool middleware:
  - Verify tool params match authorized request
  - If mismatch → deny
  - Otherwise call next
  │
  ▼
DryRunSafety middleware:
  - Assess risk level
  - If destructive → show dry-run, require approval
  - Call next after approval
  │
  ▼
Observability middleware:
  - Create tool span
  - Call next
  - Record tool metrics
  │
  ▼
Tool executor (actual execution):
  - Execute kubectl, API call, etc
  - Return result
  │
  ▼
Result flows back through middleware
  │
  ▼
Result returned to agent

===============================================================================
AUDIT LOGGING
===============================================================================

Type: AuditLogger (component)

Purpose: Record all authorization decisions and actions

Fields:
- writer: Audit log writer (file, database, syslog, etc)
- buffer: Optional buffering for performance
- config: Audit configuration

Methods required:

LogAuthz - Log authorization decision
  Input: AuditEvent struct
  Returns: error
  Behavior: Write event to audit log asynchronously
  Format: Structured JSON with timestamp, user, request, decision
  
Flush - Ensure buffered events are written
  Returns: error
  Behavior: Block until all buffered events persisted
  
Close - Clean shutdown
  Returns: error
  Behavior: Flush and close writer

Type: AuditEvent (struct)

Fields:
- Timestamp: Event time (time.Time)
- RequestID: Unique request ID (string)
- UserID: User identifier (string)
- Groups: User groups at time of request (slice of strings)
- Token: Token used (hashed, not plaintext)
- Request: Full request details (Request struct)
- Decision: Allow or Deny (bool)
- MatchedPolicy: Policy name if allowed (string)
- DenyReason: Reason if denied (string)
- Duration: Time to make decision (time.Duration)
- SourceIP: Client IP address (string, optional)
- UserAgent: Client user agent (string, optional)

Audit log format (JSON lines):
Each event is single JSON object per line
Timestamp in ISO8601 format
All fields present even if empty
No nested objects for easy parsing

Audit log rotation:
Daily rotation by default
Compress old logs
Configurable retention period
Separate audit logs from application logs

Audit log destinations:
- Local file: ~/.joe/audit.log or configured path
- Syslog: For centralized logging
- Database: For queryable audit trail
- Cloud logging: AWS CloudWatch, Azure Monitor, GCP Logging

===============================================================================
CONFIGURATION
===============================================================================

RBAC configuration in Joe config file (~/.joe/config.yaml or /etc/joe/config.yaml)

Top level rbac section:

Fields:
- enabled: Boolean to enable/disable RBAC (default false for MVP)
- identity_provider: Provider type (ldap, entra, aws, gcp, oidc, apikey, mtls, local)
- provider_config: Provider-specific configuration (map)
- policy_source: Where policies are stored (file, git, configmap)
- policy_config: Policy source configuration (map)
- token_cache_ttl: How long to cache validated tokens (duration)
- policy_refresh_interval: How often to reload policies (duration)
- audit_enabled: Enable audit logging (boolean)
- audit_config: Audit configuration (map)

Top level middleware section:

Fields:
- request_pipeline: Ordered list of middleware names for requests
- tool_pipeline: Ordered list of middleware names for tools
- environments: Map of environment name to middleware overrides

Request pipeline options:
- token-extractor: Extract authentication token
- rbac: RBAC authorization checks
- ratelimit: Rate limiting per user
- sanitize: Input sanitization
- audit: Audit logging
- observability: Tracing and metrics
- timeout: Request timeouts
- circuit-breaker: Circuit breaking for backends (future)

Tool pipeline options:
- rbac-tool: RBAC tool parameter verification
- dryrun: Dry-run safety controls
- observability: Tool execution tracing

Example middleware configuration:

Production environment:
  request_pipeline: [token-extractor, rbac, ratelimit, sanitize, audit, observability, timeout]
  tool_pipeline: [rbac-tool, dryrun, observability]

Development environment:
  request_pipeline: [token-extractor, sanitize, observability]
  tool_pipeline: [dryrun, observability]

Testing environment:
  request_pipeline: [observability]
  tool_pipeline: [observability]

Per-middleware configuration:

ratelimit config:
- requests_per_minute: Number of requests allowed per minute per user
- burst: Burst size for token bucket
- strategy: "sliding-window" or "token-bucket"

timeout config:
- request_timeout: Maximum duration for request processing
- tool_timeout: Maximum duration for tool execution

observability config:
- trace_enabled: Enable distributed tracing
- metrics_enabled: Enable metrics collection
- trace_endpoint: OTLP endpoint for traces
- metrics_endpoint: Prometheus or OTLP endpoint

sanitize config:
- max_message_length: Maximum user message length
- blocked_patterns: Regex patterns to reject
- escape_html: Whether to escape HTML characters

Example structure (describe without code):
RBAC enabled, using Entra ID provider with tenant and client IDs, policies loaded from git repository, token cache TTL of 1 hour, policy refresh every 5 minutes, audit enabled to local file.

Middleware configuration with full production pipeline for requests including token extraction, RBAC, rate limiting at 60 requests per minute, input sanitization with 10KB message limit, audit logging, observability with traces enabled, and 30 second timeout.

Tool pipeline with RBAC tool checks, dry-run safety requiring approval for destructive operations, and observability.

Development mode overrides with minimal pipeline: just sanitization and observability for requests, dry-run and observability for tools.

Provider-specific configs:

LDAP config needs:
- url: LDAP server URL
- bind_dn: Service account DN
- bind_password_env: Environment variable with password
- user_base_dn: Base DN for user search
- group_base_dn: Base DN for group search
- user_filter: LDAP filter for user lookup
- group_filter: LDAP filter for group membership

Entra config needs:
- tenant_id: Azure AD tenant ID
- client_id: Application client ID  
- client_secret_env: Environment variable with secret
- redirect_uri: OAuth redirect URI

Policy config for git source:
- repo_url: Git repository URL
- branch: Branch name
- path: Path to policy files within repo
- auth_token_env: Environment variable with git token
- refresh_interval: How often to pull

Audit config:
- destination: file, syslog, database, cloudwatch
- file_path: If destination is file
- rotation: daily, size-based
- retention_days: How long to keep
- buffer_size: Events to buffer before flush

===============================================================================
ERROR HANDLING
===============================================================================

All errors must include context for debugging:

Authentication errors:
- InvalidCredentials: Wrong username/password
- TokenExpired: Token no longer valid
- ProviderUnavailable: Cannot reach IdP
- ConfigurationError: Provider misconfigured

Authorization errors:
- AccessDenied: Policy evaluation denied request
- PolicyLoadError: Cannot load policies
- ConstraintViolation: Constraint not satisfied

Audit errors:
- AuditWriteFailed: Cannot write to audit log
- BufferFull: Audit buffer overflow

Error messages to users:
- Never leak internal details (no stack traces)
- Specific enough to be actionable
- Include request ID for correlation
- Suggest remediation if possible

Example user-facing error:
"Access denied: You don't have permissions to perform Delete actions in prod environment. Contact platform-team@company.com to request access."

===============================================================================
TESTING REQUIREMENTS
===============================================================================

Unit tests required for:
- Each identity provider implementation
- Policy evaluation logic with various scenarios
- Request extraction from user messages
- Token caching behavior
- Constraint evaluation
- Each middleware in isolation
- Middleware chain construction and execution
- Context propagation through middleware

Integration tests required:
- Full auth flow with test IdP
- End-to-end authorization with real policies
- Audit log writing and reading
- Token refresh workflows
- Policy reload on changes
- Complete middleware chain execution
- Middleware ordering and composition
- Error propagation through middleware stack

Middleware-specific tests:

RBAC Middleware tests:
- Allowed request proceeds to next middleware
- Denied request short-circuits chain
- RequestContext attached to context correctly
- Token validation caching works
- Audit events logged for both allow and deny
- Concurrent requests handled safely

RBAC Tool Middleware tests:
- Tool params matching authorized scope allowed
- Tool params exceeding scope denied
- Missing RequestContext in context handled
- Security violation logged

Token Extractor Middleware tests:
- Token extracted from CLI token file
- Token extracted from Authorization header
- Token extracted from cookie
- Missing token triggers auth flow
- Invalid token triggers re-auth

Rate Limit Middleware tests:
- Requests within limit proceed
- Requests exceeding limit denied
- Burst handling works correctly
- Per-user limits enforced
- Counter reset after window

Input Sanitization Middleware tests:
- Dangerous patterns removed
- Length limits enforced
- Prompt injection attempts blocked
- Valid input passes unchanged

Observability Middleware tests:
- Trace spans created and closed
- Metrics recorded correctly
- Context propagated to next middleware
- Errors recorded in spans

Audit Middleware tests:
- Request start logged
- Request completion logged with duration
- Errors logged
- Logging failures don't block requests

Middleware Chain tests:
- Middlewares execute in order
- Early middleware can short-circuit
- Context flows through all middlewares
- Response flows back through stack
- Error in middleware stops chain
- Empty chain executes handler directly

Test scenarios to cover:

Authentication:
- Valid credentials accepted
- Invalid credentials rejected
- Expired token triggers re-auth
- Token refresh works
- Provider unavailable handled gracefully
- Cached token reduces latency

Authorization:
- Policy match allows request
- No policy match denies request  
- Wildcard matching works correctly
- Constraints properly evaluated
- Group membership changes reflected

Middleware:
- RBAC denial prevents LLM processing
- Rate limit prevents abuse
- Input sanitization blocks malicious input
- Observability captures all operations
- Middleware ordering matters
- Middleware can modify context
- Middleware can modify response

Performance:
- Benchmark policy evaluation <1ms
- Cache hit rate >90% in normal usage
- Policy reload doesn't block requests
- Audit buffering prevents I/O blocking
- Middleware overhead <5ms total
- Concurrent request handling scalable

Error handling:
- Middleware errors propagate correctly
- Partial chain execution on early error
- Audit logs record all errors
- User receives appropriate error messages
- No sensitive data in error messages

===============================================================================
ROLLOUT PLAN
===============================================================================

Phase 1: Middleware Framework
- Define middleware interface
- Implement middleware chain builder
- Add basic observability middleware (traces, metrics)
- Add input sanitization middleware
- Wire middleware chain into user agent
- Test middleware execution order and short-circuiting
- Validate with development workload

Phase 2: RBAC Foundation
- Implement authentication interfaces
- Add local provider for development
- Add API key provider
- Implement policy engine with file-based policies
- Implement token caching
- Create RBAC middleware (request and tool)
- Add token extractor middleware
- Wire RBAC middleware into chain (disabled by default)

Phase 3: Identity Providers
- Implement OIDC provider (generic SSO)
- Implement Entra ID provider (Azure)
- Implement LDAP/AD provider
- Test each provider independently
- Test provider switching

Phase 4: Production Readiness
- Implement audit middleware
- Implement rate limit middleware
- Add git-based policy storage
- Add timeout middleware
- Comprehensive audit logging
- Enable RBAC by default with feature flag
- Performance testing and optimization
- Security audit

Phase 5: Enterprise Features
- Implement AWS IAM provider
- Implement GCP Identity provider
- Add mTLS provider
- Advanced constraints (MFA, time windows, IP whitelisting)
- Circuit breaker middleware
- External policy engine integration (OPA)
- Compliance reporting
- SIEM integration

Phase 6: Operational Tools
- Policy management CLI
- Policy validation tool
- Audit log query tool
- Real-time authorization monitoring
- Policy testing framework
- Web UI for policy management

===============================================================================
ACCEPTANCE CRITERIA
===============================================================================

For RBAC implementation to be complete:

1. All identity providers implement IdentityProvider interface
2. Policy engine evaluates policies in <1ms with caching
3. Token cache reduces IdP calls by >90%
4. All authorization decisions logged to audit trail
5. Denied requests never reach LLM
6. Allowed requests include identity context
7. Middleware interface defined and documented
8. Middleware chain builder implemented
9. RBAC middleware enforces authorization pre-LLM
10. RBAC tool middleware enforces tool parameter scope
11. Middleware can be enabled/disabled per environment
12. Middleware execution order configurable
13. Request context propagates through middleware chain
14. Middleware short-circuit works correctly
15. Middleware overhead <5ms total for full stack
16. Configuration validated at startup
17. Errors provide actionable messages to users
18. Unit test coverage >80% for RBAC and middleware code
19. Integration tests verify end-to-end flows including middleware
20. Performance benchmarks meet targets
21. Documentation covers middleware pattern and all providers
22. Other middleware implementations (audit, observability, rate limit, sanitize) functional

Additional middleware acceptance criteria:

23. Audit middleware logs all requests without blocking
24. Rate limit middleware enforces per-user limits
25. Input sanitization blocks malicious patterns
26. Observability middleware creates traces and metrics
27. Timeout middleware prevents hung requests
28. Middleware can modify context
29. Middleware can modify response
30. Empty middleware chain works (executes handler directly)

===============================================================================
DEPENDENCIES
===============================================================================

External libraries needed:

LDAP:
- go-ldap/ldap package for LDAP client
- Connection pooling and TLS support

OAuth/OIDC:
- coreos/go-oidc for OIDC flows
- golang.org/x/oauth2 for OAuth 2.0

JWT:
- golang-jwt/jwt for JWT parsing and validation

Caching:
- patrickmn/go-cache or similar for TTL cache
- Thread-safe, supports expiration

Audit:
- Standard library encoding/json for JSON formatting
- lumberjack for log rotation

Configuration:
- viper for config file parsing
- Supports YAML, environment variables

No external policy engines in MVP
Consider OPA integration in future phases

===============================================================================
SECURITY CONSIDERATIONS
===============================================================================

Credentials storage:
- Never log credentials or tokens in plaintext
- Use environment variables for secrets
- Support secret management systems (Vault, AWS Secrets Manager)

Token security:
- Short expiration times (1 hour default)
- Encrypted storage of cached tokens
- Secure token transmission (HTTPS only)
- Token revocation support

Policy security:
- Policies stored in version control
- Review process for policy changes
- Principle of least privilege
- Regular policy audits

Audit security:
- Tamper-proof audit logs
- Separate storage from application data
- Retention policies enforced
- Access restricted to security team

Transport security:
- TLS 1.2+ for all external communication
- Certificate validation enforced
- No insecure protocols allowed

===============================================================================
IMPLEMENTATION NOTES
===============================================================================

Start with middleware framework first:
- Define middleware interface before any implementations
- Build chain mechanism that works with empty chain
- Test chain execution order and short-circuit behavior
- Validate context propagation through chain

Build one middleware at a time:
- Implement interface fully for each middleware
- Test middleware in isolation with mocks
- Test middleware in chain with real components
- Validate performance impact of each middleware

RBAC is just another middleware:
- Don't special-case RBAC in agent code
- RBAC middleware uses same interface as others
- Configuration controls middleware ordering
- RBAC can be disabled by removing from chain

Identity providers independent of middleware:
- Start with simplest (API key or local)
- Validate provider pattern before adding complexity
- Each provider in separate package
- Test providers with mocks before real IdP integration

Policy engine is critical path:
- Optimize for read performance
- Cache policy lookups
- Profile and benchmark
- In-memory evaluation only

Middleware composition is key feature:
- Order matters: RBAC before rate limit before LLM
- Each middleware focused on single concern
- Middlewares don't depend on each other
- Chain builder validates configuration

Testing is not optional:
- Write tests alongside implementation
- Integration tests for provider flows
- Load tests for caching and concurrency
- Test middleware ordering combinations

Configuration must be validated:
- Fail fast on startup if misconfigured
- Validate middleware ordering makes sense
- Check provider config completeness
- Provide clear error messages
- Support config validation command

Documentation for operators:
- How to configure middleware pipeline
- How to configure each provider
- Policy syntax examples
- Troubleshooting guide
- Security best practices
- Performance tuning guide

Migration path:
- Middleware framework deployed first
- RBAC disabled by default initially
- Feature flag to enable incrementally
- Graceful degradation if provider unavailable
- Clear error messages guide users
- Support for environment-specific pipelines

Middleware allows experimentation:
- Easy to add new cross-cutting concerns
- Easy to disable problematic middleware
- Easy to reorder for performance
- Each middleware independently testable

===============================================================================
DEVIATIONS NOT PERMITTED
===============================================================================

Do not implement:
- Policy evaluation at LLM level (must be pre-LLM via middleware)
- Shared policy cache across processes (in-memory only)
- Policy DSL different from described YAML format
- Custom authentication protocols (use standard IdP integrations)
- Logging of sensitive data (tokens, credentials, secrets)
- RBAC enforcement outside middleware (no hard-coded checks in agent)
- Middleware that depends on other middleware (must be independent)
- Middleware with side effects that can't be tested in isolation
- Direct handler calls bypassing middleware chain
- RBAC or middleware in joe local (ONLY in joecored)
- Authorization checks in joe local tools (local operations unrestricted)
- Policy evaluation in client code (ONLY in joecored)

Do not change:
- Core middleware interface signature without updating this document
- Middleware execution order semantics (sequential, short-circuit on error)
- Policy evaluation algorithm (wildcard handling, precedence)
- Audit event format (breaking changes require versioning)
- Request context structure (other components depend on it)
- Chain builder interface (consistent API critical)
- Deployment boundary (middleware in joecored, NOT joe local)

Do not skip:
- Token caching (performance requirement)
- Audit logging (compliance requirement)
- Input validation (security requirement)
- Error handling (reliability requirement)
- Unit tests (quality requirement)
- Middleware isolation testing (architectural requirement)
- Performance benchmarking (SLA requirement)
- Chain execution testing (correctness requirement)
- API endpoint protection (all infrastructure endpoints must have middleware)

Do not hard-code:
- Middleware order in agent code (must be configurable)
- Environment-specific logic in middleware (use configuration)
- Provider selection logic (must be pluggable)
- Policy decisions in application code (belongs in policy engine)

Middleware-specific constraints:
- Middleware must not store request state between calls
- Middleware must be thread-safe for concurrent requests
- Middleware must not block on I/O in hot path
- Middleware errors must propagate to caller
- Middleware must not modify request in place (create new)
- Middleware must complete in <10ms individual execution time
- Middleware must not call other middleware directly
- Middleware configuration must be validated at startup

Deployment constraints:
- All middleware MUST run in joecored only
- joe local MUST NOT contain middleware packages
- joe local MUST NOT contain RBAC packages
- joe local MUST make API calls to joecored for infrastructure operations
- joecored API endpoints MUST be protected by middleware
- Local tools in joe local MUST execute directly without API calls

===============================================================================
END OF IMPLEMENTATION REFERENCE
===============================================================================

Questions or clarifications: Update this document, do not deviate from architecture.
