import { apiClient } from './client';
import {
  AutoRunListSchema,
  AutoRunSchema,
  AutoScheduleListSchema,
  AutoScheduleSchema,
} from './schemas';
import type { AutoRun, AutoRunSelection, AutoSchedule } from './types';

// The Auto Joe run surface (internal/api/autorun.go): reads open to any
// authenticated principal, writes admin-gated — exactly as the API is. The
// page offers the writes to admins and nothing else; a refusal is the API's
// (a 409 on a live scope, a 400 on an empty or unknown scope), surfaced at the
// point of the act with the reason the API gave.

export function fetchAutoRuns(): Promise<AutoRun[]> {
  return apiClient
    .get<unknown>('/api/v1/auto/runs')
    .then((r) => AutoRunListSchema.parse(r).runs ?? []);
}

export function fetchAutoRun(id: string): Promise<AutoRun> {
  return apiClient
    .get<unknown>(`/api/v1/auto/runs/${encodeURIComponent(id)}`)
    .then((r) => AutoRunSchema.parse(r));
}

export function fetchAutoSchedules(): Promise<AutoSchedule[]> {
  return apiClient
    .get<unknown>('/api/v1/auto/schedules')
    .then((r) => AutoScheduleListSchema.parse(r).schedules ?? []);
}

// triggerAutoRun starts one run over the scope, budgeted from the deployment
// defaults, as svc:agent:core with the caller as activator. A scope and
// nothing else: no principal, no budget, no mode.
export function triggerAutoRun(scope: AutoRunSelection): Promise<AutoRun> {
  return apiClient
    .post<unknown>('/api/v1/auto/runs', { scope })
    .then((r) => AutoRunSchema.parse(r));
}

// installAutoSchedule installs a standing trigger: a scope and a cadence in
// minutes, what the install route takes. Budget comes from deployment
// defaults; there is no budget field.
export function installAutoSchedule(
  scope: AutoRunSelection,
  intervalMinutes: number
): Promise<AutoSchedule> {
  return apiClient
    .post<unknown>('/api/v1/auto/schedules', { scope, interval_minutes: intervalMinutes })
    .then((r) => AutoScheduleSchema.parse(r));
}

export function removeAutoSchedule(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/auto/schedules/${encodeURIComponent(id)}`);
}
