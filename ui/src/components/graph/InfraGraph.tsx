import { useCallback, useEffect, useMemo, useState } from 'react';
import ReactFlow, {
  Background,
  Controls,
  MiniMap,
  type NodeTypes,
  type EdgeTypes,
  type NodeChange,
  type EdgeChange,
  applyNodeChanges,
  applyEdgeChanges,
  type Node,
  type Edge,
  MarkerType,
} from 'reactflow';
import 'reactflow/dist/style.css';
import type { Graph, GraphNode } from '@/api/types';
import { GenericNode } from './nodes/GenericNode';
import { DependencyEdge } from './edges/DependencyEdge';
import { GraphLegend } from './GraphLegend';
import { GraphControls } from './GraphControls';
import { NodeDetails } from './NodeDetails';
import { applyHierarchicalLayout, applyGridLayout } from '@/lib/graph-layout';
import { Button } from '@/components/ui/button';
import { RefreshCw } from 'lucide-react';

const nodeTypes: NodeTypes = {
  generic: GenericNode,
};

const edgeTypes: EdgeTypes = {
  dependency: DependencyEdge,
};

interface InfraGraphProps {
  graph: Graph;
  onRefresh?: () => void;
}

export function InfraGraph({ graph, onRefresh }: InfraGraphProps) {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [filterNamespace, setFilterNamespace] = useState('all');
  const [filterKind, setFilterKind] = useState('all');
  const [filterStatus, setFilterStatus] = useState('all');
  const [layout, setLayout] = useState<'hierarchical' | 'grid'>('hierarchical');

  // Filter nodes
  const filteredNodes = useMemo(() => {
    return graph.nodes.filter((n) => {
      if (filterNamespace !== 'all' && n.namespace !== filterNamespace) return false;
      if (filterKind !== 'all' && n.kind !== filterKind) return false;
      if (filterStatus !== 'all' && n.status !== filterStatus) return false;
      return true;
    });
  }, [graph.nodes, filterNamespace, filterKind, filterStatus]);

  const filteredNodeIds = useMemo(() => new Set(filteredNodes.map((n) => n.id)), [filteredNodes]);

  const filteredEdges = useMemo(() => {
    return graph.edges.filter(
      (e) => filteredNodeIds.has(e.source) && filteredNodeIds.has(e.target)
    );
  }, [graph.edges, filteredNodeIds]);

  // Convert to React Flow format
  const rfNodes = useMemo((): Node<GraphNode>[] => {
    const raw: Node<GraphNode>[] = filteredNodes.map((n) => ({
      id: n.id,
      type: 'generic',
      data: n,
      position: { x: 0, y: 0 },
      selected: n.id === selectedNodeId,
    }));
    const rfEdgesForLayout: Edge[] = filteredEdges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
    }));
    return layout === 'hierarchical'
      ? applyHierarchicalLayout(raw, rfEdgesForLayout)
      : applyGridLayout(raw);
  }, [filteredNodes, filteredEdges, layout, selectedNodeId]);

  const rfEdges = useMemo((): Edge[] => {
    return filteredEdges.map((e) => ({
      id: e.id,
      source: e.source,
      target: e.target,
      type: 'dependency',
      data: { type: e.type },
      markerEnd: { type: MarkerType.ArrowClosed, width: 12, height: 12, color: '#9ca3af' },
      animated: false,
    }));
  }, [filteredEdges]);

  const [nodes, setNodes] = useState(rfNodes);
  const [edges, setEdges] = useState(rfEdges);

  // Sync React Flow's controlled state when derived graph data changes.
  // React Flow requires controlled nodes/edges state to enable interactive node dragging;
  // updating it inside an effect is the React Flow recommended pattern for external data changes.
  useEffect(() => {
    setNodes(rfNodes);
    setEdges(rfEdges);
  }, [rfNodes, rfEdges]);

  const onNodesChange = useCallback(
    (changes: NodeChange[]) => setNodes((ns) => applyNodeChanges(changes, ns)),
    []
  );
  const onEdgesChange = useCallback(
    (changes: EdgeChange[]) => setEdges((es) => applyEdgeChanges(changes, es)),
    []
  );

  const onNodeClick = useCallback((_: React.MouseEvent, node: Node) => {
    setSelectedNodeId(node.id);
  }, []);

  const selectedNode = graph.nodes.find((n) => n.id === selectedNodeId) ?? null;

  return (
    <div className="flex h-full">
      <div className="relative flex-1">
        {/* Toolbar */}
        <div className="absolute left-4 top-4 z-10 flex items-center gap-2 flex-wrap">
          <GraphControls
            nodes={graph.nodes}
            filterNamespace={filterNamespace}
            filterKind={filterKind}
            filterStatus={filterStatus}
            layout={layout}
            onFilterNamespace={setFilterNamespace}
            onFilterKind={setFilterKind}
            onFilterStatus={setFilterStatus}
            onLayoutChange={setLayout}
          />
          {onRefresh && (
            <Button variant="outline" size="sm" className="h-8 text-xs" onClick={onRefresh}>
              <RefreshCw className="mr-1 h-3 w-3" />
              Refresh
            </Button>
          )}
        </div>
        {/* Legend */}
        <div className="absolute bottom-16 left-4 z-10">
          <GraphLegend />
        </div>
        <ReactFlow
          nodes={nodes}
          edges={edges}
          nodeTypes={nodeTypes}
          edgeTypes={edgeTypes}
          onNodesChange={onNodesChange}
          onEdgesChange={onEdgesChange}
          onNodeClick={onNodeClick}
          onPaneClick={() => setSelectedNodeId(null)}
          fitView
          fitViewOptions={{ padding: 0.2 }}
        >
          <Background />
          <Controls />
          <MiniMap nodeStrokeWidth={3} zoomable pannable />
        </ReactFlow>
      </div>
      {selectedNode && <NodeDetails node={selectedNode} onClose={() => setSelectedNodeId(null)} />}
    </div>
  );
}
