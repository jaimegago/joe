import { apiClient } from './client';
import { GraphSchema } from './schemas';
import type { Graph } from './types';

export function fetchGraph(): Promise<Graph> {
  return apiClient.get<unknown>('/api/v1/graph').then((r) => GraphSchema.parse(r));
}
