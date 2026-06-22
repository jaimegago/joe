import { apiClient } from './client';
import { RegimeSchema } from './schemas';
import type { Regime } from './types';

// GET /api/v1/regime — is an incident regime active, and if so who declared
// it and when. Read by the app-shell incident banner.
export function fetchRegime(): Promise<Regime> {
  return apiClient.get<unknown>('/api/v1/regime').then((r) => RegimeSchema.parse(r));
}

// POST /api/v1/regime/resolve — end the active incident and return the system to
// normal mode (§R4). The server refuses (409) unless the incident has reached
// 'believed_mitigated', and enforces the regime-control resolve capability (403);
// the caller surfaces those. Returns the resolved session id.
export function resolveIncident(): Promise<{ session_id: string; resolved_by: string }> {
  return apiClient.post<{ session_id: string; resolved_by: string }>('/api/v1/regime/resolve', {});
}

// IncidentWorkState is the pre-resolve lifecycle the UI can advance an incident
// through; resolve takes over at 'believed_mitigated'.
export type IncidentWorkState = 'being_worked' | 'believed_mitigated';

// POST /api/v1/sessions/{id}/incident-state — walk the active incident master
// toward resolution (declared → being_worked → believed_mitigated). Same
// regime-control resolve capability as resolveIncident. Returns the new state.
export function advanceIncidentState(
  sessionId: string,
  state: IncidentWorkState
): Promise<{ session_id: string; incident_state: string }> {
  return apiClient.post<{ session_id: string; incident_state: string }>(
    `/api/v1/sessions/${encodeURIComponent(sessionId)}/incident-state`,
    { state }
  );
}
