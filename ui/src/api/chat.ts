import { apiClient } from './client';
import { ChatMessageSchema, SessionSchema } from './schemas';
import { z } from 'zod';
import type { ChatMessage, Session } from './types';

export function fetchMessages(sessionId: string): Promise<ChatMessage[]> {
  return apiClient
    .get<unknown>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/messages`)
    .then((r) => z.object({ messages: z.array(ChatMessageSchema) }).parse(r).messages);
}

export function fetchSessions(limit = 20): Promise<Session[]> {
  return apiClient
    .get<unknown>(`/api/v1/sessions?limit=${limit}`)
    .then((r) => z.object({ sessions: z.array(SessionSchema) }).parse(r).sessions);
}

export function createSession(): Promise<Session> {
  return apiClient.post<unknown>('/api/v1/sessions', {}).then((r) => SessionSchema.parse(r));
}

// fetchSession returns a single session's metadata (GET /sessions/{id}). The
// owner sees it with read_only=false; a non-owner sees it only when it is
// public, flagged read_only=true; a private session owned by someone else (or
// a missing one) yields 404.
export function fetchSession(id: string): Promise<Session> {
  return apiClient
    .get<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}`)
    .then((r) => SessionSchema.parse(r));
}

// updateSessionVisibility flips a session between 'private' and 'public' (PATCH
// /sessions/{id}). Owner-checked server-side; a non-owner or missing session
// yields 404.
export function updateSessionVisibility(
  id: string,
  visibility: 'private' | 'public'
): Promise<Session> {
  return apiClient
    .patch<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}`, { visibility })
    .then((r) => SessionSchema.parse(r));
}

// updateSessionTitle renames a session (PATCH /sessions/{id}). Owner-checked
// server-side; a non-owner or missing session yields 404.
export function updateSessionTitle(id: string, title: string): Promise<Session> {
  return apiClient
    .patch<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}`, { title })
    .then((r) => SessionSchema.parse(r));
}

// deleteSession removes a session and (via ON DELETE CASCADE) its messages.
export function deleteSession(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/sessions/${encodeURIComponent(id)}`);
}

// linkSessionToIncident attaches a session to the currently-active incident
// (POST /sessions/{id}/link-incident, Phase 4). The server records
// linked_incident_id and promotes the session to an incident investigation.
// Owner-checked server-side (non-owner/missing → 404); 409 when no incident is
// active.
export function linkSessionToIncident(id: string): Promise<Session> {
  return apiClient
    .post<unknown>(`/api/v1/sessions/${encodeURIComponent(id)}/link-incident`, {})
    .then((r) => SessionSchema.parse(r));
}
