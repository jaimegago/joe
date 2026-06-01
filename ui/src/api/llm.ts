import { apiClient } from './client';
import {
  LLMSettingsSchema,
  SetActiveModelResponseSchema,
  SetCostLimitResponseSchema,
  SetRunawayCeilingResponseSchema,
  UsageAggregateSchema,
  UsageWindowSchema,
  UsageSessionSchema,
  LLMProvidersSchema,
} from './schemas';
import type {
  LLMSettings,
  UsageAggregate,
  UsageWindow,
  UsageSession,
  LLMProviders,
} from './types';

// Stream G phase G5 — LLM settings, usage, and providers API. Each
// function validates the response through its schema, exactly as the
// security module does.

export type UsageWindowParam = 'hour' | 'day' | 'month';

// Settings (read available to any caller; writes admin-gated server-side).

export function fetchLLMSettings(): Promise<LLMSettings> {
  return apiClient.get<unknown>('/api/v1/llm/settings').then((r) => LLMSettingsSchema.parse(r));
}

export function setActiveModel(name: string): Promise<{ current: string }> {
  return apiClient
    .post<unknown>('/api/v1/llm/settings/active-model', { name })
    .then((r) => SetActiveModelResponseSchema.parse(r));
}

// value is in nano-units of the configured currency, the same scale the
// backend stores and the gate enforces.
export function setCostLimit(
  window: string,
  value: number
): Promise<{ window: string; value: number }> {
  return apiClient
    .post<unknown>('/api/v1/llm/settings/cost-limit', { window, value })
    .then((r) => SetCostLimitResponseSchema.parse(r));
}

export function setRunawayCeiling(value: number): Promise<{ value: number }> {
  return apiClient
    .post<unknown>('/api/v1/llm/settings/runaway-ceiling', { value })
    .then((r) => SetRunawayCeilingResponseSchema.parse(r));
}

// Usage views.

export function fetchUsageAggregate(): Promise<UsageAggregate> {
  return apiClient
    .get<unknown>('/api/v1/llm/usage/aggregate')
    .then((r) => UsageAggregateSchema.parse(r));
}

export function fetchPerModelUsage(window: UsageWindowParam): Promise<UsageWindow> {
  return apiClient
    .get<unknown>(`/api/v1/llm/usage/per-model?window=${window}`)
    .then((r) => UsageWindowSchema.parse(r));
}

export function fetchPerPrincipalUsage(window: UsageWindowParam): Promise<UsageWindow> {
  return apiClient
    .get<unknown>(`/api/v1/llm/usage/per-principal?window=${window}`)
    .then((r) => UsageWindowSchema.parse(r));
}

export function fetchSessionUsage(sessionId: string): Promise<UsageSession> {
  return apiClient
    .get<unknown>(`/api/v1/llm/usage/sessions/${sessionId}`)
    .then((r) => UsageSessionSchema.parse(r));
}

// Providers — read-only presence list.

export function fetchLLMProviders(): Promise<LLMProviders> {
  return apiClient
    .get<unknown>('/api/v1/llm/providers')
    .then((r) => LLMProvidersSchema.parse(r));
}
