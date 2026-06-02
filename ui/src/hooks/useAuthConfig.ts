import { useQuery } from '@tanstack/react-query';
import { fetchAuthConfig } from '@/api/authConfig';

// useAuthConfig fetches the public, pre-auth capability flags once and shares
// them app-wide. It is the source of the OIDC-button signal on the cold
// logged-out shell: the endpoint is public, so this query resolves whether or
// not a credential is present — including before any /me result. A long
// staleTime keeps it from refetching; the configured-vs-not state does not
// change within a session.
export function useAuthConfig() {
  return useQuery({
    queryKey: ['auth-config'],
    queryFn: fetchAuthConfig,
    staleTime: Infinity,
  });
}
