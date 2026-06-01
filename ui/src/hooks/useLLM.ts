import { useQuery } from '@tanstack/react-query';
import {
  fetchLLMSettings,
  fetchLLMProviders,
  fetchUsageAggregate,
  fetchPerModelUsage,
  fetchPerPrincipalUsage,
  type UsageWindowParam,
} from '@/api/llm';

export function useLLMSettings() {
  return useQuery({ queryKey: ['llm-settings'], queryFn: fetchLLMSettings });
}

export function useLLMProviders() {
  return useQuery({ queryKey: ['llm-providers'], queryFn: fetchLLMProviders });
}

export function useUsageAggregate() {
  return useQuery({ queryKey: ['llm-usage', 'aggregate'], queryFn: fetchUsageAggregate });
}

export function usePerModelUsage(window: UsageWindowParam) {
  return useQuery({
    queryKey: ['llm-usage', 'per-model', window],
    queryFn: () => fetchPerModelUsage(window),
  });
}

// usePerPrincipalUsage is gated on the caller's admin status: the
// endpoint is admin-only server-side, so a non-admin must not even
// request it. enabled=false keeps the query from firing for non-admins.
export function usePerPrincipalUsage(window: UsageWindowParam, enabled: boolean) {
  return useQuery({
    queryKey: ['llm-usage', 'per-principal', window],
    queryFn: () => fetchPerPrincipalUsage(window),
    enabled,
  });
}
