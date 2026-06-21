import { apiClient } from './client';
import { ChatMessageSchema, SessionSchema } from './schemas';
import { z } from 'zod';
import type { ChatMessage, Session } from './types';

export function fetchMessages(sessionId: string): Promise<ChatMessage[]> {
  return apiClient
    .get<unknown>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/messages`)
    .then((r) => z.object({ messages: z.array(ChatMessageSchema) }).parse(r).messages);
}

// fetchSessions returns the TEAM-WIDE session list (§12.8 team-public read): every
// session, since any authenticated principal may read any session. Rows the caller
// does not own come back read_only=true with shared_by (the owner) set. Passing
// mine=true narrows to the caller's own sessions (all read_only=false). There is no
// visibility concept and no separate "shared with you" route — that split was
// removed with the team-public read model.
export function fetchSessions(opts?: { mine?: boolean; limit?: number }): Promise<Session[]> {
  const params = new URLSearchParams();
  params.set('limit', String(opts?.limit ?? 20));
  if (opts?.mine) params.set('mine', 'true');
  return apiClient
    .get<unknown>(`/api/v1/sessions?${params.toString()}`)
    .then((r) => z.object({ sessions: z.array(SessionSchema) }).parse(r).sessions);
}

// fetchTrash returns the caller's own trashed sessions (GET /sessions/trash,
// §12.8). Each row carries purge_after — the deadline after which the sweeper
// auto-purges it — so the trash view can show the remaining time before purge.
export function fetchTrash(limit = 20): Promise<Session[]> {
  return apiClient
    .get<unknown>(`/api/v1/sessions/trash?limit=${limit}`)
    .then((r) => z.object({ sessions: z.array(SessionSchema) }).parse(r).sessions);
}

export function createSession(): Promise<Session> {
  return apiClient.post<unknown>('/api/v1/sessions', {}).then((r) => SessionSchema.parse(r));
}

// fetchSession returns a single session's metadata (GET /sessions/{id}). Any
// authenticated principal may read any session that exists (team-public read);
// the owner sees read_only=false, a non-owner read_only=true. A missing session
// yields 404.
export function fetchSession(id: string): Promise<Session> {
  return apiClient
    .get<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}`)
    .then((r) => SessionSchema.parse(r));
}

// updateSessionTitle renames a session (PATCH /sessions/{id}). Owner-checked
// server-side; a non-owner or missing session yields 404.
export function updateSessionTitle(id: string, title: string): Promise<Session> {
  return apiClient
    .patch<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}`, { title })
    .then((r) => SessionSchema.parse(r));
}

// deleteSession SOFT-DELETES a session the caller owns to trash (DELETE
// /sessions/{id}, §12.5 macOS-trash). It is recoverable via restoreSession until
// the sweeper or an admin purges it — this is NOT a hard delete. 204 No Content.
export function deleteSession(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/sessions/${encodeURIComponent(id)}`);
}

// restoreSession returns a trashed session the caller owns back to active (POST
// /sessions/{id}/restore, §12.5). Owner-checked; 409 if the session is not trashed.
export function restoreSession(id: string): Promise<Session> {
  return apiClient
    .post<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}/restore`, {})
    .then((r) => SessionSchema.parse(r));
}

// promoteSessionToIncident is the canonical promote-in-place transition (POST
// /sessions/{id}/promote-incident, §12.3 / §12.8): it promotes an existing default
// session into the incident master in one transaction (flip type, set
// incident_state=declared, attach the declarer as captain, flip the global regime).
// BOTH UI entry points target it — the chat-view promote affordance and the global
// declare control's start-new/promote-existing disambiguation. Authorized by the
// regime-control zone (not the session seam). Returns {session_id, captain_id,
// declared_by}.
export function promoteSessionToIncident(
  id: string
): Promise<{ session_id: string; captain_id: string; declared_by: string }> {
  return apiClient
    .post<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}/promote-incident`, {})
    .then((r) =>
      z.object({ session_id: z.string(), captain_id: z.string(), declared_by: z.string() }).parse(r)
    );
}

// linkSessionToIncident attaches a session to the currently-active incident
// (POST /sessions/{id}/link-incident, §12.3). It only sets linked_incident_id (no
// type flip) — the session stays a plain default conversation participating in the
// incident. Owner-checked (non-owner/missing → 404); 409 when no incident is active.
export function linkSessionToIncident(id: string): Promise<Session> {
  return apiClient
    .post<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}/link-incident`, {})
    .then((r) => SessionSchema.parse(r));
}
