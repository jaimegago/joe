import { apiClient } from './client';
import { RunFindingsResponseSchema } from './schemas';
import type { RunFindingsResponse } from './types';

// fetchRunFindings reads the typed finding records a read-only Auto Joe run
// produced: the latest run's findings on every scope, by band, with their
// new/standing/resolved/unknown labels (GET /api/v1/run-findings). With runId
// it reads one run's set. This is the endpoint of its own the findings view
// reads — real records, not a stub (D-0038 retired the alerts widget for
// lacking exactly this).
export function fetchRunFindings(runId?: string): Promise<RunFindingsResponse> {
  const path = runId
    ? `/api/v1/run-findings?run_id=${encodeURIComponent(runId)}`
    : '/api/v1/run-findings';
  return apiClient.get<unknown>(path).then((r) => RunFindingsResponseSchema.parse(r));
}

// fetchProposedAdmissions reads the graph-update findings of the latest runs —
// proposals to register a component or accept a relationship — which the
// server gates to admins because the action they propose is an admission
// (D-0029). The run applies nothing; this list is information for the human
// who can.
export function fetchProposedAdmissions(): Promise<RunFindingsResponse> {
  return apiClient
    .get<unknown>('/api/v1/run-findings/admissions')
    .then((r) => RunFindingsResponseSchema.parse(r));
}
