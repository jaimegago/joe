import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { ComponentRegisterForm } from './ComponentRegisterForm';
import { fetchComponentTypes, fetchComponents } from '@/api/components';

vi.mock('@/api/components', () => ({
  fetchComponentTypes: vi.fn(),
  fetchComponents: vi.fn(),
}));

const mockTypes = vi.mocked(fetchComponentTypes);
const mockComponents = vi.mocked(fetchComponents);

function renderForm(onSubmit = vi.fn()) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <ComponentRegisterForm open onOpenChange={vi.fn()} onSubmit={onSubmit} />
    </Wrapper>
  );
  return { onSubmit };
}

describe('ComponentRegisterForm', () => {
  beforeEach(() => {
    mockTypes.mockReset();
    mockTypes.mockResolvedValue(['kubernetes', 'prometheus', 'git']);
    mockComponents.mockReset();
    mockComponents.mockResolvedValue([]);
  });

  it('auto-slugifies the ID from Name: lowercase, invalid runs to one hyphen, trimmed edges', async () => {
    renderForm();
    const user = userEvent.setup();

    const name = screen.getByLabelText(/^name$/i);
    await user.type(name, 'My Prod Cluster!');
    expect(screen.getByLabelText(/^component id$/i)).toHaveValue('my-prod-cluster');

    // Leading/trailing junk and repeated separators collapse and trim.
    await user.clear(name);
    await user.type(name, '  __Weird--  Name__ ');
    expect(screen.getByLabelText(/^component id$/i)).toHaveValue('weird-name');
  });

  it('keeps the ID read-only until Edit unlocks it', async () => {
    renderForm();
    const user = userEvent.setup();

    const idInput = screen.getByLabelText(/^component id$/i);
    expect(idInput).toHaveAttribute('readonly');

    // Typing into the locked field changes nothing.
    await user.type(idInput, 'typed-while-locked');
    expect(idInput).toHaveValue('');

    await user.click(screen.getByRole('button', { name: /edit component id/i }));
    expect(idInput).not.toHaveAttribute('readonly');

    await user.type(idInput, 'manual-id');
    expect(idInput).toHaveValue('manual-id');
  });

  it('stops auto-syncing from Name after a manual ID edit', async () => {
    renderForm();
    const user = userEvent.setup();

    const name = screen.getByLabelText(/^name$/i);
    await user.type(name, 'First Name');
    const idInput = screen.getByLabelText(/^component id$/i);
    expect(idInput).toHaveValue('first-name');

    // Unlocking alone does not stop the sync — only an actual edit does.
    await user.click(screen.getByRole('button', { name: /edit component id/i }));
    await user.clear(name);
    await user.type(name, 'Second Name');
    expect(idInput).toHaveValue('second-name');

    await user.clear(idInput);
    await user.type(idInput, 'my-own-id');
    await user.clear(name);
    await user.type(name, 'Third Name');
    expect(idInput).toHaveValue('my-own-id');
  });

  it('flags a rule-violating ID and keeps submit disabled', async () => {
    renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Some Name');
    const idInput = screen.getByLabelText(/^component id$/i);
    await user.click(screen.getByRole('button', { name: /edit component id/i }));
    await user.clear(idInput);
    await user.type(idInput, 'Bad/ID');

    expect(idInput).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByRole('button', { name: /register/i })).toBeDisabled();
  });

  // --- git: type-conditional routing config (D-0150) ---
  //
  // Config carriage is deliberately per-type, not a generic config editor. The
  // git branch collects the repository URL and an optional hosting declaration;
  // every other type submits exactly the payload it always did.

  async function selectType(user: ReturnType<typeof userEvent.setup>, label: string) {
    await user.click(screen.getByRole('combobox', { name: /^type$/i }));
    await user.click(await screen.findByRole('option', { name: label }));
  }

  it('submits no config for a non-git type', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Prod Cluster');
    await selectType(user, 'kubernetes');
    expect(screen.queryByLabelText(/repository url/i)).not.toBeInTheDocument();

    await user.click(screen.getByRole('button', { name: /register/i }));
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'prod-cluster',
      type: 'kubernetes',
      name: 'Prod Cluster',
    });
  });

  it('requires a repository URL for git and submits it as config', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Platform Repo');
    await selectType(user, 'git');

    // A repository is unusable without its URL, so submit stays disabled.
    expect(screen.getByRole('button', { name: /register/i })).toBeDisabled();

    await user.type(
      screen.getByLabelText(/repository url/i),
      'https://github.com/org/platform.git'
    );
    await user.click(screen.getByRole('button', { name: /register/i }));

    expect(onSubmit).toHaveBeenCalledWith({
      id: 'platform-repo',
      type: 'git',
      name: 'Platform Repo',
      config: { url: 'https://github.com/org/platform.git' },
    });
  });

  it('offers the registered github and gitlab components as the hosting provider', async () => {
    const stamps = { created_at: '2026-08-07T00:00:00Z', updated_at: '2026-08-07T00:00:00Z' };
    mockComponents.mockResolvedValue([
      { id: 'gh-main', type: 'github', name: 'Corp GitHub', armed: true, status: 'ok', ...stamps },
      { id: 'gl-main', type: 'gitlab', name: 'Corp GitLab', armed: true, status: 'ok', ...stamps },
      { id: 'k8s-prod', type: 'kubernetes', name: 'Prod', armed: true, status: 'ok', ...stamps },
    ]);
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Platform Repo');
    await selectType(user, 'git');
    await user.type(screen.getByLabelText(/repository url/i), 'https://example.com/r.git');

    await user.click(screen.getByRole('combobox', { name: /hosting provider/i }));
    // Only repository hosts are offered — never every registered component.
    expect(screen.queryByRole('option', { name: /Prod \(kubernetes\)/ })).not.toBeInTheDocument();
    await user.click(await screen.findByRole('option', { name: /Corp GitHub \(github\)/ }));

    await user.click(screen.getByRole('button', { name: /register/i }));
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'platform-repo',
      type: 'git',
      name: 'Platform Repo',
      config: { url: 'https://example.com/r.git', provider_component_id: 'gh-main' },
    });
  });
});
