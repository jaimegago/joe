import { useQuery } from '@tanstack/react-query';
import { fetchRunFindings, fetchProposedAdmissions } from '@/api/runFindings';
import { QUERY_KEYS } from '@/lib/queryKeys';

// useRunFindings reads the latest run's findings on every scope. Polled on the
// same cadence as the components list: a run that lands while the page is open
// shows up without a reload.
export function useRunFindings() {
  return useQuery({
    queryKey: QUERY_KEYS.runFindings,
    queryFn: () => fetchRunFindings(),
    refetchInterval: 30_000,
  });
}

// useProposedAdmissions reads the admin-only proposed-admissions list. Callers
// gate on isAdmin via `enabled` so a non-admin never issues a request that will
// 403.
export function useProposedAdmissions(opts: { enabled: boolean }) {
  return useQuery({
    queryKey: QUERY_KEYS.proposedAdmissions,
    queryFn: fetchProposedAdmissions,
    enabled: opts.enabled,
    refetchInterval: 30_000,
  });
}
