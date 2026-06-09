import { Header } from '@/components/layout/Header';
import { PageContainer } from '@/components/layout/PageContainer';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { CredentialStatusTable } from '@/components/admin/CredentialStatusTable';
import { useCredentialStatus } from '@/hooks/useCredentialStatus';
import { ApiRequestError } from '@/api/client';
import { KeyRound } from 'lucide-react';

// CredentialStatusPage is the admin operator view of "is Joe's credential
// resolution to component X healthy?" (D-0026 threat T4 — "no Joe to troubleshoot
// Joe"). On load it shows the PASSIVE, config-derived descriptor for every
// component (what credential is configured) without contacting any backend. The
// live "does it actually work right now" check is a deliberate per-row probe; the
// captured exec-plugin output is revealed only on explicit request. No credential
// material is ever requested or rendered.
export function CredentialStatusPage() {
  const statusQ = useCredentialStatus();
  const entries = statusQ.data ?? [];

  if (statusQ.isLoading) return <LoadingPage />;

  // A 503 means the surface is genuinely unconfigured (RBAC off); any other
  // failure is retriable rather than misattributed to misconfiguration — the
  // same posture the Users page uses.
  const notConfigured = statusQ.error instanceof ApiRequestError && statusQ.error.status === 503;

  return (
    <>
      <Header title="Credentials" />
      <PageContainer>
        {statusQ.isError ? (
          notConfigured ? (
            <EmptyState
              icon={KeyRound}
              title="Credential status unavailable"
              description="The credential status surface requires RBAC to be configured."
            />
          ) : (
            <EmptyState
              icon={KeyRound}
              title="Couldn't load credential status"
              description="The request failed. This may be a transient error — try again."
              action={{ label: 'Retry', onClick: () => void statusQ.refetch() }}
            />
          )
        ) : entries.length === 0 ? (
          <EmptyState
            icon={KeyRound}
            title="No components yet"
            description="Credential status appears here once components are registered."
          />
        ) : (
          <CredentialStatusTable entries={entries} />
        )}
      </PageContainer>
    </>
  );
}
