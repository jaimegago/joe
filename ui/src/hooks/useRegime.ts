import { useQuery } from '@tanstack/react-query';
import { fetchRegime } from '@/api/regime';
import { ApiRequestError } from '@/api/client';

// useRegime polls the system regime so the app shell can surface an active
// incident. Incident state changes out-of-band (an operator declares/resolves
// via CLI or another session), so unlike useCurrentUser this refetches on an
// interval — 30s is responsive enough for an operator banner without being
// chatty. A 401 is definitive (handled by the auth context) and not retried.
export function useRegime() {
  return useQuery({
    queryKey: ['regime'],
    queryFn: fetchRegime,
    refetchInterval: 30_000,
    refetchOnWindowFocus: true,
    retry: (failureCount, error) => {
      if (error instanceof ApiRequestError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}
