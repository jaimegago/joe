import { useMemo } from 'react';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { PrincipalsTable, type PrincipalRow } from '@/components/admin/PrincipalsTable';
import { usePrincipals, useAdmins } from '@/hooks/usePrincipals';
import { usePolicies } from '@/hooks/usePolicies';
import { useAuth } from '@/auth/AuthContext';
import { ApiRequestError } from '@/api/client';
import { Users } from 'lucide-react';

// UsersPage is the admin operator view of identity. It lists provisioned
// principals (GET /admin/principals) and composes two columns client-side —
// per-principal granted zones from the policies list and admin status from the
// admin roster — rather than relying on a combined backend endpoint.
export function UsersPage() {
  const principalsQ = usePrincipals();
  const policiesQ = usePolicies();
  const adminsQ = useAdmins();
  const { principal: selfPrincipal } = useAuth();

  // Compose zone-grants and admin-status per principal. O(principals + policies
  // + admins) with two lookup maps — cheap for the registry sizes this serves.
  // (A combined users endpoint would avoid the three round-trips; flagged as a
  // future backend convenience, not built in this stage.)
  const rows: PrincipalRow[] = useMemo(() => {
    const principals = principalsQ.data ?? [];
    const policies = policiesQ.data ?? [];
    const admins = adminsQ.data ?? [];

    const zonesByPrincipal = new Map<string, string[]>();
    for (const p of policies) {
      const list = zonesByPrincipal.get(p.principal) ?? [];
      list.push(p.zone_id);
      zonesByPrincipal.set(p.principal, list);
    }
    const adminSet = new Set(admins.map((a) => a.principal));

    return principals.map((record) => ({
      record,
      zones: zonesByPrincipal.get(record.principal) ?? [],
      isAdmin: adminSet.has(record.principal),
    }));
  }, [principalsQ.data, policiesQ.data, adminsQ.data]);

  if (principalsQ.isLoading || policiesQ.isLoading || adminsQ.isLoading) return <LoadingPage />;

  // Only a 503 means the registry is genuinely unconfigured (RBAC/OIDC off);
  // server-side the handler returns 503 when the principal repository is
  // unwired. Any other failure (500, network, a transient DB lock) is reported
  // as a retriable error rather than misattributed to misconfiguration.
  const notConfigured =
    principalsQ.error instanceof ApiRequestError && principalsQ.error.status === 503;

  return (
    <>
      <Header title="Users" />
      <PageContainer>
        {principalsQ.isError ? (
          notConfigured ? (
            <EmptyState
              icon={Users}
              title="Identity registry unavailable"
              description="The principals registry could not be loaded. It requires RBAC and OIDC to be configured."
            />
          ) : (
            <EmptyState
              icon={Users}
              title="Couldn't load users"
              description="The identity registry request failed. This may be a transient error — try again."
              action={{ label: 'Retry', onClick: () => void principalsQ.refetch() }}
            />
          )
        ) : rows.length === 0 ? (
          <EmptyState
            icon={Users}
            title="No users yet"
            description="Principals appear here after their first sign-in."
          />
        ) : (
          <PrincipalsTable rows={rows} selfPrincipal={selfPrincipal} />
        )}
      </PageContainer>
    </>
  );
}
