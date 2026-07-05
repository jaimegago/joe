import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { QueryError } from '@/components/common/QueryError';
import { ReadPromotionsTable } from '@/components/admin/ReadPromotionsTable';
import { useReadPromotions } from '@/hooks/useReadPromotions';

// AutonomousReadsAdminPage is the former Admin "Autonomous Reads" tab promoted to
// a standalone admin-only route under the Admin nav subgroup (session
// admin-nav-consolidation). Server-side gating is unchanged; the route renders
// behind <RequireAdmin>.
export function AutonomousReadsAdminPage() {
  const readPromotionsQ = useReadPromotions();

  return (
    <>
      <Header title="Autonomous Reads" />
      <PageContainer>
        {readPromotionsQ.isLoading ? (
          <LoadingPage />
        ) : readPromotionsQ.isError ? (
          <QueryError
            error={readPromotionsQ.error}
            onRetry={() => void readPromotionsQ.refetch()}
            resourceLabel="autonomous-read settings"
          />
        ) : (
          <ReadPromotionsTable promotions={readPromotionsQ.data ?? []} />
        )}
      </PageContainer>
    </>
  );
}
