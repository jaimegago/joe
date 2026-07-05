import { useQuery } from '@tanstack/react-query';
import { fetchZones, fetchUnassigned } from '@/api/security';
import { QUERY_KEYS } from '@/lib/queryKeys';

export function useZones() {
  return useQuery({ queryKey: QUERY_KEYS.zones, queryFn: fetchZones });
}

export function useUnassigned() {
  return useQuery({
    queryKey: QUERY_KEYS.unassigned,
    queryFn: fetchUnassigned,
    refetchInterval: 60_000,
  });
}
