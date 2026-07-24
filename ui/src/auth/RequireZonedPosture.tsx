import type { ReactNode } from 'react';
import { Navigate } from 'react-router';
import { useReadPosture } from '@/hooks/useReadPosture';
import { READ_POSTURE } from '@/api/security';
import { LoadingPage } from '@/components/common/LoadingSpinner';

// RequireZonedPosture gates a route on the install read posture being `zoned`.
// Under the `team_flat` launch posture the grant-based Policies surface is inert
// — the boot-resolved write floor denies every Mutate below RBAC, and the
// team_flat admit widens read access to every authenticated principal ahead of
// the read-grant logic — so the page is hidden. This is a CLIENT-SIDE visibility
// gate only: the backing /api/v1/admin/policies REST endpoints stay registered
// and admin-gated so an operator can still manage grants over REST in either
// posture. Zones and component-zone assignment are deliberately NOT gated: the
// zone.Allows action check runs ahead of the team_flat admit, so those surfaces
// retain live read-shaping function under team_flat (see read-posture-latch).
//
// Nested inside <RequireAdmin>, so this only mounts for admins — the admin-gated
// posture fetch never fires for a non-admin. While the posture query is
// resolving we render the loading page rather than redirect, so a zoned-install
// admin hard-reloading /admin/policies is not bounced to the index route.
export function RequireZonedPosture({ children }: { children: ReactNode }) {
  const postureQ = useReadPosture();
  if (postureQ.isLoading) return <LoadingPage />;
  if (postureQ.data !== READ_POSTURE.zoned) return <Navigate to="/" replace />;
  return <>{children}</>;
}
