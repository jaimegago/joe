import { apiClient } from './client';
import type { Alert } from './types';

export function fetchAlerts(): Promise<Alert[]> {
  return apiClient.get<{ alerts: Alert[] }>('/api/v1/alerts').then(r => r.alerts ?? []);
}
