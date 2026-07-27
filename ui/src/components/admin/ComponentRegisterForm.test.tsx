import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { ComponentRegisterForm } from './ComponentRegisterForm';
import { fetchComponentTypes } from '@/api/components';

vi.mock('@/api/components', () => ({
  fetchComponentTypes: vi.fn(),
}));

const mockTypes = vi.mocked(fetchComponentTypes);

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
    mockTypes.mockResolvedValue(['kubernetes', 'prometheus']);
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
});
