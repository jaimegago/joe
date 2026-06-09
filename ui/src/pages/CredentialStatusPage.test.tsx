import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { CredentialStatusPage } from './CredentialStatusPage';
import {
  fetchCredentialStatuses,
  probeCredential,
  fetchCredentialStderr,
} from '@/api/credentialStatus';
import type { CredentialStatusEntry, CredentialProbeResponse } from '@/api/types';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/credentialStatus', () => ({
  fetchCredentialStatuses: vi.fn(),
  probeCredential: vi.fn(),
  fetchCredentialStderr: vi.fn(),
}));

const mockList = vi.mocked(fetchCredentialStatuses);
const mockProbe = vi.mocked(probeCredential);
const mockStderr = vi.mocked(fetchCredentialStderr);

const entries: CredentialStatusEntry[] = [
  {
    component_id: 'prod-cluster',
    type: 'kubernetes',
    name: 'Prod Cluster',
    descriptor: { provider: 'kubeconfig-exec', context: 'prod', audience: 'cluster' },
  },
  {
    component_id: 'github-main',
    type: 'git',
    name: 'GitHub',
    descriptor: { provider: 'static', audience: 'github' },
  },
];

function renderPage() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <CredentialStatusPage />
    </Wrapper>
  );
}

describe('CredentialStatusPage', () => {
  beforeEach(() => {
    mockList.mockReset();
    mockProbe.mockReset();
    mockStderr.mockReset();
  });

  it('renders the passive descriptor for each component without probing', async () => {
    mockList.mockResolvedValue(entries);
    renderPage();

    expect(await screen.findByText('prod-cluster')).toBeInTheDocument();
    expect(screen.getByText('kubeconfig-exec')).toBeInTheDocument();
    expect(screen.getByText('static')).toBeInTheDocument();
    // No probe happens on load — connectivity is "Not probed" until asked.
    expect(screen.getAllByText('Not probed').length).toBe(2);
    expect(mockProbe).not.toHaveBeenCalled();
  });

  it('renders connectivity-verified after a successful probe', async () => {
    mockList.mockResolvedValue([entries[1]]);
    const ok: CredentialProbeResponse = {
      component_id: 'github-main',
      diagnostic: { component_id: 'github-main', provider: 'static', stage: 'connectivity-probed', ok: true },
      stderr_available: false,
    };
    mockProbe.mockResolvedValue(ok);
    renderPage();
    await screen.findByText('github-main');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Probe now' }));

    expect(await screen.findByText('Connectivity verified')).toBeInTheDocument();
  });

  it('renders the lazy "minted, not yet proven" state as legitimate, not a failure', async () => {
    mockList.mockResolvedValue([entries[0]]);
    const lazy: CredentialProbeResponse = {
      component_id: 'prod-cluster',
      diagnostic: { component_id: 'prod-cluster', provider: 'kubeconfig-exec', stage: 'mint-succeeded', ok: true },
      stderr_available: false,
    };
    mockProbe.mockResolvedValue(lazy);
    renderPage();
    await screen.findByText('prod-cluster');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Probe now' }));

    expect(await screen.findByText('Minted · not yet proven')).toBeInTheDocument();
    // It is NOT a failure: no failure badge or detail row.
    expect(screen.queryByText(/Failed at/)).not.toBeInTheDocument();
  });

  it('surfaces captured plugin stderr only behind an explicit expand action', async () => {
    mockList.mockResolvedValue([entries[0]]);
    const failed: CredentialProbeResponse = {
      component_id: 'prod-cluster',
      diagnostic: {
        component_id: 'prod-cluster',
        provider: 'kubeconfig-exec',
        stage: 'mint-attempted',
        ok: false,
        reason: 'credential mint failed (exec plugin error)',
      },
      stderr_available: true,
    };
    mockProbe.mockResolvedValue(failed);
    mockStderr.mockResolvedValue('exec: aws-iam-authenticator: token Bearer eyJhbGci...');
    renderPage();
    await screen.findByText('prod-cluster');

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Probe now' }));

    // The failure stage and non-sensitive reason render inline.
    expect(await screen.findByText('Failed at mint-attempted')).toBeInTheDocument();
    expect(screen.getByText('credential mint failed (exec plugin error)')).toBeInTheDocument();

    // The stderr is NOT fetched or shown until the operator asks for it.
    expect(mockStderr).not.toHaveBeenCalled();
    expect(screen.queryByText(/aws-iam-authenticator/)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: 'Show plugin output' }));

    await waitFor(() => expect(mockStderr).toHaveBeenCalledWith('prod-cluster'));
    expect(await screen.findByText(/aws-iam-authenticator/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Copy/ })).toBeInTheDocument();
  });

  it('shows the empty state when there are no components', async () => {
    mockList.mockResolvedValue([]);
    renderPage();
    expect(await screen.findByText('No components yet')).toBeInTheDocument();
  });
});
