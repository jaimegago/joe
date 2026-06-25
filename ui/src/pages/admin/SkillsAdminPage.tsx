import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { SkillsTable } from '@/components/admin/SkillsTable';
import { useSkills } from '@/hooks/useSkills';
import { ApiRequestError } from '@/api/client';
import { Puzzle } from 'lucide-react';

// SkillsAdminPage is the former Admin "Skills" tab promoted to a standalone
// admin-only route under the Admin nav subgroup (session admin-nav-consolidation).
// Server-side gating is unchanged; the route renders behind <RequireAdmin>.
export function SkillsAdminPage() {
  const skillsQ = useSkills();

  return (
    <>
      <Header title="Skills" />
      <PageContainer>
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
      </PageContainer>
    </>
  );
}
