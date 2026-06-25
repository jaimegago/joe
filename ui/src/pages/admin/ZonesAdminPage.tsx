import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { Button } from '@/components/ui/button';
import { ZonesTable } from '@/components/admin/ZonesTable';
import { ZoneForm } from '@/components/admin/ZoneForm';
import { UnassignedComponents } from '@/components/admin/UnassignedComponents';
import { useZones, useUnassigned } from '@/hooks/useZones';
import { createZone } from '@/api/security';
import { ShieldCheck } from 'lucide-react';

// ZonesAdminPage is the former Admin "Zones" tab promoted to a standalone
// admin-only route under the Admin nav subgroup (session admin-nav-consolidation).
// It owns only the zones surface — the list, the create-zone form, and the
// unassigned-components assignment control. Server-side gating is unchanged; the
// route renders behind <RequireAdmin>.
export function ZonesAdminPage() {
  const qc = useQueryClient();
  const [showCreateZone, setShowCreateZone] = useState(false);

  const zonesQ = useZones();
  const unassignedQ = useUnassigned();

  const createZoneMut = useMutation({
    mutationFn: (data: {
      id: string;
      name: string;
      description: string;
      allowed_actions: string[];
    }) => createZone(data),
    onSuccess: () => {
      toast.success('Zone created');
      setShowCreateZone(false);
      void qc.invalidateQueries({ queryKey: ['zones'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (zonesQ.isLoading) return <LoadingPage />;

  const zones = zonesQ.data ?? [];
  const unassigned = unassignedQ.data ?? [];

  return (
    <>
      <Header title="Zones" />
      <PageContainer>
        <div className="mb-3 flex justify-end">
          <Button size="sm" onClick={() => setShowCreateZone(true)}>
            + Create Zone
          </Button>
        </div>
        <UnassignedComponents unassigned={unassigned} zones={zones} />
        {zones.length === 0 ? (
          <EmptyState
            icon={ShieldCheck}
            title="No zones"
            description="Create a security zone to get started."
          />
        ) : (
          <ZonesTable zones={zones} />
        )}
      </PageContainer>

      <ZoneForm
        open={showCreateZone}
        onOpenChange={setShowCreateZone}
        onSubmit={(data) => createZoneMut.mutate(data)}
        isLoading={createZoneMut.isPending}
      />
    </>
  );
}
