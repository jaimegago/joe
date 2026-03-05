import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MetricsCard } from './MetricsCard';

describe('MetricsCard', () => {
  it('renders title and value', () => {
    render(<MetricsCard title="Total Nodes" value={42} />);
    expect(screen.getByText('Total Nodes')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
  });

  it('renders string values', () => {
    render(<MetricsCard title="Status" value="Healthy" />);
    expect(screen.getByText('Healthy')).toBeInTheDocument();
  });

  it('renders subLabel when provided', () => {
    render(<MetricsCard title="Nodes" value={10} subLabel="across 3 clusters" />);
    expect(screen.getByText('across 3 clusters')).toBeInTheDocument();
  });

  it('does not render subLabel when omitted', () => {
    render(<MetricsCard title="Nodes" value={10} />);
    expect(screen.queryByText(/cluster/)).not.toBeInTheDocument();
  });
});
