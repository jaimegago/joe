import { apiClient } from './client';
import {
  SecurityZoneSchema,
  ComponentZoneAssignmentSchema,
  RbacPolicySchema,
  PrincipalRecordSchema,
  AdminSchema,
  ReadPromotionSchema,
} from './schemas';
import { z } from 'zod';
import type {
  SecurityZone,
  ComponentZoneAssignment,
  RbacPolicy,
  PrincipalRecord,
  Admin,
  ReadPromotion,
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

export function fetchComponentZones(): Promise<ComponentZoneAssignment[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/component-zones')
    .then(
      (r) => z.object({ assignments: z.array(ComponentZoneAssignmentSchema) }).parse(r).assignments
    );
}

export function fetchUnassigned(): Promise<{ component_id: string }[]> {
  return apiClient.get<unknown>('/api/v1/admin/unassigned').then((r) =>
    z
      .object({ component_ids: z.array(z.string()) })
      .parse(r)
      .component_ids.map((id) => ({ component_id: id }))
  );
}

export function assignZone(
  componentId: string,
  zoneId: string,
  reason?: string
): Promise<ComponentZoneAssignment> {
  return apiClient
    .post<unknown>('/api/v1/admin/component-zones', {
      component_id: componentId,
      zone_id: zoneId,
      assigned_by: 'web-ui',
      reason: reason ?? '',
    })
    .then((r) => ComponentZoneAssignmentSchema.parse(r));
}

// removeZone unassigns a source from its zone — DELETE
// /api/v1/admin/component-zones/{sourceID}. The source then falls back to the
// policy engine's default unassigned behaviour.
export function removeZone(componentId: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/admin/component-zones/${encodeURIComponent(componentId)}`);
}

export function fetchPolicies(): Promise<RbacPolicy[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/policies')
    .then((r) => z.object({ policies: z.array(RbacPolicySchema) }).parse(r).policies);
}

export function createPolicy(
  policy: Pick<RbacPolicy, 'principal' | 'zone_id'>
): Promise<RbacPolicy> {
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

// --- Install read posture (read-posture-latch) ---

// The two install-wide read postures, mirroring internal/readposture (Go
// PostureTeamFlat / PostureZoned). `team_flat` (launch default) admits every
// authenticated principal to read every component; `zoned` is the grant-based
// full-mode read decision. These literals are the wire values of the `posture`
// field on GET /api/v1/admin/read-posture.
export const READ_POSTURE = {
  teamFlat: 'team_flat',
  zoned: 'zoned',
} as const;
export type ReadPosture = (typeof READ_POSTURE)[keyof typeof READ_POSTURE];

// fetchReadPosture reads the current install-wide read posture. Admin-gated
// server-side (GET /api/v1/admin/read-posture returns 403 to non-admins), so
// callers only fire it behind an admin gate. The response is {"posture": "..."}.
export function fetchReadPosture(): Promise<ReadPosture> {
  return apiClient
    .get<unknown>('/api/v1/admin/read-posture')
    .then(
      (r) =>
        z.object({ posture: z.enum([READ_POSTURE.teamFlat, READ_POSTURE.zoned]) }).parse(r).posture
    );
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

// --- Autonomous reads (per component-type read-admit toggle) ---

// fetchReadPromotions lists every component type with its current autonomous-read
// state — GET /api/v1/admin/read-promotions. The server returns the FULL component-
// type enum overlaid with stored on-rows, so a type with no backend row comes back
// enabled:false (off by default). The wrapping {read_promotions,count} is unwrapped
// to the array, matching the other list fns here.
export function fetchReadPromotions(): Promise<ReadPromotion[]> {
  return apiClient
    .get<unknown>('/api/v1/admin/read-promotions')
    .then(
      (r) => z.object({ read_promotions: z.array(ReadPromotionSchema) }).parse(r).read_promotions
    );
}

// setReadPromotion flips one component type's autonomous-read state — POST
// /api/v1/admin/read-promotions. The server validates the type against the
// authoritative enum (400 on unknown) and commits the flag and its audit row in a
// single transaction. Returns the echoed {component_type,enabled} it just set.
export function setReadPromotion(componentType: string, enabled: boolean): Promise<ReadPromotion> {
  return apiClient
    .post<unknown>('/api/v1/admin/read-promotions', {
      component_type: componentType,
      enabled,
    })
    .then((r) => ReadPromotionSchema.parse(r));
}
