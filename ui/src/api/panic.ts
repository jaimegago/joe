import { apiClient } from './client';
import { PanicStatusSchema } from './schemas';
import type { PanicStatus } from './types';

// GET /api/v1/panic/status — is the system in safe mode (panic recovery), and
// if so when it was triggered, by what source, and why. Read by the app-shell
// safe-mode banner. Mirrors fetchRegime.
export function fetchPanicStatus(): Promise<PanicStatus> {
  return apiClient.get<unknown>('/api/v1/panic/status').then((r) => PanicStatusSchema.parse(r));
}
