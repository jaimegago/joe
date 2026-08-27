import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { QueryError } from '@/components/common/QueryError';
import { ReadPostureControl } from '@/components/admin/ReadPostureControl';
import { useReadPosture } from '@/hooks/useReadPosture';
import { usePolicies } from '@/hooks/usePolicies';

// ReadPostureAdminPage hosts the install-wide read-posture control — the admin
// affordance that flips team_flat <-> zoned (read-posture-latch, "v2 zoned-flip
// UI"). It is a standalone admin-only route under the Admin nav subgroup, the
// shape every admin-only surface with no operator view takes.
//
// It reads the grant count beside the posture because that count is the
// consequence of switching to `zoned`: the zoned decision is grant-based for
// every non-admin principal, so an install with no grants silently stops
// non-admin reads. Only the posture query gates the page — a failed policies
// read leaves grantCount undefined and the control says so, rather than
// withholding a governance control over a secondary fetch.
export function ReadPostureAdminPage() {
  const postureQ = useReadPosture();
  const policiesQ = usePolicies();

  return (
    <>
      <Header title="Read Posture" />
      <PageContainer>
        {postureQ.isLoading ? (
          <LoadingPage />
        ) : postureQ.isError || !postureQ.data ? (
          <QueryError
            error={postureQ.error}
            onRetry={() => void postureQ.refetch()}
            resourceLabel="read posture"
          />
        ) : (
          <ReadPostureControl
            posture={postureQ.data}
            grantCount={policiesQ.isSuccess ? policiesQ.data.length : undefined}
          />
        )}
      </PageContainer>
    </>
  );
}
