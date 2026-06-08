import { useQuery } from '@tanstack/react-query';
import { fetchMutateStatus } from '@/api/mutateStatus';
import { ApiRequestError } from '@/api/client';

// useMutateStatus reads the boot-resolved mutate status so the app shell can
// surface an active observation-mode posture. Unlike usePanicStatus/useRegime,
// this value is boot-immutable: the write floor is resolved once at startup and
// can only change across a daemon restart (D-0018/D-0019). So there is NO
// refetchInterval — polling would never observe a change within a run. We still
// keep refetchOnWindowFocus: true so a daemon restart that happened while the
// tab was backgrounded is picked up when the operator returns to the tab. A 401
// is definitive (handled by the auth context) and not retried.
export function useMutateStatus() {
  return useQuery({
    queryKey: ['mutate-status'],
    queryFn: fetchMutateStatus,
    refetchOnWindowFocus: true,
    retry: (failureCount, error) => {
      if (error instanceof ApiRequestError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}
