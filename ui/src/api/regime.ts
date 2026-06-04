import { apiClient } from './client';
import { RegimeSchema } from './schemas';
import type { Regime } from './types';

// GET /api/v1/regime — is an incident regime active, and if so who declared
// it and when. Read by the app-shell incident banner.
export function fetchRegime(): Promise<Regime> {
  return apiClient.get<unknown>('/api/v1/regime').then((r) => RegimeSchema.parse(r));
}
