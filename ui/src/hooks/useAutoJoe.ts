import { useQuery } from '@tanstack/react-query';
import { fetchAutoRuns, fetchAutoSchedules } from '@/api/autoRuns';
import { QUERY_KEYS } from '@/lib/queryKeys';

// The Auto Joe page's two lists. Polled every 5 s: a run moves from running
// to completed in seconds and a schedule's last outcome changes at the
// scheduler's tick, and the page shows the record's state without a reload.
// The cadence is the implementer's choice (autojoe-ro-run-visibility).
export const AUTO_JOE_POLL_MS = 5_000;

export function useAutoRuns() {
  return useQuery({
    queryKey: QUERY_KEYS.autoRuns,
    queryFn: fetchAutoRuns,
    refetchInterval: AUTO_JOE_POLL_MS,
  });
}

export function useAutoSchedules() {
  return useQuery({
    queryKey: QUERY_KEYS.autoSchedules,
    queryFn: fetchAutoSchedules,
    refetchInterval: AUTO_JOE_POLL_MS,
  });
}
