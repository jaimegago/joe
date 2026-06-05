import { apiClient } from './client';
import {
  SecurityZoneSchema,
  SourceZoneAssignmentSchema,
  RbacPolicySchema,
  PrincipalRecordSchema,
  AdminSchema,
} from './schemas';
import { z } from 'zod';
import type {
  SecurityZone,
  SourceZoneAssignment,
  RbacPolicy,
  PrincipalRecord,
  Admin,
} from './types';

// All admin mutations reach apiClient, which throws ApiRequestError (carrying
// the HTTP `status` and the server's `message`) on any non-2xx. Callers branch
// on `error.status === 409` to surface the actionable conflict reason
// (`error.message`) for the zone-in-use, last-admin, bootstrap-admin, and
// self-disable cases; no bespoke error type is needed here.

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

// updateZone applies a partial update (any subset of name, description,
// allowed_actions) to the zone — PATCH /api/v1/admin/zones/{id}. Omitted fields
// are left untouched server-side.
export function updateZone(
  id: string,
  patch: { name?: string; description?: string; allowed_actions?: string[] }
): Promise<SecurityZone> {
  return apiClient
    .patch<unknown>(`/api/v1/admin/zones/${encodeURIComponent(id)}`, patch)
    .then((r) => SecurityZoneSchema.parse(r));
}

// deleteZone removes a zone — DELETE /api/v1/admin/zones/{id}. Returns 409
// (ApiRequestError.status) when the zone still has source assignments; the
// caller surfaces that as an actionable reassign-first message.
export function deleteZone(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/zones/${encodeURIComponent(id)}`);
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

// removeZone unassigns a source from its zone — DELETE
// /api/v1/admin/source-zones/{sourceID}. The source then falls back to the
// policy engine's default unassigned behaviour.
export function removeZone(sourceId: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/source-zones/${encodeURIComponent(sourceId)}`);
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

// revokePolicy revokes a single principal→zone grant by its natural key —
// POST /api/v1/admin/policies/revoke. Lets a caller revoke without first
// resolving the synthetic policy id.
export function revokePolicy(principal: string, zoneId: string): Promise<void> {
  return apiClient.post<void>('/api/v1/admin/policies/revoke', {
    principal,
    zone_id: zoneId,
  });
}

// --- Identity registry (Users page) ---

export function fetchPrincipals(): Promise<PrincipalRecord[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/principals')
    .then((r) => z.object({ principals: z.array(PrincipalRecordSchema) }).parse(r).principals);
}

// disablePrincipal disables a principal — POST
// /api/v1/admin/principals/{principal}/disable. Server-side this also revokes
// the principal's live sessions. Returns 409 on a self-disable attempt.
export function disablePrincipal(principal: string): Promise<void> {
  return apiClient.post<void>(
    `/api/v1/admin/principals/${encodeURIComponent(principal)}/disable`,
    {}
  );
}

// enablePrincipal re-enables a disabled principal — POST
// /api/v1/admin/principals/{principal}/enable. No sessions are resurrected.
export function enablePrincipal(principal: string): Promise<void> {
  return apiClient.post<void>(
    `/api/v1/admin/principals/${encodeURIComponent(principal)}/enable`,
    {}
  );
}

// --- Admin roster ---

export function fetchAdmins(): Promise<Admin[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/admins')
    .then((r) => z.object({ admins: z.array(AdminSchema) }).parse(r).admins);
}

// addAdmin promotes a principal to admin — POST /api/v1/admin/admins. The
// server validates the reserved prefix (400) and wraps GrantAdmin.
export function addAdmin(principal: string, reason?: string): Promise<void> {
  return apiClient.post<void>('/api/v1/admin/admins', {
    principal,
    reason: reason ?? '',
  });
}

// removeAdmin demotes an admin — DELETE /api/v1/admin/admins/{principal}.
// Returns 409 for the configured bootstrap admin (auth.admin_email must change
// first) and for the last remaining admin.
export function removeAdmin(principal: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/admins/${encodeURIComponent(principal)}`);
}
