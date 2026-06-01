import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { createWrapper } from '@/test/utils';
import { UsageTab } from './UsageTab';
import { fetchUsageAggregate, fetchPerModelUsage, fetchPerPrincipalUsage } from '@/api/llm';

vi.mock('@/api/llm', () => ({
  fetchUsageAggregate: vi.fn(),
  fetchPerModelUsage: vi.fn(),
  fetchPerPrincipalUsage: vi.fn(),
}));
const mockAggregate = vi.mocked(fetchUsageAggregate);
const mockPerModel = vi.mocked(fetchPerModelUsage);
const mockPerPrincipal = vi.mocked(fetchPerPrincipalUsage);

function renderTab(isAdmin: boolean) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <UsageTab isAdmin={isAdmin} />
    </Wrapper>
  );
}

describe('UsageTab per-principal gating', () => {
  beforeEach(() => {
    mockAggregate.mockReset();
    mockPerModel.mockReset();
    mockPerPrincipal.mockReset();
    mockAggregate.mockResolvedValue({ today: [], week: [], month: [] });
    mockPerModel.mockResolvedValue({ window: 'day', rows: [] });
    mockPerPrincipal.mockResolvedValue({ window: 'day', rows: [] });
  });

  it('shows the per-principal section and requests it for an admin', async () => {
    renderTab(true);
    expect(await screen.findByText('Per principal')).toBeInTheDocument();
    await waitFor(() => expect(mockPerPrincipal).toHaveBeenCalled());
  });

  it('does not show or request the per-principal section for a non-admin', async () => {
    renderTab(false);
    // Wait for the aggregate to resolve so the tab has rendered.
    expect(await screen.findByText('Per model')).toBeInTheDocument();
    expect(screen.queryByText('Per principal')).not.toBeInTheDocument();
    expect(mockPerPrincipal).not.toHaveBeenCalled();
  });
});
