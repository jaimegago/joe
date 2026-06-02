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
  CurrentUserSchema,
  AuthConfigSchema,
  LLMLimitStateSchema,
  LLMCostLimitSchema,
  LLMRunawayCeilingSchema,
  LLMSettingsSchema,
  UsageBreakdownSchema,
  UsageAggregateSchema,
  UsageWindowSchema,
  UsageSessionSchema,
  LLMProviderSchema,
  LLMProvidersSchema,
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
export type CurrentUser = z.infer<typeof CurrentUserSchema>;
export type AuthConfig = z.infer<typeof AuthConfigSchema>;
export type LLMLimitState = z.infer<typeof LLMLimitStateSchema>;
export type LLMCostLimit = z.infer<typeof LLMCostLimitSchema>;
export type LLMRunawayCeiling = z.infer<typeof LLMRunawayCeilingSchema>;
export type LLMSettings = z.infer<typeof LLMSettingsSchema>;
export type UsageBreakdown = z.infer<typeof UsageBreakdownSchema>;
export type UsageAggregate = z.infer<typeof UsageAggregateSchema>;
export type UsageWindow = z.infer<typeof UsageWindowSchema>;
export type UsageSession = z.infer<typeof UsageSessionSchema>;
export type LLMProvider = z.infer<typeof LLMProviderSchema>;
export type LLMProviders = z.infer<typeof LLMProvidersSchema>;
