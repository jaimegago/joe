import { apiClient } from './client';
import {
  SkillsListResponseSchema,
  SkillsReloadResponseSchema,
  SkillsApprovalResponseSchema,
} from './schemas';
import type {
  SkillsListResponse,
  SkillsReloadResponse,
  SkillsApprovalResponse,
} from './types';

// Skills inspection/management API. These endpoints are bearer-authed (not
// RBAC-gated server-side), but every caller renders behind the admin-only
// Admin page, matching the other operator surfaces. No skill body is fetched
// here — only identity, git source, and status.

// fetchSkills lists every installed skill, split into the loaded (active) set
// and the on-disk-but-held (quarantined) set — GET /api/v1/skills.
export function fetchSkills(): Promise<SkillsListResponse> {
  return apiClient
    .get<unknown>('/api/v1/skills')
    .then((r) => SkillsListResponseSchema.parse(r));
}

// reloadSkills triggers a synchronous rescan of the skills directory and
// returns the before/after counts plus the diff — POST /api/v1/skills/reload.
// 503 when hot reload is disabled on this instance.
export function reloadSkills(): Promise<SkillsReloadResponse> {
  return apiClient
    .post<unknown>('/api/v1/skills/reload', {})
    .then((r) => SkillsReloadResponseSchema.parse(r));
}

// approveSkill moves a quarantined skill into the active tree, where the next
// reload loads it into the router — POST /api/v1/skills/approve.
export function approveSkill(name: string): Promise<SkillsApprovalResponse> {
  return apiClient
    .post<unknown>('/api/v1/skills/approve', { name })
    .then((r) => SkillsApprovalResponseSchema.parse(r));
}

// rejectSkill deletes a quarantined skill from disk — POST /api/v1/skills/reject.
export function rejectSkill(name: string): Promise<SkillsApprovalResponse> {
  return apiClient
    .post<unknown>('/api/v1/skills/reject', { name })
    .then((r) => SkillsApprovalResponseSchema.parse(r));
}
