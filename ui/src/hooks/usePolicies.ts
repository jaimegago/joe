import { useQuery } from '@tanstack/react-query';
import { fetchPolicies } from '@/api/security';
import { QUERY_KEYS } from '@/lib/queryKeys';

export function usePolicies() {
  return useQuery({ queryKey: QUERY_KEYS.policies, queryFn: fetchPolicies });
}
