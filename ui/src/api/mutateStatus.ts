import { apiClient } from './client';
import { MutateStatusSchema } from './schemas';
import type { MutateStatus } from './types';

// GET /api/v1/mutate-status — may Joe currently mutate the managed system, and
// why (observation, safe_mode, or full). The reason is derived from the
// boot-resolved write floor and is immutable for the life of the daemon. Read
// by the app-shell observation banner. Mirrors fetchPanicStatus.
export function fetchMutateStatus(): Promise<MutateStatus> {
  return apiClient.get<unknown>('/api/v1/mutate-status').then((r) => MutateStatusSchema.parse(r));
}
