import { apiClient } from './client';
import { ComponentSchema } from './schemas';
import { z } from 'zod';
import type { Component } from './types';

export function fetchComponents(): Promise<Component[]> {
  return apiClient
    .get<unknown>('/api/v1/components')
    .then((r) => z.object({ components: z.array(ComponentSchema) }).parse(r).components);
}

export function fetchComponent(id: string): Promise<Component> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}`)
    .then((r) => ComponentSchema.parse(r));
}

export function testComponent(id: string): Promise<{ ok: boolean; message?: string }> {
  return apiClient.post<{ ok: boolean; message?: string }>(
    `/api/v1/components/${encodeURIComponent(id)}/test`,
    {}
  );
}

export function deleteComponent(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/components/${encodeURIComponent(id)}`);
}
