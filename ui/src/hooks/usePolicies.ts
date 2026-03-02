import { useQuery } from '@tanstack/react-query';
import { fetchPolicies } from '@/api/security';

export function usePolicies() {
  return useQuery({ queryKey: ['policies'], queryFn: fetchPolicies });
}
