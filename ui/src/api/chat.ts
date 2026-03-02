import { apiClient } from './client';
import type { ChatMessage, Session } from './types';

export interface ChatResponse {
  message: ChatMessage;
  toolCalls?: ChatMessage['toolCalls'];
}

export function sendMessage(sessionId: string, content: string): Promise<ChatResponse> {
  return apiClient.post<ChatResponse>('/api/v1/chat', { session_id: sessionId, message: content });
}

export function fetchMessages(sessionId: string): Promise<ChatMessage[]> {
  return apiClient.get<{ messages: ChatMessage[] }>(`/api/v1/sessions/${encodeURIComponent(sessionId)}/messages`).then(r => r.messages ?? []);
}

export function fetchSessions(limit = 20): Promise<Session[]> {
  return apiClient.get<{ sessions: Session[] }>(`/api/v1/sessions?limit=${limit}`).then(r => r.sessions ?? []);
}

export function createSession(): Promise<Session> {
  return apiClient.post<Session>('/api/v1/sessions', {});
}
