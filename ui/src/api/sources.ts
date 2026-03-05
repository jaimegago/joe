import { apiClient } from './client';
import { SourceSchema } from './schemas';
import { z } from 'zod';
import type { Source } from './types';

export function fetchSources(): Promise<Source[]> {
  return apiClient
    .get<unknown>('/api/v1/sources')
    .then((r) => z.object({ sources: z.array(SourceSchema) }).parse(r).sources);
}

export function fetchSource(id: string): Promise<Source> {
  return apiClient
    .get<unknown>(`/api/v1/sources/${encodeURIComponent(id)}`)
    .then((r) => SourceSchema.parse(r));
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
