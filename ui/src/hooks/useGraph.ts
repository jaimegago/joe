import { useQuery } from '@tanstack/react-query';
import { fetchGraph } from '@/api/graph';

export function useGraph() {
  return useQuery({
    queryKey: ['graph'],
    queryFn: fetchGraph,
    refetchInterval: 60_000,
  });
}
