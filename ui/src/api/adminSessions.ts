import { apiClient } from './client';
import { SessionSchema, RetentionPolicySchema, PurgePreviewSchema } from './schemas';
import { z } from 'zod';
import type { Session, RetentionPolicy, PurgePreview } from './types';

// adminSessions.ts drives the admin session-governance namespace
// (/api/v1/admin/sessions, §12.8 / B006). Every call here is admin-gated and (for
// the govern actions) audited server-side; this module only shapes the requests
// and parses the responses — it changes no backend behavior.

export type SessionState = 'active' | 'trashed' | 'archived';

export interface AdminSessionFilters {
  principal?: string;
  type?: string;
  state?: SessionState | '';
  limit?: number;
}

// fetchAdminSessions is the cross-tenant list (§12.8), filterable by principal,
// type, and lifecycle state (active/trashed/archived). An empty state returns
// every session regardless of state.
export function fetchAdminSessions(filters: AdminSessionFilters = {}): Promise<Session[]> {
  const params = new URLSearchParams();
  if (filters.principal) params.set('principal', filters.principal);
  if (filters.type) params.set('type', filters.type);
  if (filters.state) params.set('state', filters.state);
  if (filters.limit) params.set('limit', String(filters.limit));
  const qs = params.toString();
  return apiClient
    .get<unknown>(`/api/v1/admin/sessions${qs ? `?${qs}` : ''}`)
    .then((r) => z.object({ sessions: z.array(SessionSchema) }).parse(r).sessions);
}

// fetchAdminTrash is the cross-tenant all-trash view (§12.8) — every principal's
// trashed sessions, each carrying purge_after for remaining-time rendering.
export function fetchAdminTrash(limit = 100): Promise<Session[]> {
  return apiClient
    .get<unknown>(`/api/v1/admin/sessions/trash?limit=${limit}`)
    .then((r) => z.object({ sessions: z.array(SessionSchema) }).parse(r).sessions);
}

// previewPurge runs the §12.5 manifest-with-hard-stop dry run: confirm=false
// returns the counts a confirmed purge would irreversibly destroy and destroys
// NOTHING. Step one of the two-step.
export function previewPurge(id: string): Promise<PurgePreview> {
  return apiClient
    .post<unknown>(`/api/v1/admin/sessions/${encodeURIComponent(id)}/purge`, { confirm: false })
    .then((r) => PurgePreviewSchema.parse(r));
}

// confirmPurge fires the irreversible expunge (confirm=true) — step two, only
// after the operator has seen the manifest. Returns the destroyed manifest.
export function confirmPurge(id: string): Promise<PurgePreview> {
  return apiClient
    .post<unknown>(`/api/v1/admin/sessions/${encodeURIComponent(id)}/purge`, { confirm: true })
    .then((r) => PurgePreviewSchema.parse(r));
}

// archiveSession moves a session to cold storage (§12.6). Returns 503 when no
// archive provider/directory is configured — the caller surfaces that.
export function archiveSession(id: string): Promise<{ archive_ref: string }> {
  return apiClient
    .post<unknown>(`/api/v1/admin/sessions/${encodeURIComponent(id)}/archive`, {})
    .then((r) => z.object({ status: z.string(), archive_ref: z.string() }).parse(r));
}

// restoreArchive rebuilds an archived session from its artifact (§12.6,
// restore-archive). 422 when the artifact's schema version is unsupported.
export function restoreArchive(id: string): Promise<void> {
  return apiClient
    .post<void>(`/api/v1/admin/sessions/${encodeURIComponent(id)}/restore-archive`, {})
    .then(() => undefined);
}

export function fetchRetentionPolicy(): Promise<RetentionPolicy> {
  return apiClient
    .get<unknown>('/api/v1/admin/sessions/retention-policy')
    .then((r) => RetentionPolicySchema.parse(r));
}

// updateRetentionPolicy writes the §12.5 knobs. inactivity_days null ⇒ OFF (the
// default). A partial body updates only the named knobs server-side.
export function updateRetentionPolicy(patch: {
  inactivity_days?: number | null;
  trash_grace_days?: number;
  terminal_action?: 'trash_then_purge' | 'archive';
}): Promise<RetentionPolicy> {
  return apiClient
    .put<unknown>('/api/v1/admin/sessions/retention-policy', patch)
    .then((r) => RetentionPolicySchema.parse(r));
}
