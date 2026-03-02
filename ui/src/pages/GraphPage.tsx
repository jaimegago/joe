import { useGraph } from '@/hooks/useGraph';
import { Header } from '@/components/layout/Header';
import { LoadingPage } from '@/components/common/LoadingSpinner';
import { EmptyState } from '@/components/common/EmptyState';
import { InfraGraph } from '@/components/graph/InfraGraph';
import { Network } from 'lucide-react';

export function GraphPage() {
  const { data: graph, isLoading, isError, refetch } = useGraph();

  if (isLoading) return <LoadingPage />;

  if (isError || !graph) {
    return (
      <>
        <Header title="Infrastructure Graph" />
        <EmptyState
          icon={Network}
          title="Failed to load graph"
          description="Could not connect to joecored. Make sure it's running on :7777."
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
