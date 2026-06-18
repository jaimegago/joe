import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { SkillsTable } from './SkillsTable';
import { reloadSkills, approveSkill, rejectSkill } from '@/api/skills';
import type { SkillsListResponse } from '@/api/types';

vi.mock('sonner', () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock('@/api/skills', () => ({
  reloadSkills: vi.fn(),
  approveSkill: vi.fn(),
  rejectSkill: vi.fn(),
}));

const mockReload = vi.mocked(reloadSkills);
const mockApprove = vi.mocked(approveSkill);
const mockReject = vi.mocked(rejectSkill);

const data: SkillsListResponse = {
  active: [
    {
      name: 'k8s-triage',
      description: 'Diagnose unhealthy pods',
      repo: 'github.com/jaimegago/joe-sre-skills',
      ref: 'main',
      commit: 'abcdef1234567890',
      status: 'active',
    },
  ],
  quarantined: [
    {
      name: 'sketchy-skill',
      repo: 'github.com/somebody/untrusted',
      status: 'quarantined',
      quarantine_reason: 'source not in trusted list',
    },
  ],
};

function renderTable(props: SkillsListResponse = data) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <SkillsTable data={props} />
    </Wrapper>
  );
}

describe('SkillsTable', () => {
  beforeEach(() => {
    mockReload.mockReset();
    mockApprove.mockReset();
    mockReject.mockReset();
  });

  it('shows each active skill with the git source it came from', () => {
    renderTable();
    expect(screen.getByText('k8s-triage')).toBeInTheDocument();
    expect(screen.getByText('github.com/jaimegago/joe-sre-skills')).toBeInTheDocument();
    expect(screen.getByText('ref: main')).toBeInTheDocument();
    // Commit is shown short, not the full hash.
    expect(screen.getByText('abcdef123456')).toBeInTheDocument();
  });

  it('lists quarantined skills with their reason', () => {
    renderTable();
    expect(screen.getByText('sketchy-skill')).toBeInTheDocument();
    expect(screen.getByText('source not in trusted list')).toBeInTheDocument();
    expect(screen.getByText('1 pending')).toBeInTheDocument();
  });

  it('reloads the registry on demand', async () => {
    mockReload.mockResolvedValue({ status: 'ok', trigger: 'manual', before: 1, after: 1 });
    renderTable();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: /Reload/ }));

    await waitFor(() => expect(mockReload).toHaveBeenCalledTimes(1));
  });

  it('approves a quarantined skill directly', async () => {
    mockApprove.mockResolvedValue({ status: 'ok', name: 'sketchy-skill', skills: ['sketchy-skill'] });
    renderTable();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Approve' }));

    await waitFor(() => expect(mockApprove).toHaveBeenCalledWith('sketchy-skill'));
  });

  it('rejects a skill only after confirmation', async () => {
    mockReject.mockResolvedValue({ status: 'ok', name: 'sketchy-skill', skills: ['sketchy-skill'] });
    renderTable();

    const user = userEvent.setup();
    await user.click(screen.getByRole('button', { name: 'Reject' }));

    // Reject is destructive — it fires only once the confirm dialog is accepted.
    expect(mockReject).not.toHaveBeenCalled();
    const dialog = await screen.findByRole('dialog');
    await user.click(within(dialog).getByRole('button', { name: 'Reject' }));

    await waitFor(() => expect(mockReject).toHaveBeenCalledWith('sketchy-skill'));
  });

  it('shows an empty state when no skills are active', () => {
    renderTable({ active: [], quarantined: [] });
    expect(screen.getByText('No active skills')).toBeInTheDocument();
  });
});
