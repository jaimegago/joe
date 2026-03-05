import type { z } from 'zod';
import type {
  GraphNodeSchema,
  GraphEdgeSchema,
  GraphSchema,
  SubgraphSchema,
  SourceSchema,
  SecurityZoneSchema,
  SourceZoneAssignmentSchema,
  RbacPolicySchema,
  ChatMessageSchema,
  ToolCallSchema,
  SessionSchema,
  ChatResponseSchema,
  AlertSchema,
} from './schemas';

// Types derived from Zod schemas — single source of truth
export type GraphNode = z.infer<typeof GraphNodeSchema>;
export type GraphEdge = z.infer<typeof GraphEdgeSchema>;
export type Graph = z.infer<typeof GraphSchema>;
export type Subgraph = z.infer<typeof SubgraphSchema>;
export type Source = z.infer<typeof SourceSchema>;
export type SecurityZone = z.infer<typeof SecurityZoneSchema>;
export type SourceZoneAssignment = z.infer<typeof SourceZoneAssignmentSchema>;
export type RbacPolicy = z.infer<typeof RbacPolicySchema>;
export type ChatMessage = z.infer<typeof ChatMessageSchema>;
export type ToolCall = z.infer<typeof ToolCallSchema>;
export type Session = z.infer<typeof SessionSchema>;
export type ChatResponse = z.infer<typeof ChatResponseSchema>;
export type Alert = z.infer<typeof AlertSchema>;
