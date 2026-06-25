import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { Button } from '@/components/ui/button';
import { AdminsTable } from '@/components/admin/AdminsTable';
import { AdminForm } from '@/components/admin/AdminForm';
import { useAdmins } from '@/hooks/usePrincipals';
import { addAdmin } from '@/api/security';
import { ShieldCheck } from 'lucide-react';

// AdminsAdminPage is the former Admin "Admins" tab promoted to a standalone
// admin-only route under the Admin nav subgroup (session admin-nav-consolidation).
// Server-side gating is unchanged; the route renders behind <RequireAdmin>.
export function AdminsAdminPage() {
  const qc = useQueryClient();
  const [showAddAdmin, setShowAddAdmin] = useState(false);

  const adminsQ = useAdmins();

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

  if (adminsQ.isLoading) return <LoadingPage />;

  const admins = adminsQ.data ?? [];

  return (
    <>
      <Header title="Admins" />
      <PageContainer>
        <div className="mb-3 flex justify-end">
          <Button size="sm" onClick={() => setShowAddAdmin(true)}>
            + Add Admin
          </Button>
        </div>
        {admins.length === 0 ? (
          <EmptyState
            icon={ShieldCheck}
            title="No admins"
            description="Add an admin to manage Joe."
          />
        ) : (
          <AdminsTable admins={admins} />
        )}
      </PageContainer>

      <AdminForm
        open={showAddAdmin}
        onOpenChange={setShowAddAdmin}
        onSubmit={(principal, reason) => addAdminMut.mutate({ principal, reason })}
        isLoading={addAdminMut.isPending}
      />
    </>
  );
}
