import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ReactFlowProvider, type NodeProps } from 'reactflow';
import { GenericNode } from './GenericNode';
import { NODE_KIND_CONFIG, DEFAULT_NODE_CONFIG } from '@/lib/constants';
import type { GraphNode } from '@/api/types';

// The regression net for session graph-ui-declutter: every graph node used to
// render a type glyph that was ALWAYS the ⚙️ fallback. NODE_KIND_CONFIG is keyed by
// TitleCase K8s names (Deployment, Service, ...) while the kinds the API actually
// emits are snake_case component/resource types (deployment, git_repo, ...), so the
// lookup never hit and DEFAULT_NODE_CONFIG.icon rendered on every node — a gear
// glyph that told the user nothing about the node's type. The glyph was dropped;
// the node's name, namespace, and kind text carry the type instead.
//
// GenericNode renders ReactFlow <Handle>s, which read the ReactFlow store, so the
// node is wrapped in a provider. Nothing else about the graph surface is exercised.
function renderNode(overrides: Partial<GraphNode> = {}) {
  const data: GraphNode = {
    id: 'n-1',
    kind: 'deployment',
    name: 'orders',
    namespace: 'shop',
    metadata: {},
    ...overrides,
  };
  const props = {
    id: data.id,
    data,
    selected: false,
    type: 'generic',
    zIndex: 0,
    isConnectable: false,
    xPos: 0,
    yPos: 0,
    dragging: false,
  } as unknown as NodeProps<GraphNode>;

  return render(
    <ReactFlowProvider>
      <GenericNode {...props} />
    </ReactFlowProvider>
  );
}

describe('GenericNode', () => {
  it('renders no type glyph for a real snake_case kind', () => {
    const { container } = renderNode({ kind: 'deployment' });
    // ⚙️ is DEFAULT_NODE_CONFIG.icon — what every node used to show.
    expect(container.textContent).not.toContain(DEFAULT_NODE_CONFIG.icon);
  });

  // The kinds the refreshers actually emit. None of them is a NODE_KIND_CONFIG key,
  // which is precisely why the lookup always fell through to the gear.
  it.each(['deployment', 'git_repo', 'helm_component', 'rds_instance'])(
    'renders no gear fallback for kind %s',
    (kind) => {
      const { container } = renderNode({ kind });
      expect(container.textContent).not.toContain(DEFAULT_NODE_CONFIG.icon);
    }
  );

  // Pins the removal of the glyph itself, not just the fallback: a TitleCase kind
  // does hit NODE_KIND_CONFIG, and must still render no icon.
  it('renders no configured icon even for a kind that hits NODE_KIND_CONFIG', () => {
    const { container } = renderNode({ kind: 'Deployment' });
    expect(container.textContent).not.toContain(NODE_KIND_CONFIG.Deployment.icon);
  });

  // Guards the test above from passing trivially: the node still renders, and the
  // text that carries the node's identity and type survived the glyph removal.
  it('still renders the name, namespace, and kind text', () => {
    renderNode({ kind: 'deployment', name: 'orders', namespace: 'shop' });
    expect(screen.getByText('orders')).toBeInTheDocument();
    expect(screen.getByText('shop')).toBeInTheDocument();
    expect(screen.getByText('deployment')).toBeInTheDocument();
  });
});
