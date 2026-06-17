import { z } from 'zod';

// Graph
export const GraphNodeSchema = z.object({
  id: z.string(),
  kind: z.string(),
  name: z.string(),
  namespace: z.string().optional(),
  cluster: z.string().optional(),
  metadata: z.record(z.string(), z.unknown()),
  labels: z.record(z.string(), z.unknown()).optional(),
  status: z.string().optional(),
});

export const GraphEdgeSchema = z.object({
  id: z.string(),
  source: z.string(),
  target: z.string(),
  type: z.string(),
  metadata: z.record(z.string(), z.unknown()).optional(),
});

export const GraphSchema = z.object({
  nodes: z.array(GraphNodeSchema),
  edges: z.array(GraphEdgeSchema),
});

export const SubgraphSchema = GraphSchema;

// Components
export const ComponentSchema = z.object({
  id: z.string(),
  type: z.string(),
  name: z.string(),
  zone: z.string().optional(),
  // A002 read-model fix: GET /api/v1/components and /components/{id} no longer
  // serialize the raw config blob (it carried credential reference locators for
  // an armed component). In its place the server sends a derived arm-state
  // projection: `armed` is always present; `provider` (the credential provider
  // Kind) is present only when armed and absent/empty for an inert component.
  armed: z.boolean(),
  provider: z.string().optional(),
  status: z.string(),
  last_sync_at: z.string().optional(),
  last_error: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

// ComponentTypesSchema is the response of GET /api/v1/component-types — the
// authoritative component-type enum (store.AllowedComponentTypes), so the
// registration form populates its selector without a hardcoded TS list.
export const ComponentTypesSchema = z.object({
  component_types: z.array(z.string()),
  count: z.number().optional(),
});

// CreatedComponentSchema validates the 201 body of POST /api/v1/components. It
// is deliberately lenient where ComponentSchema is strict: a config-less
// registration returns config: null, and (under the at-rest encryption wrapper)
// zero-value status/timestamps — only id/type/name are reliably populated on
// the create response. The form only needs id/type/name to confirm the write.
export const CreatedComponentSchema = z.object({
  id: z.string(),
  type: z.string(),
  name: z.string(),
  config: z.record(z.string(), z.unknown()).nullable().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
  updated_at: z.string().optional(),
});

// Component promotion (A002 operator-facing arm form). The two describe
// endpoints and the promote write back the privilege surface where an admin
// supplies a credential REFERENCE — never a secret — to arm an inert component.

// One locator field an operator supplies as part of a credential reference.
export const PromotionLocatorFieldSchema = z.object({
  name: z.string(),
  required: z.boolean(),
});

// A Kind-level cross-field rule the form switches on to render the right
// affordance (e.g. at-least-one-of {in_cluster, kubeconfig}).
export const PromotionConstraintSchema = z.object({
  rule: z.string(),
  fields: z.array(z.string()),
  message: z.string(),
});

// GET /api/v1/components/{id}/promotion-requirements — the cacheable SHAPE of
// the reference. Discriminated on `wired`: a wired component carries the
// provider kind + locator field shape + constraints; an unwired type carries
// only the sorted set of types that CAN be armed (200, not a 400).
export const PromotionRequirementsSchema = z.discriminatedUnion('wired', [
  z.object({
    type: z.string(),
    wired: z.literal(true),
    kind: z.string(),
    locator_fields: z.array(PromotionLocatorFieldSchema),
    constraints: z.array(PromotionConstraintSchema),
  }),
  z.object({
    type: z.string(),
    wired: z.literal(false),
    armable_types: z.array(z.string()),
  }),
]);

// One live credential reference the admin may choose: a human label and the
// composed reference name (the static provider's env var name). Never a value.
export const PromotionCandidateSchema = z.object({
  label: z.string(),
  env_var_name: z.string(),
});

// GET /api/v1/components/{id}/promotion-candidates — the LIVE candidate set
// (not cacheable; reflects Joe's environment at request time). Discriminated on
// `wired`. A wired component reports applicability + (static) the enumerable
// candidates and their env-name prefix; kubeconfig-exec answers applicable:false
// with no candidates. An unwired type mirrors the requirements shape. `prefix`
// is omitted by the server when not applicable.
export const PromotionCandidatesSchema = z.discriminatedUnion('wired', [
  z.object({
    type: z.string(),
    wired: z.literal(true),
    kind: z.string(),
    prefix: z.string().optional(),
    applicable: z.boolean(),
    candidates: z.array(PromotionCandidateSchema),
  }),
  z.object({
    type: z.string(),
    wired: z.literal(false),
    armable_types: z.array(z.string()),
  }),
]);

// POST /api/v1/components/{id}/promote response — OUTCOME ONLY, never echoes
// the reference. `rearm` is true when an already-armed component's reference
// was rotated rather than armed for the first time.
export const PromoteResponseSchema = z.object({
  component_id: z.string(),
  type: z.string(),
  provider: z.string(),
  armed: z.boolean(),
  rearm: z.boolean(),
});

// Security / RBAC
export const SecurityZoneSchema = z.object({
  id: z.string(),
  name: z.string(),
  description: z.string(),
  allowed_actions: z.array(z.string()),
  created_at: z.string().optional(),
  sourceCount: z.number().optional(),
});

export const ComponentZoneAssignmentSchema = z.object({
  component_id: z.string(),
  zone_id: z.string(),
  assigned_by: z.string(),
  assigned_at: z.string(),
  reason: z.string().optional(),
});

export const RbacPolicySchema = z.object({
  id: z.number(),
  principal: z.string(),
  zone_id: z.string(),
  created_at: z.string(),
});

// Identity registry entry (Stage 1-3 — GET /api/v1/admin/principals). A row is
// created at a principal's first OIDC login; status flips between 'active' and
// 'disabled' on admin disable/enable. disabled_at/disabled_by are present only
// while disabled; display_name and last_seen_at are present when known (the Go
// struct omits them when empty, so they are optional here).
export const PrincipalRecordSchema = z.object({
  principal: z.string(),
  created_at: z.string(),
  status: z.enum(['active', 'disabled']),
  disabled_at: z.string().optional(),
  disabled_by: z.string().optional(),
  display_name: z.string().optional(),
  last_seen_at: z.string().optional(),
});

// Admin roster entry (GET /api/v1/admin/admins). granted_by/reason are always
// sent by the server (no omitempty), so they are required here.
export const AdminSchema = z.object({
  principal: z.string(),
  granted_at: z.string(),
  granted_by: z.string(),
  reason: z.string(),
});

// Chat / Sessions
export const ToolCallSchema = z.object({
  id: z.string(),
  name: z.string(),
  arguments: z.record(z.string(), z.unknown()),
  result: z.string().optional(),
  status: z.enum(['pending', 'success', 'error']),
});

export const ChatMessageSchema = z.object({
  id: z.number(),
  session_id: z.string(),
  role: z.enum(['user', 'assistant']),
  content: z.string(),
  tool_name: z.string().optional(),
  tool_args: z.record(z.string(), z.unknown()).optional(),
  created_at: z.string(),
  toolCalls: z.array(ToolCallSchema).optional(),
});

export const SessionSchema = z.object({
  id: z.string(),
  started_at: z.string(),
  // last_activity_at is the recency key the browse list sorts/labels by; the
  // server omits it only on legacy rows, so it is optional.
  last_activity_at: z.string().optional(),
  ended_at: z.string().optional(),
  summary: z.string().optional(),
  message_count: z.number(),
  // title is the human-editable session label (Phase 2); visibility is
  // 'private' | 'public' (Phase 3).
  title: z.string().optional(),
  visibility: z.string().optional(),
  // read_only: true when the caller is a non-owner viewing a public session
  // (Phase 3 access matrix), false when the caller owns it. The server always
  // sends it explicitly, so owner-only controls can gate on the positive signal
  // (read_only === false) and fail closed if it is ever absent. Kept optional
  // here only for backward/defensive parsing — absent is treated as "not owner".
  read_only: z.boolean().optional(),
  // linked_incident_id is the active incident this session is attached to
  // (Phase 4 incident linkage), or absent when unlinked. Its presence drives
  // the incident badge on the session row and chat header.
  linked_incident_id: z.string().optional(),
  // shared_by is the owning principal of a public session surfaced in the
  // "shared with you" list (GET /sessions/shared). Present only on that list;
  // the owner's own list and per-id GET never include it. Drives the
  // "read-only · shared by <owner>" label.
  shared_by: z.string().optional(),
});

// Current user (Stream G phase G5 — GET /api/v1/me)

// ZoneAccessSchema is one zone the caller can reach, with the actions that
// zone permits. The zones array is the data the zero-zone empty state keys on.
export const ZoneAccessSchema = z.object({
  id: z.string(),
  allowed_actions: z.array(z.string()),
});

export const CurrentUserSchema = z.object({
  principal: z.string(),
  is_admin: z.boolean(),
  rbac_enabled: z.boolean(),
  // Stream H2 — app-wide capability flag: whether OIDC human login is
  // configured. Always sent by the server, so required here.
  oidc_enabled: z.boolean(),
  // Zones the caller can reach (admin: all; non-admin: their granted zones;
  // zero-zone caller: []). Always sent by the server as an array. Optional in
  // the schema with a [] default so a stale cached response without the field
  // still parses as "no zones known" rather than throwing.
  zones: z.array(ZoneAccessSchema).default([]),
});

// System regime (incident mode) — GET /api/v1/regime.
//
// The server marshals sessionmodel.Regime with no JSON tags, so the wire keys
// are the exported Go field names (Mode, DeclaredAt, ...). The schema accepts
// those and transforms to a lower-cased, null-normalized shape the UI
// consumes. DeclaredAt/DeclaredByPrincipal/DeclaredKind are null when the mode
// is normal.
export const RegimeSchema = z
  .object({
    Mode: z.string(),
    DeclaredAt: z.string().nullable().optional(),
    DeclaredByPrincipal: z.string().nullable().optional(),
    DeclaredKind: z.string().nullable().optional(),
  })
  .transform((r) => ({
    mode: r.Mode,
    declaredAt: r.DeclaredAt ?? null,
    declaredByPrincipal: r.DeclaredByPrincipal ?? null,
    declaredKind: r.DeclaredKind ?? null,
  }));

// Panic / safe mode status — GET /api/v1/panic/status.
//
// Unlike the regime endpoint, panicStatusResponse carries explicit snake_case
// JSON tags (internal/api/panic.go), so the wire keys are already lower-cased.
// The detail fields are omitted (absent) when safe mode is off — the endpoint
// returns just {safe_mode:false} in that case — so they are optional here and
// normalize to null. Schema mirrors RegimeSchema's accept-then-transform style.
export const PanicStatusSchema = z
  .object({
    safe_mode: z.boolean(),
    triggered_at: z.string().nullable().optional(),
    trigger_source: z.string().nullable().optional(),
    trigger_reason: z.string().nullable().optional(),
  })
  .transform((r) => ({
    safeMode: r.safe_mode,
    triggeredAt: r.triggered_at ?? null,
    triggerSource: r.trigger_source ?? null,
    triggerReason: r.trigger_reason ?? null,
  }));

// Mutate status — GET /api/v1/mutate-status.
//
// Like panicStatusResponse (and unlike the tagless Regime struct),
// mutateStatusResponse carries explicit snake_case JSON tags
// (internal/api/mutatestatus.go), so the wire keys are already lower-cased and
// consumed directly with no key transform. reason is always one of the three
// listed values — never the empty string. Read by the app-shell observation
// banner, which gates on reason === "observation".
export const MutateStatusSchema = z.object({
  can_mutate: z.boolean(),
  reason: z.enum(['observation', 'safe_mode', 'full']),
});

// Public auth config (Stream H2 follow-up — GET /api/v1/auth/config).
//
// The single app-wide capability the logged-out shell needs before any
// credential exists: whether OIDC human login is configured. Served under the
// public /api/v1/auth/ prefix so it is readable pre-auth, unlike /me.
export const AuthConfigSchema = z.object({
  oidc_enabled: z.boolean(),
});

// LLM settings (Stream G phase G5 — GET /api/v1/llm/settings)
//
// State is the backend's configured-vs-backstop label; effective is the
// number the enforcement gate actually applies (the substituted backstop
// for an unset window, the operator value for a configured positive one,
// and 0 for an explicit-disable). See internal/api/llmsettings.go.
export const LLMLimitStateSchema = z.enum(['backstop_fallback', 'configured']);

export const LLMCostLimitSchema = z.object({
  window: z.string(),
  stored_raw: z.number(),
  state: LLMLimitStateSchema,
  effective: z.number(),
});

export const LLMRunawayCeilingSchema = z.object({
  stored_raw: z.number(),
  state: LLMLimitStateSchema,
  effective: z.number(),
});

// context_budget mirrors runaway_ceiling's shape, but the values are
// fractions of the model's context window (in (0, 1.0]) rather than token
// counts. effective is the fraction the agentic path actually budgets with —
// the backstop-substituted default when state is "backstop_fallback", the
// stored fraction when "configured". See internal/api/llmsettings.go
// contextBudgetView.
export const LLMContextBudgetSchema = z.object({
  stored_raw: z.number(),
  state: LLMLimitStateSchema,
  effective: z.number(),
});

export const LLMSettingsSchema = z.object({
  active_model: z.string(),
  cost_limits: z.array(LLMCostLimitSchema),
  runaway_ceiling: LLMRunawayCeilingSchema,
  context_budget: LLMContextBudgetSchema,
});

// LLM settings write request/response bodies.
export const SetActiveModelResponseSchema = z.object({ current: z.string() });
export const SetCostLimitResponseSchema = z.object({
  window: z.string(),
  value: z.number(),
});
export const SetRunawayCeilingResponseSchema = z.object({ value: z.number() });
// The context-budget POST echoes the accepted fraction back (the handler
// returns {"fraction": ...}, NOT {"value": ...} like the other two writes).
export const SetContextBudgetResponseSchema = z.object({ fraction: z.number() });

// LLM usage views (Stream G phase G5). Every row carries its own currency
// so display surfaces can subtotal per currency without ever summing
// across currencies. model/principal/session_id are present only on the
// breakdown that groups by them.
export const UsageBreakdownSchema = z.object({
  calls: z.number(),
  input_tokens: z.number(),
  output_tokens: z.number(),
  estimated_cost_nano: z.number(),
  currency: z.string(),
  model: z.string().optional(),
  principal: z.string().optional(),
  session_id: z.string().optional(),
});

export const UsageAggregateSchema = z.object({
  today: z.array(UsageBreakdownSchema),
  week: z.array(UsageBreakdownSchema),
  month: z.array(UsageBreakdownSchema),
});

export const UsageWindowSchema = z.object({
  window: z.string(),
  rows: z.array(UsageBreakdownSchema),
});

export const UsageSessionSchema = z.object({
  session_id: z.string(),
  rows: z.array(UsageBreakdownSchema),
});

// LLM providers (Stream G phase G5 — GET /api/v1/llm/providers). Booleans
// only; the response never carries key material, only key presence.
export const LLMProviderSchema = z.object({
  name: z.string(),
  provider: z.string(),
  model: z.string(),
  configured: z.boolean(),
  key_present: z.boolean(),
});

export const LLMProvidersSchema = z.object({
  providers: z.array(LLMProviderSchema),
  current: z.string(),
});

// Credential authz/connectivity status (D-0026 unit 3). These mirror the
// serializable halves of the resolved-credential type — the Descriptor (pure
// config-derived fact) and the Diagnostic (staged Resolve/Probe outcome). No
// schema carries credential material; the captured plugin stderr arrives only
// from its own dedicated endpoint, never inline.

// The four diagnostic stages, in order. mint-succeeded WITHOUT
// connectivity-probed is the legal "minted, not yet proven" lazy state.
export const CredentialStageSchema = z.enum([
  'provider-selected',
  'mint-attempted',
  'mint-succeeded',
  'connectivity-probed',
]);

export const CredentialDescriptorSchema = z.object({
  provider: z.string(),
  audience: z.string().optional(),
  context: z.string().optional(),
  expires_at: z.string().optional(),
});

// One component's passive status row. descriptor is absent and error is set
// when the component's config has no usable/parseable provider.
export const CredentialStatusEntrySchema = z.object({
  component_id: z.string(),
  type: z.string(),
  name: z.string(),
  descriptor: CredentialDescriptorSchema.optional(),
  error: z.string().optional(),
});

export const CredentialDiagnosticSchema = z.object({
  component_id: z.string(),
  provider: z.string(),
  audience: z.string().optional(),
  expires_at: z.string().optional(),
  stage: CredentialStageSchema,
  ok: z.boolean(),
  reason: z.string().optional(),
});

// The live probe response: the staged diagnostic plus a flag that captured
// stderr exists. The stderr text itself is NOT here — it is fetched separately.
export const CredentialProbeResponseSchema = z.object({
  component_id: z.string(),
  diagnostic: CredentialDiagnosticSchema,
  stderr_available: z.boolean(),
});

// Alerts
export const AlertSchema = z.object({
  id: z.string(),
  severity: z.enum(['critical', 'warning', 'info']),
  source: z.string(),
  message: z.string(),
  timestamp: z.string(),
  acknowledged: z.boolean(),
});
