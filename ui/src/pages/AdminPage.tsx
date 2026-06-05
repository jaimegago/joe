import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { ZonesTable } from '@/components/admin/ZonesTable';
import { ZoneForm } from '@/components/admin/ZoneForm';
import { UnassignedSources } from '@/components/admin/UnassignedSources';
import { SourceZoneAssign } from '@/components/admin/SourceZoneAssign';
import { PoliciesTable } from '@/components/admin/PoliciesTable';
import { PolicyForm } from '@/components/admin/PolicyForm';
import { AdminsTable } from '@/components/admin/AdminsTable';
import { AdminForm } from '@/components/admin/AdminForm';
import { useZones, useUnassigned } from '@/hooks/useZones';
import { usePolicies } from '@/hooks/usePolicies';
import { usePrincipals, useAdmins } from '@/hooks/usePrincipals';
import { createZone, createPolicy, addAdmin } from '@/api/security';
import { ShieldCheck } from 'lucide-react';

export function AdminPage() {
  const qc = useQueryClient();
  const [showCreateZone, setShowCreateZone] = useState(false);
  const [showCreatePolicy, setShowCreatePolicy] = useState(false);
  const [showAddAdmin, setShowAddAdmin] = useState(false);

  const zonesQ = useZones();
  const unassignedQ = useUnassigned();
  const policiesQ = usePolicies();
  const principalsQ = usePrincipals();
  const adminsQ = useAdmins();

  const createZoneMut = useMutation({
    mutationFn: (data: { id: string; name: string; description: string; allowed_actions: string[] }) =>
      createZone(data),
    onSuccess: () => {
      toast.success('Zone created');
      setShowCreateZone(false);
      void qc.invalidateQueries({ queryKey: ['zones'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

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

  const addAdminMut = useMutation({
    mutationFn: ({ principal, reason }: { principal: string; reason: string }) =>
      addAdmin(principal, reason),
    onSuccess: () => {
      toast.success('Admin added');
      setShowAddAdmin(false);
      void qc.invalidateQueries({ queryKey: ['admins'] });
      // Promotion strips the principal's redundant per-zone grants server-side.
      void qc.invalidateQueries({ queryKey: ['policies'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  const isLoading = zonesQ.isLoading || policiesQ.isLoading;
  if (isLoading) return <LoadingPage />;

  const zones = zonesQ.data ?? [];
  const policies = policiesQ.data ?? [];
  const unassigned = unassignedQ.data ?? [];
  const principals = principalsQ.data ?? [];
  const admins = adminsQ.data ?? [];

  return (
    <>
      <Header title="Admin" />
      <PageContainer>
        <Tabs defaultValue="zones">
          <TabsList className="mb-4">
            <TabsTrigger value="zones">Zones</TabsTrigger>
            <TabsTrigger value="sources">Sources</TabsTrigger>
            <TabsTrigger value="policies">Policies</TabsTrigger>
            <TabsTrigger value="admins">Admins</TabsTrigger>
          </TabsList>

          <TabsContent value="zones">
            <div className="mb-3 flex justify-end">
              <Button size="sm" onClick={() => setShowCreateZone(true)}>+ Create Zone</Button>
            </div>
            <UnassignedSources unassigned={unassigned} zones={zones} />
            {zones.length === 0 ? (
              <EmptyState icon={ShieldCheck} title="No zones" description="Create a security zone to get started." />
            ) : (
              <ZonesTable zones={zones} />
            )}
          </TabsContent>

          <TabsContent value="sources">
            <SourceZoneAssign />
          </TabsContent>

          <TabsContent value="policies">
            <div className="mb-3 flex justify-end">
              <Button size="sm" onClick={() => setShowCreatePolicy(true)}>+ Create Policy</Button>
            </div>
            {policies.length === 0 ? (
              <EmptyState icon={ShieldCheck} title="No policies" description="Create an RBAC policy to control access." />
            ) : (
              <PoliciesTable policies={policies} />
            )}
          </TabsContent>

          <TabsContent value="admins">
            <div className="mb-3 flex justify-end">
              <Button size="sm" onClick={() => setShowAddAdmin(true)}>+ Add Admin</Button>
            </div>
            {admins.length === 0 ? (
              <EmptyState icon={ShieldCheck} title="No admins" description="Add an admin to manage Joe." />
            ) : (
              <AdminsTable admins={admins} />
            )}
          </TabsContent>
        </Tabs>
      </PageContainer>

      <ZoneForm
        open={showCreateZone}
        onOpenChange={setShowCreateZone}
        onSubmit={(data) => createZoneMut.mutate(data)}
        isLoading={createZoneMut.isPending}
      />

      <PolicyForm
        open={showCreatePolicy}
        onOpenChange={setShowCreatePolicy}
        zones={zones}
        principals={principals}
        onSubmit={(principal, zoneId) => createPolicyMut.mutate({ principal, zone_id: zoneId })}
        isLoading={createPolicyMut.isPending}
      />

      <AdminForm
        open={showAddAdmin}
        onOpenChange={setShowAddAdmin}
        onSubmit={(principal, reason) => addAdminMut.mutate({ principal, reason })}
        isLoading={addAdminMut.isPending}
      />
    </>
  );
}
