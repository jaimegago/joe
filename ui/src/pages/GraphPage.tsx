import { useGraph } from '@/hooks/useGraph';
import { ApiRequestError } from '@/api/client';
import { Header } from '@/components/layout/Header';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { InfraGraph } from '@/components/graph/InfraGraph';
import { Network, Lock } from 'lucide-react';

// graphErrorState maps a failed graph fetch to a user-facing empty state. It
// distinguishes an authorization denial (403) and an expired session (401)
// from a genuine connectivity/server failure, so a permission problem is no
// longer misreported as "joe isn't running".
function graphErrorState(error: unknown): {
  icon: typeof Network;
  title: string;
  description: string;
} {
  if (error instanceof ApiRequestError) {
    if (error.status === 403) {
      return {
        icon: Lock,
        title: 'No access to the graph',
        description:
          "Your account doesn't have permission to view the infrastructure graph. Ask an administrator to grant your account access.",
      };
    }
    if (error.status === 401) {
      return {
        icon: Lock,
        title: 'Session expired',
        description: 'Your session has expired. Please sign in again.',
      };
    }
  }
  return {
    icon: Network,
    title: 'Failed to load graph',
    description: "Could not connect to joe. Make sure it's running on :7777.",
  };
}

export function GraphPage() {
  const { data: graph, isLoading, isError, error, refetch } = useGraph();

  if (isLoading) return <LoadingPage />;

  if (isError || !graph) {
    const state = graphErrorState(error);
    return (
      <>
        <Header title="Infrastructure Graph" />
        <EmptyState
          icon={state.icon}
          title={state.title}
          description={state.description}
          action={{ label: 'Retry', onClick: () => void refetch() }}
        />
      </>
    );
  }

  if (graph.nodes.length === 0) {
    return (
      <>
        <Header title="Infrastructure Graph" />
        <EmptyState
          icon={Network}
          title="No infrastructure nodes"
          description="Connect some sources to populate the graph."
        />
      </>
    );
  }

  return (
    <div className="flex h-screen flex-col">
      <Header title="Infrastructure Graph" />
      <div className="flex-1 overflow-hidden">
        <InfraGraph graph={graph} onRefresh={() => void refetch()} />
      </div>
    </div>
  );
}
