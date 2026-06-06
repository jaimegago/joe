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
  return apiClient
    .post<unknown>('/api/v1/sessions', {})
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
