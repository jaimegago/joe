import { apiClient } from './client';
import { SecurityZoneSchema, SourceZoneAssignmentSchema, RbacPolicySchema } from './schemas';
import { z } from 'zod';
import type { SecurityZone, SourceZoneAssignment, RbacPolicy } from './types';

export function fetchZones(): Promise<SecurityZone[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/zones')
    .then((r) => z.object({ zones: z.array(SecurityZoneSchema) }).parse(r).zones);
}

export function createZone(zone: {
  id: string;
  name: string;
  description: string;
  allowed_actions: string[];
}): Promise<SecurityZone> {
  return apiClient
    .post<unknown>('/api/v1/admin/zones', zone)
    .then((r) => SecurityZoneSchema.parse(r));
}

export function updateZone(_id: string, _zone: Partial<SecurityZone>): Promise<SecurityZone> {
  return Promise.reject(new Error('Update zone not supported'));
}

export function deleteZone(_id: string): Promise<void> {
  return Promise.reject(new Error('Delete zone not supported'));
}

export function fetchSourceZones(): Promise<SourceZoneAssignment[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/source-zones')
    .then(
      (r) =>
        z.object({ assignments: z.array(SourceZoneAssignmentSchema) }).parse(r).assignments
    );
}

export function fetchUnassigned(): Promise<{ source_id: string }[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/unassigned')
    .then((r) =>
      z
        .object({ source_ids: z.array(z.string()) })
        .parse(r)
        .source_ids.map((id) => ({ source_id: id }))
    );
}

export function assignZone(
  sourceId: string,
  zoneId: string,
  reason?: string
): Promise<SourceZoneAssignment> {
  return apiClient
    .post<unknown>('/api/v1/admin/source-zones', {
      source_id: sourceId,
      zone_id: zoneId,
      assigned_by: 'web-ui',
      reason: reason ?? '',
    })
    .then((r) => SourceZoneAssignmentSchema.parse(r));
}

export function removeZone(_sourceId: string): Promise<void> {
  return Promise.reject(new Error('Remove zone assignment not supported'));
}

export function fetchPolicies(): Promise<RbacPolicy[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/policies')
    .then((r) => z.object({ policies: z.array(RbacPolicySchema) }).parse(r).policies);
}

export function createPolicy(policy: Pick<RbacPolicy, 'principal' | 'zone_id'>): Promise<RbacPolicy> {
  return apiClient
    .post<unknown>('/api/v1/admin/policies', {
      principal: policy.principal,
      zone_id: policy.zone_id,
    })
    .then((r) => RbacPolicySchema.parse(r));
}

export function deletePolicy(id: number): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/policies/${id}`);
}
