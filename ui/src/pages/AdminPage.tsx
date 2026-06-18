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
import { UnassignedComponents } from '@/components/admin/UnassignedComponents';
import { ComponentZoneAssign } from '@/components/admin/ComponentZoneAssign';
import { PoliciesTable } from '@/components/admin/PoliciesTable';
import { PolicyForm } from '@/components/admin/PolicyForm';
import { AdminsTable } from '@/components/admin/AdminsTable';
import { AdminForm } from '@/components/admin/AdminForm';
import { ComponentRegisterForm } from '@/components/admin/ComponentRegisterForm';
import { ReadPromotionsTable } from '@/components/admin/ReadPromotionsTable';
import { SkillsTable } from '@/components/admin/SkillsTable';
import { useZones, useUnassigned } from '@/hooks/useZones';
import { useReadPromotions } from '@/hooks/useReadPromotions';
import { useSkills } from '@/hooks/useSkills';
import { usePolicies } from '@/hooks/usePolicies';
import { usePrincipals, useAdmins } from '@/hooks/usePrincipals';
import { createZone, createPolicy, addAdmin } from '@/api/security';
import { createComponent } from '@/api/components';
import { ApiRequestError } from '@/api/client';
import { ShieldCheck, Puzzle } from 'lucide-react';

export function AdminPage() {
  const qc = useQueryClient();
  const [showCreateZone, setShowCreateZone] = useState(false);
  const [showCreatePolicy, setShowCreatePolicy] = useState(false);
  const [showAddAdmin, setShowAddAdmin] = useState(false);
  const [showRegisterComponent, setShowRegisterComponent] = useState(false);

  const zonesQ = useZones();
  const unassignedQ = useUnassigned();
  const policiesQ = usePolicies();
  const readPromotionsQ = useReadPromotions();
  const skillsQ = useSkills();
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

  const registerComponentMut = useMutation({
    mutationFn: (data: { id: string; type: string; name: string }) => createComponent(data),
    onSuccess: (comp) => {
      // The component is registered INERT — credential-less, in the unassigned
      // zone, under the read-only floor. Point the operator at the next
      // governance steps rather than implying it is ready to use.
      toast.success(
        `Component "${comp.id}" registered (inert). Assign it a zone (below) and promote it to supply credentials before it can act.`
      );
      setShowRegisterComponent(false);
      // A new registration lands unassigned; refresh the dependent lists so it
      // surfaces in the unassigned pool and the component views.
      void qc.invalidateQueries({ queryKey: ['components'] });
      void qc.invalidateQueries({ queryKey: ['unassigned'] });
      void qc.invalidateQueries({ queryKey: ['component-zones'] });
    },
    onError: (e: Error) => {
      // Duplicate id is a 409 from the governed create endpoint. The
      // credential-rejection 400 is defended against here even though this form
      // sends no config and so cannot trigger it.
      if (e instanceof ApiRequestError && e.status === 409) {
        toast.error('A component with that ID already exists. Choose a different ID.');
        return;
      }
      if (e instanceof ApiRequestError && e.status === 400) {
        toast.error(`Registration rejected: ${e.message}`);
        return;
      }
      toast.error(e.message);
    },
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
            <TabsTrigger value="components">Components</TabsTrigger>
            <TabsTrigger value="policies">Policies</TabsTrigger>
            <TabsTrigger value="autonomous-reads">Autonomous Reads</TabsTrigger>
            <TabsTrigger value="skills">Skills</TabsTrigger>
            <TabsTrigger value="admins">Admins</TabsTrigger>
          </TabsList>

          <TabsContent value="zones">
            <div className="mb-3 flex justify-end">
              <Button size="sm" onClick={() => setShowCreateZone(true)}>+ Create Zone</Button>
            </div>
            <UnassignedComponents unassigned={unassigned} zones={zones} />
            {zones.length === 0 ? (
              <EmptyState icon={ShieldCheck} title="No zones" description="Create a security zone to get started." />
            ) : (
              <ZonesTable zones={zones} />
            )}
          </TabsContent>

          <TabsContent value="components">
            <div className="mb-3 flex justify-end">
              <Button size="sm" onClick={() => setShowRegisterComponent(true)}>
                + Register Component
              </Button>
            </div>
            <ComponentZoneAssign />
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

          <TabsContent value="autonomous-reads">
            {readPromotionsQ.isLoading ? (
              <LoadingPage />
            ) : (
              <ReadPromotionsTable promotions={readPromotionsQ.data ?? []} />
            )}
          </TabsContent>

          <TabsContent value="skills">
            {skillsQ.isLoading ? (
              <LoadingPage />
            ) : skillsQ.isError ? (
              skillsQ.error instanceof ApiRequestError && skillsQ.error.status === 503 ? (
                <EmptyState
                  icon={Puzzle}
                  title="Skills unavailable"
                  description="The skills manager is not enabled on this joe instance."
                />
              ) : (
                <EmptyState
                  icon={Puzzle}
                  title="Couldn't load skills"
                  description="The request failed. This may be a transient error — try again."
                  action={{ label: 'Retry', onClick: () => void skillsQ.refetch() }}
                />
              )
            ) : (
              <SkillsTable data={skillsQ.data ?? { active: [], quarantined: [] }} />
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

      <ComponentRegisterForm
        open={showRegisterComponent}
        onOpenChange={setShowRegisterComponent}
        onSubmit={(data) => registerComponentMut.mutate(data)}
        isLoading={registerComponentMut.isPending}
      />
    </>
  );
}
