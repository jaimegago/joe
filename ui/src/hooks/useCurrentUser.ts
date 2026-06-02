import { useQuery } from '@tanstack/react-query';
import { fetchCurrentUser } from '@/api/currentUser';
import { ApiRequestError } from '@/api/client';

// useCurrentUser fetches the caller's identity once and shares it
// app-wide. A long staleTime keeps the result from refetching across
// consumers (the sidebar and the LLM settings page both read it); admin
// status does not change within a session. Because the endpoint returns
// is_admin=true when RBAC is disabled, a consumer keying on isAdmin alone
// shows admin surfaces in local auth-disabled mode without a second check.
//
// A 401 is a definitive "not authenticated" answer, not a transient
// failure, so it is never retried — the auth context maps it straight to
// the logged-out state. Other failures retry up to twice.
export function useCurrentUser() {
  return useQuery({
    queryKey: ['current-user'],
    queryFn: fetchCurrentUser,
    staleTime: Infinity,
    retry: (failureCount, error) => {
      if (error instanceof ApiRequestError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}
