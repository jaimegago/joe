import { apiClient } from './client';
import type { SecurityZone, SourceZoneAssignment, RbacPolicy } from './types';

export function fetchZones(): Promise<SecurityZone[]> {
  return apiClient.get<{ zones: SecurityZone[] }>('/api/v1/admin/zones').then(r => r.zones ?? []);
}

export function createZone(zone: { id: string; name: string; description: string; allowed_actions: string[] }): Promise<SecurityZone> {
  return apiClient.post<SecurityZone>('/api/v1/admin/zones', zone);
}

export function updateZone(_id: string, _zone: Partial<SecurityZone>): Promise<SecurityZone> {
  return Promise.reject(new Error('Update zone not supported'));
}

export function deleteZone(_id: string): Promise<void> {
  return Promise.reject(new Error('Delete zone not supported'));
}

export function fetchSourceZones(): Promise<SourceZoneAssignment[]> {
  return apiClient.get<{ assignments: SourceZoneAssignment[] }>('/api/v1/admin/source-zones').then(r => r.assignments ?? []);
}

export function fetchUnassigned(): Promise<{ source_id: string }[]> {
  return apiClient.get<{ source_ids: string[] }>('/api/v1/admin/unassigned').then(r =>
    (r.source_ids ?? []).map(id => ({ source_id: id }))
  );
}

export function assignZone(sourceId: string, zoneId: string, reason?: string): Promise<SourceZoneAssignment> {
  return apiClient.post<SourceZoneAssignment>('/api/v1/admin/source-zones', {
    source_id: sourceId,
    zone_id: zoneId,
    assigned_by: 'web-ui',
    reason: reason ?? '',
  });
}

export function removeZone(_sourceId: string): Promise<void> {
  return Promise.reject(new Error('Remove zone assignment not supported'));
}

export function fetchPolicies(): Promise<RbacPolicy[]> {
  return apiClient.get<{ policies: RbacPolicy[] }>('/api/v1/admin/policies').then(r => r.policies ?? []);
}

export function createPolicy(policy: Pick<RbacPolicy, 'principal' | 'zone_id'>): Promise<RbacPolicy> {
  return apiClient.post<RbacPolicy>('/api/v1/admin/policies', {
    principal: policy.principal,
    zone_id: policy.zone_id,
  });
}

export function deletePolicy(id: number): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/policies/${id}`);
}
