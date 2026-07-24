import type { ReactNode } from 'react';
import { Navigate } from 'react-router';
import { useAuth } from '@/auth/AuthContext';

// RequireAdmin gates admin-only routes at the route level. A non-admin
// reaching /admin or /llm-settings by direct URL is redirected to the index
// route rather than rendering the page. This is the authoritative gate; the
// in-page is_admin checks remain as defense in depth. It only renders inside
// the authed shell, so auth state is already resolved here.
export function RequireAdmin({ children }: { children: ReactNode }) {
  const { isAdmin } = useAuth();
  if (!isAdmin) return <Navigate to="/" replace />;
  return <>{children}</>;
}
