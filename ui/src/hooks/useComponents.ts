import { useQuery } from '@tanstack/react-query';
import { fetchComponents } from '@/api/components';
import { QUERY_KEYS } from '@/lib/queryKeys';

export function useComponents() {
  return useQuery({
    queryKey: QUERY_KEYS.components,
    queryFn: fetchComponents,
    refetchInterval: 30_000,
  });
}
