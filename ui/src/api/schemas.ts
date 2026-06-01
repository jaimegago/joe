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

// Sources
export const SourceSchema = z.object({
  id: z.string(),
  type: z.string(),
  name: z.string(),
  zone: z.string().optional(),
  config: z.record(z.string(), z.unknown()),
  status: z.string(),
  last_sync_at: z.string().optional(),
  last_error: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
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

export const SourceZoneAssignmentSchema = z.object({
  source_id: z.string(),
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
  ended_at: z.string().optional(),
  summary: z.string().optional(),
  message_count: z.number(),
});

export const ChatResponseSchema = z.object({
  message: ChatMessageSchema,
  toolCalls: z.array(ToolCallSchema).optional(),
});

// Current user (Stream G phase G5 — GET /api/v1/me)
export const CurrentUserSchema = z.object({
  principal: z.string(),
  is_admin: z.boolean(),
  rbac_enabled: z.boolean(),
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

export const LLMSettingsSchema = z.object({
  active_model: z.string(),
  cost_limits: z.array(LLMCostLimitSchema),
  runaway_ceiling: LLMRunawayCeilingSchema,
});

// LLM settings write request/response bodies.
export const SetActiveModelResponseSchema = z.object({ current: z.string() });
export const SetCostLimitResponseSchema = z.object({
  window: z.string(),
  value: z.number(),
});
export const SetRunawayCeilingResponseSchema = z.object({ value: z.number() });

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

// Alerts
export const AlertSchema = z.object({
  id: z.string(),
  severity: z.enum(['critical', 'warning', 'info']),
  source: z.string(),
  message: z.string(),
  timestamp: z.string(),
  acknowledged: z.boolean(),
});
