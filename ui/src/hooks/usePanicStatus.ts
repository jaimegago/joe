import { useQuery } from '@tanstack/react-query';
import { fetchPanicStatus } from '@/api/panic';
import { ApiRequestError } from '@/api/client';

// usePanicStatus polls the panic/safe-mode status so the app shell can surface
// an active safe-mode state. Like the regime, safe mode changes out-of-band (an
// operator triggers it via CLI, SIGUSR1, or another session, and clears it with
// `joe unlock`), so this refetches on an interval rather than once.
//
// REVIEW: poll interval. useRegime polls every 30s. Safe mode is the more
// time-sensitive, more-restrictive state — every write is blocked while it is
// active — so this is set to 15s to shorten the window where an operator who
// just unlocked (or just triggered) sees a stale banner. Final value left for
// review; bump to 30_000 to match useRegime if consistency is preferred.
const SAFE_MODE_POLL_INTERVAL_MS = 15_000;

export function usePanicStatus() {
  return useQuery({
    queryKey: ['panic-status'],
    queryFn: fetchPanicStatus,
    refetchInterval: SAFE_MODE_POLL_INTERVAL_MS,
    refetchOnWindowFocus: true,
    retry: (failureCount, error) => {
      if (error instanceof ApiRequestError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}
