import { useQuery } from '@tanstack/react-query';
import { fetchCredentialStatuses } from '@/api/credentialStatus';

// useCredentialStatus lists the passive, config-derived credential descriptor for
// every component (GET /admin/credential-status). Admin-gated server-side; the
// caller renders behind RequireAdmin. Pure server-side, so it never probes a
// backend on load — connectivity is only checked by the explicit per-row probe.
export function useCredentialStatus() {
  return useQuery({ queryKey: ['credential-status'], queryFn: fetchCredentialStatuses });
}
