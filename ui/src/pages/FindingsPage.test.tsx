import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router';
import { createWrapper } from '@/test/utils';
import { FindingsPage } from './FindingsPage';
import { fetchRunFindings } from '@/api/runFindings';
import type { RunFinding, RunFindingsResponse } from '@/api/types';

vi.mock('@/api/runFindings', () => ({
  fetchRunFindings: vi.fn(),
  fetchProposedAdmissions: vi.fn(),
}));
const mockFetch = vi.mocked(fetchRunFindings);

function finding(over: Partial<RunFinding>): RunFinding {
  return {
    id: 'row-' + (over.key ?? 'k'),
    key: 'k',
    run_id: 'run-1',
    scope_key: 'scope',
    session_id: 'sess-1',
    type: 'issue',
    reference: 'self_report',
    subject_node_id: 'default/svc-a',
    subject_component_id: 'k8s',
    claim: 'claim',
    consequence: 'degraded',
    band: 'P2',
    reach_count: 0,
    weight: 1,
    derivation: {
      consequence: 'degraded',
      evidence_line: 'CrashLoopBackOff',
      reach: { dependents: [], components: [], count: 0 },
      weight: 1,
      base_band: 'P2',
      band: 'P2',
      movement: 'none',
    },
    label: 'new',
    evidence_session_id: 'sess-1',
    established_at: '2026-10-28T12:00:00Z',
    created_at: '2026-10-28T12:00:00Z',
    ...over,
  };
}

const response: RunFindingsResponse = {
  runs: [
    {
      run_id: 'run-1',
      scope_key: 'scope',
      session_id: 'sess-1',
      created_at: '2026-10-28T12:00:00Z',
      finding_count: 4,
    },
  ],
  findings: [
    finding({
      key: 'db',
      type: 'issue',
      subject_node_id: 'pg/primary',
      subject_component_id: 'pg',
      claim: 'database not accepting connections',
      consequence: 'unavailable',
      band: 'P0',
      reach_count: 3,
      label: 'new',
      derivation: {
        consequence: 'unavailable',
        evidence_line: 'pg_isready: no response',
        reach: { dependents: ['a', 'b', 'c'], components: ['k8s'], count: 3 },
        weight: 1,
        base_band: 'P1',
        band: 'P0',
        movement: 'raised',
      },
    }),
    finding({
      key: 'replica',
      type: 'risk',
      claim: 'single replica',
      condition: 'a node drain takes the only pod down',
      consequence: 'would_be_unavailable',
      band: 'P2',
      label: 'standing',
      evidence_session_id: 'sess-0',
      proposal: {
        shape: 'live_object_change',
        target: { node_id: 'default/svc-a', component_id: 'k8s', system_id: 'default/svc-a' },
        base: 'generation 7',
        change: 'spec.replicas: 2',
        expected_effect: '2 replicas available',
        limits: 'capacity not checked',
        status: 'unverified',
        executable: true,
      },
    }),
    finding({ key: 'old', claim: 'crashlooping', band: 'P2', label: 'resolved' }),
    finding({
      key: 'gu',
      type: 'graph_update',
      claim: 'stores_in points at an unregistered datastore',
      consequence: undefined,
      band: 'P3',
      label: 'new',
    }),
  ],
  count: 4,
};

function renderPage() {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <MemoryRouter initialEntries={['/findings']}>
        <FindingsPage />
      </MemoryRouter>
    </Wrapper>
  );
}

describe('FindingsPage', () => {
  beforeEach(() => {
    mockFetch.mockReset();
  });

  it('groups the latest run’s findings by band with their labels, and links each to its node and evidence', async () => {
    mockFetch.mockResolvedValue(response);
    renderPage();

    expect(await screen.findByText('database not accepting connections')).toBeInTheDocument();

    // By band: the P0 section holds the database issue, P2 the risk and the
    // resolved issue, P1 and P3 say they are empty.
    const p0 = screen.getByTestId('band-P0');
    expect(within(p0).getByText('database not accepting connections')).toBeInTheDocument();
    expect(within(screen.getByTestId('band-P1')).getByText('No P1 findings.')).toBeInTheDocument();
    const p2 = screen.getByTestId('band-P2');
    expect(within(p2).getByText('single replica')).toBeInTheDocument();
    expect(within(p2).getByText('crashlooping')).toBeInTheDocument();
    expect(within(screen.getByTestId('band-P3')).getByText('No P3 findings.')).toBeInTheDocument();

    // Labels: new, standing and resolved all render.
    const labels = screen.getAllByTestId('finding-label').map((el) => el.textContent);
    expect(labels).toEqual(expect.arrayContaining(['new', 'standing', 'resolved']));

    // A graph update is NOT in the ranked list; it is pointed at for admins.
    expect(
      screen.queryByText('stores_in points at an unregistered datastore')
    ).not.toBeInTheDocument();
    expect(screen.getByTestId('admissions-note')).toHaveTextContent('1 proposed admission');

    // Links: subject node → graph page selecting that node; evidence → the
    // session where the finding was established (the previous run's for a
    // standing finding carried from it).
    const subjectLinks = screen.getAllByTestId('subject-link');
    expect(subjectLinks[0]).toHaveAttribute('href', '/graph?node=pg%2Fprimary');
    const evidenceLinks = screen.getAllByTestId('evidence-link');
    expect(evidenceLinks.map((a) => a.getAttribute('href'))).toEqual(
      expect.arrayContaining(['/chat/sess-1', '/chat/sess-0'])
    );

    // The derivation is rendered from the record, and so is the condition.
    expect(within(p0).getByTestId('finding-derivation')).toHaveTextContent(
      'Base P1 from unavailable; reach 3 dependents; weight 1; raised.'
    );
    expect(screen.getByText(/a node drain takes the only pod down/)).toBeInTheDocument();

    // The proposal is labelled unverified, with its shape and the five fields.
    const proposal = screen.getByTestId('finding-proposal');
    expect(within(proposal).getByText('unverified')).toBeInTheDocument();
    expect(within(proposal).getByText('live object change')).toBeInTheDocument();
    expect(within(proposal).getByText('spec.replicas: 2')).toBeInTheDocument();
    expect(within(proposal).getByText(/2 replicas available/)).toBeInTheDocument();
    expect(within(proposal).getByText(/capacity not checked/)).toBeInTheDocument();
    expect(within(proposal).getByText(/generation 7/)).toBeInTheDocument();

    // The run row links to the run's session, and its run pointer to that run
    // on the Auto Joe page.
    expect(screen.getByTestId('run-row')).toHaveTextContent('4 findings');
    expect(screen.getByTestId('run-link')).toHaveAttribute('href', '/auto-joe?run=run-1');
  });

  it('shows a clean latest run as clean, not as the previous run’s list', async () => {
    mockFetch.mockResolvedValue({
      runs: [response.runs[0]],
      findings: [],
      count: 0,
    });
    renderPage();
    expect(await screen.findByText('No findings')).toBeInTheDocument();
    expect(screen.getByTestId('run-row')).toBeInTheDocument();
  });

  it('shows an empty state before any run has reported', async () => {
    mockFetch.mockResolvedValue({ runs: [], findings: [], count: 0 });
    renderPage();
    expect(await screen.findByText('No runs yet')).toBeInTheDocument();
  });

  it('renders the shared error panel when the query fails', async () => {
    mockFetch.mockRejectedValue(new Error('boom'));
    renderPage();
    expect(await screen.findByText("Couldn't load findings")).toBeInTheDocument();
  });
});
