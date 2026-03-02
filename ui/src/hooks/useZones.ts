import { useQuery } from '@tanstack/react-query';
import { fetchZones, fetchUnassigned } from '@/api/security';

export function useZones() {
  return useQuery({ queryKey: ['zones'], queryFn: fetchZones });
}

export function useUnassigned() {
  return useQuery({ queryKey: ['unassigned'], queryFn: fetchUnassigned, refetchInterval: 60_000 });
}
