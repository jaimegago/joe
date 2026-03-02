import { apiClient } from './client';
import type { Source } from './types';

export function fetchSources(): Promise<Source[]> {
  return apiClient.get<{ sources: Source[] }>('/api/v1/sources').then(r => r.sources ?? []);
}

export function fetchSource(id: string): Promise<Source> {
  return apiClient.get<Source>(`/api/v1/sources/${encodeURIComponent(id)}`);
}

export function testSource(id: string): Promise<{ ok: boolean; message?: string }> {
  return apiClient.post<{ ok: boolean; message?: string }>(
    `/api/v1/sources/${encodeURIComponent(id)}/test`,
    {}
  );
}

export function deleteSource(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/sources/${encodeURIComponent(id)}`);
}
