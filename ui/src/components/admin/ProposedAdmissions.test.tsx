import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { createWrapper } from '@/test/utils';
import { ProposedAdmissions } from './ProposedAdmissions';
import { fetchProposedAdmissions } from '@/api/runFindings';
import type { RunFindingsResponse } from '@/api/types';

vi.mock('@/api/runFindings', () => ({
  fetchRunFindings: vi.fn(),
  fetchProposedAdmissions: vi.fn(),
}));
const mockFetch = vi.mocked(fetchProposedAdmissions);

const admissions: RunFindingsResponse = {
  runs: [],
  findings: [
    {
      id: 'row-gu',
      key: 'gu',
      run_id: 'run-1',
      scope_key: 'scope',
      session_id: 'sess-1',
      type: 'graph_update',
      reference: 'graph',
      subject_node_id: 'default/svc-a',
      subject_component_id: 'k8s',
      claim: 'stores_in points at a redis no component covers',
      band: 'P3',
      reach_count: 0,
      weight: 1,
      derivation: {
        consequence: '',
        evidence_line: 'edge default/svc-a -stores_in-> redis-unknown',
        reach: { dependents: [], components: [], count: 0 },
        weight: 1,
        base_band: 'P3',
        band: 'P3',
        movement: 'none',
      },
      proposal: {
        shape: 'operator_action',
        target: { node_id: 'default/svc-a', component_id: 'k8s', system_id: 'default/svc-a' },
        base: 'no redis component registered',
        change: 'register the redis instance as a component',
        expected_effect: 'the stores_in edge resolves to a registered component',
        limits: 'the redis type may be blocked by the allow-list',
        status: 'unverified',
        executable: false,
      },
      label: 'new',
      evidence_session_id: 'sess-1',
      established_at: '2026-10-28T12:00:00Z',
      created_at: '2026-10-28T12:00:00Z',
    },
  ],
  count: 1,
};

function renderList(enabled: boolean) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter>
        <ProposedAdmissions enabled={enabled} />
      </MemoryRouter>
    </Wrapper>
  );
}

describe('ProposedAdmissions', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('renders nothing and fetches nothing for a non-admin', () => {
    renderList(false);
    expect(screen.queryByTestId('proposed-admissions')).not.toBeInTheDocument();
    expect(mockFetch).not.toHaveBeenCalled();
  });

  it('lists each proposed admission with its evidence and its not-executable proposal', async () => {
    mockFetch.mockResolvedValue(admissions);
    renderList(true);
    expect(
      await screen.findByText('stores_in points at a redis no component covers')
    ).toBeInTheDocument();
    expect(screen.getByText('edge default/svc-a -stores_in-> redis-unknown')).toBeInTheDocument();
    expect(screen.getByText('not executable')).toBeInTheDocument();
    expect(screen.getByText('unverified')).toBeInTheDocument();
    expect(screen.getByText(/may be blocked by the allow-list/)).toBeInTheDocument();
    expect(screen.getByText('register the redis instance as a component')).toBeInTheDocument();
    expect(screen.getByTestId('subject-link')).toHaveAttribute(
      'href',
      '/graph?node=default%2Fsvc-a'
    );
  });

  it('says so when the latest runs proposed no admission', async () => {
    mockFetch.mockResolvedValue({ runs: [], findings: [], count: 0 });
    renderList(true);
    expect(await screen.findByText(/proposed no admission/)).toBeInTheDocument();
  });
});
