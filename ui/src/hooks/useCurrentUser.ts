import { useQuery } from '@tanstack/react-query';
import { fetchCurrentUser } from '@/api/currentUser';
import { ApiRequestError } from '@/api/client';

// useCurrentUser fetches the caller's identity and shares it app-wide. The
// many consumers (the sidebar, the LLM settings page, AuthContext) do not each
// trigger a fetch: React Query dedupes concurrent observers of the same key
// into one in-flight request regardless of staleTime. Because the endpoint
// returns is_admin=true when RBAC is disabled, a consumer keying on isAdmin
// alone shows admin surfaces in local auth-disabled mode without a second check.
//
// staleTime is deliberately FINITE (not Infinity) and refetchOnWindowFocus is
// on so the identity re-resolves from the current cookie when the user returns
// to the tab. The session is cookie-authenticated and that cookie is shared
// across every tab/window of the browser profile (and across all of Chrome's
// incognito windows); logging in as a different user in one tab silently
// rebinds the cookie for the others. Without revalidation a backgrounded tab
// keeps an Infinity-cached identity while its requests now carry the new
// cookie, so it paints the new user's data under the old user's name. A focus
// refetch surfaces the change, which AuthContext turns into a cache purge.
const CURRENT_USER_STALE_MS = 30_000;

export function useCurrentUser() {
  return useQuery({
    queryKey: ['current-user'],
    queryFn: fetchCurrentUser,
    staleTime: CURRENT_USER_STALE_MS,
    refetchOnWindowFocus: true,
    retry: (failureCount, error) => {
      if (error instanceof ApiRequestError && error.status === 401) return false;
      return failureCount < 2;
    },
  });
}
