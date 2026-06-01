import { useQuery } from '@tanstack/react-query';
import { fetchCurrentUser } from '@/api/currentUser';

// useCurrentUser fetches the caller's identity once and shares it
// app-wide. A long staleTime keeps the result from refetching across
// consumers (the sidebar and the LLM settings page both read it); admin
// status does not change within a session. Because the endpoint returns
// is_admin=true when RBAC is disabled, a consumer keying on isAdmin alone
// shows admin surfaces in local auth-disabled mode without a second check.
export function useCurrentUser() {
  return useQuery({
    queryKey: ['current-user'],
    queryFn: fetchCurrentUser,
    staleTime: Infinity,
  });
}
