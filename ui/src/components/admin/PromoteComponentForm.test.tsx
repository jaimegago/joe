import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { PromoteComponentForm } from './PromoteComponentForm';
import { fetchPromotionRequirements, fetchPromotionCandidates } from '@/api/components';
import type { Component } from '@/api/types';

vi.mock('@/api/components', () => ({
  fetchPromotionRequirements: vi.fn(),
  fetchPromotionCandidates: vi.fn(),
}));

const mockReqs = vi.mocked(fetchPromotionRequirements);
const mockCands = vi.mocked(fetchPromotionCandidates);

const gitComponent = {
  id: 'platform-repo',
  type: 'git',
  name: 'Platform Repo',
  armed: false,
  status: 'unknown',
} as unknown as Component;

function renderForm(component: Component, onSubmit = vi.fn()) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <PromoteComponentForm
        open
        onOpenChange={vi.fn()}
        component={component}
        onSubmit={onSubmit}
      />
    </Wrapper>
  );
  return { onSubmit };
}

// The git promote surface (D-0150). git is the multi-kind type: an operator arms
// a repository either with a credential reference or with the explicit
// no-credential kind, and the latter must state plainly what it permits.
describe('PromoteComponentForm — git', () => {
  beforeEach(() => {
    mockReqs.mockReset();
    mockCands.mockReset();
    mockReqs.mockResolvedValue({
      type: 'git',
      wired: true,
      kind: 'static',
      selectable_kinds: ['static', 'none'],
      locator_fields: [{ name: 'env_var', required: true }],
      constraints: [],
    });
    mockCands.mockResolvedValue({
      type: 'git',
      wired: true,
      kind: 'static',
      prefix: 'JOE_GIT_',
      applicable: true,
      candidates: [],
    });
  });

  it('offers a credential choice and defaults to the type-level kind', async () => {
    renderForm(gitComponent);
    // The reference form is shown by default; the no-credential warning is not.
    expect(await screen.findByLabelText(/credential$/i)).toBeInTheDocument();
    expect(screen.queryByText(/unauthenticated/i)).not.toBeInTheDocument();
  });

  it('states plainly what the no-credential arm permits, and submits it', async () => {
    const { onSubmit } = renderForm(gitComponent);
    const user = userEvent.setup();

    await user.click(await screen.findByRole('combobox', { name: /credential$/i }));
    await user.click(await screen.findByRole('option', { name: /no credential/i }));

    // The form copy must name the actual privilege: an unauthenticated outbound
    // fetch of the named repository, plus the local copy it writes.
    const warning = screen.getByText(/unauthenticated/i);
    expect(warning).toBeInTheDocument();
    expect(screen.getByText(/Platform Repo/)).toBeInTheDocument();

    // The no-credential arm collects nothing, so Continue is immediately live.
    await user.click(screen.getByRole('button', { name: /continue/i }));
    await user.click(await screen.findByRole('button', { name: /^promote$/i }));

    expect(onSubmit).toHaveBeenCalledWith({ credential_provider: 'none' });
  });

  it('does not offer a credential choice for a single-kind type', async () => {
    mockReqs.mockResolvedValue({
      type: 'github',
      wired: true,
      kind: 'static',
      selectable_kinds: ['static'],
      locator_fields: [{ name: 'env_var', required: true }],
      constraints: [],
    });
    renderForm({ ...gitComponent, id: 'gh-main', type: 'github' } as Component);

    await screen.findByLabelText(/reference label/i);
    expect(screen.queryByRole('combobox', { name: /credential$/i })).not.toBeInTheDocument();
  });
});
