import { useQuery } from '@tanstack/react-query';
import { fetchReadPosture } from '@/api/security';
import { QUERY_KEYS } from '@/lib/queryKeys';

// useReadPosture shares the install-wide read posture app-wide (read-posture-latch).
// The backing GET /api/v1/admin/read-posture is admin-gated, so callers pass
// `enabled: isAdmin` to avoid a guaranteed 403 for non-admins; the Sidebar and the
// Policies route guard both read it and de-dupe on the shared query key.
export function useReadPosture(options?: { enabled?: boolean }) {
  return useQuery({
    queryKey: QUERY_KEYS.readPosture,
    queryFn: fetchReadPosture,
    enabled: options?.enabled ?? true,
  });
}
