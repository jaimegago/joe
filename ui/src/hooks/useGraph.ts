import { useQuery } from '@tanstack/react-query';
import { fetchGraph, fetchNode, fetchRelated } from '@/api/graph';

export function useGraph() {
  return useQuery({
    queryKey: ['graph'],
    queryFn: fetchGraph,
    refetchInterval: 60_000,
  });
}

export function useNode(id: string | null) {
  return useQuery({
    queryKey: ['graph', 'node', id],
    queryFn: () => fetchNode(id!),
    enabled: id != null,
  });
}

export function useRelated(id: string | null, depth = 1) {
  return useQuery({
    queryKey: ['graph', 'related', id, depth],
    queryFn: () => fetchRelated(id!, depth),
    enabled: id != null,
  });
}
