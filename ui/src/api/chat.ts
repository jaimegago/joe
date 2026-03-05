import { apiClient } from './client';
import { ChatMessageSchema, SessionSchema, ChatResponseSchema } from './schemas';
import { z } from 'zod';
import type { ChatMessage, Session, ChatResponse } from './types';

export function sendMessage(sessionId: string, content: string): Promise<ChatResponse> {
  return apiClient
    .post<unknown>('/api/v1/chat', { session_id: sessionId, message: content })
    .then((r) => ChatResponseSchema.parse(r));
}

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
