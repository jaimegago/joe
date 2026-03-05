import { apiClient } from './client';
import { AlertSchema } from './schemas';
import { z } from 'zod';
import type { Alert } from './types';

export function fetchAlerts(): Promise<Alert[]> {
  return apiClient
    .get<unknown>('/api/v1/alerts')
    .then((r) => z.object({ alerts: z.array(AlertSchema) }).parse(r).alerts);
}
