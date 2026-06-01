import { apiClient } from './client';
import { CurrentUserSchema } from './schemas';
import type { CurrentUser } from './types';

// GET /api/v1/me — who am I, am I an admin, and is RBAC enforcement
// active. The endpoint returns is_admin=true in auth-disabled mode, so a
// consumer keying on is_admin alone renders admin surfaces correctly in
// local mode without a second predicate.
export function fetchCurrentUser(): Promise<CurrentUser> {
  return apiClient.get<unknown>('/api/v1/me').then((r) => CurrentUserSchema.parse(r));
}
