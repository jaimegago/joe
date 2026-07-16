import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { InfraGraph } from './InfraGraph';
import { NODE_KIND_CONFIG, DEFAULT_NODE_CONFIG } from '@/lib/constants';
import type { Graph } from '@/api/types';

// The regression net for session graph-ui-declutter, legend leg: the graph surface
// used to render a GraphLegend advertising the first 8 NODE_KIND_CONFIG entries
// (Deployment/Service/Pod/Database/Cache/Queue/External/Secret) with their icons.
// Those TitleCase names match neither the snake_case kinds the API emits nor
// anything the refreshers produce, so the legend described a node taxonomy that
// does not exist — and, being an icon-plus-label palette on a product with no graph
// editing, read as an add-node palette. It was inert and was deleted.
//
// The invariant pinned here covers both legs of that session at the surface level:
// the graph renders NO NODE_KIND_CONFIG icon (the legend palette) and no
// DEFAULT_NODE_CONFIG gear (the dead per-node glyph).
const graph: Graph = {
  nodes: [
    { id: 'n1', kind: 'deployment', name: 'orders', namespace: 'shop', metadata: {} },
    { id: 'n2', kind: 'git_repo', name: 'shop-config', metadata: {} },
  ],
  edges: [],
};

describe('InfraGraph', () => {
  it('renders no node-kind icon palette', () => {
    const { container } = render(<InfraGraph graph={graph} />);
    for (const [kind, cfg] of Object.entries(NODE_KIND_CONFIG)) {
      expect(
        container.textContent,
        `graph surface must not render the ${kind} legend icon`
      ).not.toContain(cfg.icon);
    }
  });

  it('renders none of the fictional legend labels', () => {
    const { container } = render(<InfraGraph graph={graph} />);
    // The exact 8 the deleted legend advertised.
    for (const label of [
      'Deployment',
      'Service',
      'Pod',
      'Database',
      'Cache',
      'Queue',
      'External',
      'Secret',
    ]) {
      expect(container.textContent, `graph surface must not advertise "${label}"`).not.toContain(
        label
      );
    }
  });

  it('renders no gear fallback glyph', () => {
    const { container } = render(<InfraGraph graph={graph} />);
    expect(container.textContent).not.toContain(DEFAULT_NODE_CONFIG.icon);
  });

  // Guards the assertions above from passing trivially on an empty render: the
  // chrome the session deliberately KEPT (the filters) and the nodes themselves
  // must still be there.
  it('still renders the live filter chrome and the graph nodes', () => {
    render(<InfraGraph graph={graph} />);
    expect(screen.getByText('All namespaces')).toBeInTheDocument();
    expect(screen.getByText('All kinds')).toBeInTheDocument();
    expect(screen.getByText('All statuses')).toBeInTheDocument();
    expect(screen.getByText('orders')).toBeInTheDocument();
    expect(screen.getByText('deployment')).toBeInTheDocument();
  });
});
