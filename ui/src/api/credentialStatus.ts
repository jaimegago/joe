import { apiClient } from './client';
import {
  CredentialStatusEntrySchema,
  CredentialProbeResponseSchema,
} from './schemas';
import { z } from 'zod';
import type { CredentialStatusEntry, CredentialProbeResponse } from './types';

// Credential authz/connectivity status (D-0026 unit 3). Admin-gated server-side;
// every caller renders behind RequireAdmin. None of these calls ever requests or
// receives credential material — only the diagnostic/descriptor halves, and (for
// the stderr endpoint) the deliberately-surfaced captured plugin output.

// fetchCredentialStatuses returns the PASSIVE, config-derived status for every
// component — GET /api/v1/admin/credential-status. Pure server-side (Describe),
// so loading this list never probes a backend.
export function fetchCredentialStatuses(): Promise<CredentialStatusEntry[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/credential-status')
    .then((r) => z.object({ statuses: z.array(CredentialStatusEntrySchema) }).parse(r).statuses);
}

// probeCredential runs a LIVE connectivity probe for one component — POST
// /api/v1/admin/credential-status/{id}/probe. Deliberate, never automatic. The
// response carries the staged diagnostic and a flag for whether captured stderr
// exists; the stderr text itself is fetched separately and only on request.
export function probeCredential(componentId: string): Promise<CredentialProbeResponse> {
  return apiClient
    .post<unknown>(
      `/api/v1/admin/credential-status/${encodeURIComponent(componentId)}/probe`,
      {}
    )
    .then((r) => CredentialProbeResponseSchema.parse(r));
}

// fetchCredentialStderr is the explicit "show plugin output" path — POST
// /api/v1/admin/credential-status/{id}/probe/stderr. It is called ONLY when the
// operator deliberately expands the plugin output, so the untrusted,
// possibly-secret-bearing stderr reaches the browser only when asked for.
export function fetchCredentialStderr(componentId: string): Promise<string> {
  return apiClient
    .post<unknown>(
      `/api/v1/admin/credential-status/${encodeURIComponent(componentId)}/probe/stderr`,
      {}
    )
    .then((r) => z.object({ component_id: z.string(), stderr: z.string() }).parse(r).stderr);
}
