import { apiClient } from './client';
import { AuthConfigSchema } from './schemas';
import type { AuthConfig } from './types';

// GET /api/v1/auth/config — the public, pre-auth capability signal. Served
// under the /api/v1/auth/ prefix that the server's edge gate treats as public,
// so it resolves on the cold logged-out shell with no credential. Unlike /me,
// it never 401s, which is why the OIDC-button signal reads from here rather
// than from the authed /me response.
export function fetchAuthConfig(): Promise<AuthConfig> {
  return apiClient.get<unknown>('/api/v1/auth/config').then((r) => AuthConfigSchema.parse(r));
}
