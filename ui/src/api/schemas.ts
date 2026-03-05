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

// Alerts
export const AlertSchema = z.object({
  id: z.string(),
  severity: z.enum(['critical', 'warning', 'info']),
  source: z.string(),
  message: z.string(),
  timestamp: z.string(),
  acknowledged: z.boolean(),
});
