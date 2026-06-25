import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { Button } from '@/components/ui/button';
import { PoliciesTable } from '@/components/admin/PoliciesTable';
import { PolicyForm } from '@/components/admin/PolicyForm';
import { useZones } from '@/hooks/useZones';
import { usePolicies } from '@/hooks/usePolicies';
import { usePrincipals } from '@/hooks/usePrincipals';
import { createPolicy } from '@/api/security';
import { ShieldCheck } from 'lucide-react';

// PoliciesAdminPage is the former Admin "Policies" tab promoted to a standalone
// admin-only route under the Admin nav subgroup (session admin-nav-consolidation).
// Server-side gating is unchanged; the route renders behind <RequireAdmin>.
export function PoliciesAdminPage() {
  const qc = useQueryClient();
  const [showCreatePolicy, setShowCreatePolicy] = useState(false);

  const zonesQ = useZones();
  const policiesQ = usePolicies();
  const principalsQ = usePrincipals();

  const createPolicyMut = useMutation({
    mutationFn: ({ principal, zone_id }: { principal: string; zone_id: string }) =>
      createPolicy({ principal, zone_id }),
    onSuccess: () => {
      toast.success('Policy created');
      setShowCreatePolicy(false);
      void qc.invalidateQueries({ queryKey: ['policies'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (policiesQ.isLoading) return <LoadingPage />;

  const zones = zonesQ.data ?? [];
  const policies = policiesQ.data ?? [];
  const principals = principalsQ.data ?? [];

  return (
    <>
      <Header title="Policies" />
      <PageContainer>
        <div className="mb-3 flex justify-end">
          <Button size="sm" onClick={() => setShowCreatePolicy(true)}>
            + Create Policy
          </Button>
        </div>
        {policies.length === 0 ? (
          <EmptyState
            icon={ShieldCheck}
            title="No policies"
            description="Create an RBAC policy to control access."
          />
        ) : (
          <PoliciesTable policies={policies} />
        )}
      </PageContainer>

      <PolicyForm
        open={showCreatePolicy}
        onOpenChange={setShowCreatePolicy}
        zones={zones}
        principals={principals}
        onSubmit={(principal, zoneId) => createPolicyMut.mutate({ principal, zone_id: zoneId })}
        isLoading={createPolicyMut.isPending}
      />
    </>
  );
}
