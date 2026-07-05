import type { z } from 'zod';
import type {
  GraphNodeSchema,
  GraphEdgeSchema,
  GraphSchema,
  SubgraphSchema,
  ComponentSchema,
  CreatedComponentSchema,
  SecurityZoneSchema,
  ComponentZoneAssignmentSchema,
  PromotionRequirementsSchema,
  PromotionCandidateSchema,
  PromotionCandidatesSchema,
  PromoteResponseSchema,
  RbacPolicySchema,
  PrincipalRecordSchema,
  AdminSchema,
  ChatMessageSchema,
  ToolCallSchema,
  SessionSchema,
  RetentionPolicySchema,
  PurgeManifestSchema,
  PurgePreviewSchema,
  CurrentUserSchema,
  ZoneAccessSchema,
  RegimeSchema,
  PanicStatusSchema,
  MutateStatusSchema,
  AuthConfigSchema,
  LLMLimitStateSchema,
  LLMCostLimitSchema,
  LLMRunawayCeilingSchema,
  LLMContextBudgetSchema,
  LLMSettingsSchema,
  UsageBreakdownSchema,
  UsageAggregateSchema,
  UsageWindowSchema,
  LLMProviderSchema,
  LLMProvidersSchema,
  CredentialStageSchema,
  CredentialDescriptorSchema,
  CredentialStatusEntrySchema,
  CredentialDiagnosticSchema,
  CredentialProbeResponseSchema,
  ReadPromotionSchema,
  SkillStatusEntrySchema,
  SkillsListResponseSchema,
  SkillsReloadResponseSchema,
  SkillsApprovalResponseSchema,
} from './schemas';

// Types derived from Zod schemas — single source of truth
export type GraphNode = z.infer<typeof GraphNodeSchema>;
export type GraphEdge = z.infer<typeof GraphEdgeSchema>;
export type Graph = z.infer<typeof GraphSchema>;
export type Subgraph = z.infer<typeof SubgraphSchema>;
export type Component = z.infer<typeof ComponentSchema>;
export type CreatedComponent = z.infer<typeof CreatedComponentSchema>;
export type SecurityZone = z.infer<typeof SecurityZoneSchema>;
export type ComponentZoneAssignment = z.infer<typeof ComponentZoneAssignmentSchema>;
export type PromotionRequirements = z.infer<typeof PromotionRequirementsSchema>;
export type PromotionCandidate = z.infer<typeof PromotionCandidateSchema>;
export type PromotionCandidates = z.infer<typeof PromotionCandidatesSchema>;
export type PromoteResponse = z.infer<typeof PromoteResponseSchema>;
export type RbacPolicy = z.infer<typeof RbacPolicySchema>;
export type PrincipalRecord = z.infer<typeof PrincipalRecordSchema>;
export type Admin = z.infer<typeof AdminSchema>;
export type ChatMessage = z.infer<typeof ChatMessageSchema>;
export type ToolCall = z.infer<typeof ToolCallSchema>;
export type Session = z.infer<typeof SessionSchema>;
export type RetentionPolicy = z.infer<typeof RetentionPolicySchema>;
export type PurgeManifest = z.infer<typeof PurgeManifestSchema>;
export type PurgePreview = z.infer<typeof PurgePreviewSchema>;
export type CurrentUser = z.infer<typeof CurrentUserSchema>;
export type ZoneAccess = z.infer<typeof ZoneAccessSchema>;
export type Regime = z.infer<typeof RegimeSchema>;
export type PanicStatus = z.infer<typeof PanicStatusSchema>;
export type ReadPromotion = z.infer<typeof ReadPromotionSchema>;
export type MutateStatus = z.infer<typeof MutateStatusSchema>;
export type AuthConfig = z.infer<typeof AuthConfigSchema>;
export type LLMLimitState = z.infer<typeof LLMLimitStateSchema>;
export type LLMCostLimit = z.infer<typeof LLMCostLimitSchema>;
export type LLMRunawayCeiling = z.infer<typeof LLMRunawayCeilingSchema>;
export type LLMContextBudget = z.infer<typeof LLMContextBudgetSchema>;
export type LLMSettings = z.infer<typeof LLMSettingsSchema>;
export type UsageBreakdown = z.infer<typeof UsageBreakdownSchema>;
export type UsageAggregate = z.infer<typeof UsageAggregateSchema>;
export type UsageWindow = z.infer<typeof UsageWindowSchema>;
export type LLMProvider = z.infer<typeof LLMProviderSchema>;
export type LLMProviders = z.infer<typeof LLMProvidersSchema>;
export type CredentialStage = z.infer<typeof CredentialStageSchema>;
export type CredentialDescriptor = z.infer<typeof CredentialDescriptorSchema>;
export type CredentialStatusEntry = z.infer<typeof CredentialStatusEntrySchema>;
export type CredentialDiagnostic = z.infer<typeof CredentialDiagnosticSchema>;
export type CredentialProbeResponse = z.infer<typeof CredentialProbeResponseSchema>;
export type SkillStatusEntry = z.infer<typeof SkillStatusEntrySchema>;
export type SkillsListResponse = z.infer<typeof SkillsListResponseSchema>;
export type SkillsReloadResponse = z.infer<typeof SkillsReloadResponseSchema>;
export type SkillsApprovalResponse = z.infer<typeof SkillsApprovalResponseSchema>;
