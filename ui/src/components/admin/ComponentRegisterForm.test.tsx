import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { ComponentRegisterForm } from './ComponentRegisterForm';
import { fetchComponentTypes, fetchComponents } from '@/api/components';
import type { ComponentTypeDescriptor } from '@/api/types';

vi.mock('@/api/components', () => ({
  fetchComponentTypes: vi.fn(),
  fetchComponents: vi.fn(),
}));

const mockTypes = vi.mocked(fetchComponentTypes);
const mockComponents = vi.mocked(fetchComponents);

// Descriptors in the shape GET /api/v1/component-types serves (D-0168). The
// dialog renders every field from these — nothing in it is specific to a type.
const DESCRIPTORS: ComponentTypeDescriptor[] = [
  {
    name: 'kubernetes',
    fields: [
      { name: 'api_server', type: 'url', phase: 'promotion', label: 'API server URL' },
      { name: 'auth_method', type: 'string', phase: 'promotion', label: 'Auth method' },
      {
        name: 'ca_data',
        type: 'string',
        phase: 'optional',
        label: 'CA bundle (PEM)',
        multiline: true,
      },
      { name: 'namespace', type: 'string', phase: 'optional', label: 'Default namespace' },
    ],
  },
  {
    name: 'prometheus',
    fields: [
      { name: 'url', type: 'url', phase: 'registration', label: 'Prometheus URL' },
      { name: 'org_id', type: 'string', phase: 'optional', label: 'Tenant ID (X-Scope-OrgID)' },
    ],
  },
  {
    name: 'git',
    fields: [
      { name: 'url', type: 'url', phase: 'registration', label: 'Repository URL' },
      { name: 'branch', type: 'string', phase: 'optional', default: 'HEAD', label: 'Branch' },
      {
        name: 'provider_component_id',
        type: 'string',
        phase: 'optional',
        label: 'Hosting provider',
        refers_to: ['github', 'gitlab'],
      },
    ],
  },
  {
    name: 'newrelic',
    fields: [
      { name: 'account_id', type: 'integer', phase: 'registration', label: 'Account ID' },
      { name: 'region', type: 'string', phase: 'optional', default: 'US', label: 'Region' },
    ],
  },
  {
    name: 'argocd',
    fields: [
      { name: 'url', type: 'url', phase: 'registration', label: 'Argo CD URL' },
      {
        name: 'insecure_tls',
        type: 'boolean',
        phase: 'optional',
        default: false,
        label: 'Skip TLS verification',
      },
    ],
  },
  {
    name: 'github',
    fields: [
      {
        name: 'base_url',
        type: 'url',
        phase: 'optional',
        default: 'https://api.github.com',
        label: 'API base URL',
      },
    ],
  },
];

function renderForm(onSubmit = vi.fn()) {
  const { Wrapper } = createWrapper();
  render(
    <Wrapper>
      <ComponentRegisterForm open onOpenChange={vi.fn()} onSubmit={onSubmit} />
    </Wrapper>
  );
  return { onSubmit };
}

async function selectType(user: ReturnType<typeof userEvent.setup>, label: string) {
  await user.click(screen.getByRole('combobox', { name: /^type$/i }));
  await user.click(await screen.findByRole('option', { name: label }));
}

function registerButton() {
  return screen.getByRole('button', { name: /register/i });
}

describe('ComponentRegisterForm', () => {
  beforeEach(() => {
    mockTypes.mockReset();
    mockTypes.mockResolvedValue(DESCRIPTORS);
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
    expect(registerButton()).toBeDisabled();
  });

  // --- routing fields rendered from the type's descriptor (D-0168) ---

  it('submits no config when a type has nothing required and nothing is set', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Prod Cluster');
    await selectType(user, 'kubernetes');

    // Promotion-phase fields are not collected here; the dialog says where they go.
    expect(screen.queryByLabelText(/api server url/i)).not.toBeInTheDocument();
    expect(
      screen.getByText(/supplied at promotion: api server url, auth method/i)
    ).toBeInTheDocument();
    // An optional multi-line field is collected in a textarea, so a PEM keeps its lines.
    expect(screen.getByLabelText(/ca bundle/i).tagName).toBe('TEXTAREA');

    await user.click(registerButton());
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'prod-cluster',
      type: 'kubernetes',
      name: 'Prod Cluster',
    });
  });

  it('marks a registration-required URL required and refuses a malformed one', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Prod Prometheus');
    await selectType(user, 'prometheus');

    const url = screen.getByLabelText(/prometheus url/i);
    expect(url).toBeRequired();
    // Required before submit: nothing typed, nothing to send.
    expect(registerButton()).toBeDisabled();

    // The missing-scheme mistake is refused before the server sees it.
    await user.type(url, 'prometheus:9090');
    expect(url).toHaveAttribute('aria-invalid', 'true');
    expect(screen.getByText(/must be an absolute url/i)).toBeInTheDocument();
    expect(registerButton()).toBeDisabled();

    await user.clear(url);
    await user.type(url, 'http://prometheus:9090');
    await user.click(registerButton());
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'prod-prometheus',
      type: 'prometheus',
      name: 'Prod Prometheus',
      config: { url: 'http://prometheus:9090' },
    });
  });

  it('requires a repository URL for git and submits it as config', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Platform Repo');
    await selectType(user, 'git');

    // A repository is unusable without its URL, so submit stays disabled.
    expect(registerButton()).toBeDisabled();

    await user.type(
      screen.getByLabelText(/repository url/i),
      'https://github.com/org/platform.git'
    );
    await user.click(registerButton());

    // The optional branch was left empty, so it is not sent: the adapter's
    // default (HEAD) applies.
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
    // Only the types the field refers to are offered — never every registered component.
    expect(screen.queryByRole('option', { name: /Prod \(kubernetes\)/ })).not.toBeInTheDocument();
    await user.click(await screen.findByRole('option', { name: /Corp GitHub \(github\)/ }));

    await user.click(registerButton());
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'platform-repo',
      type: 'git',
      name: 'Platform Repo',
      config: { url: 'https://example.com/r.git', provider_component_id: 'gh-main' },
    });
  });

  it('sends an integer as a JSON number and refuses a non-integer', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'NR');
    await selectType(user, 'newrelic');

    const account = screen.getByLabelText(/account id/i);
    await user.type(account, '12a');
    expect(screen.getByText(/must be a whole number/i)).toBeInTheDocument();
    expect(registerButton()).toBeDisabled();

    await user.clear(account);
    await user.type(account, '1234567');
    await user.type(screen.getByLabelText(/region/i), 'EU');
    await user.click(registerButton());
    expect(onSubmit).toHaveBeenCalledWith({
      id: 'nr',
      type: 'newrelic',
      name: 'NR',
      config: { account_id: 1234567, region: 'EU' },
    });
  });

  it('sends a checked boolean as true', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Argo');
    await selectType(user, 'argocd');
    await user.type(screen.getByLabelText(/argo cd url/i), 'https://argocd.example.com');
    await user.click(screen.getByRole('checkbox', { name: /skip tls verification/i }));
    await user.click(registerButton());

    expect(onSubmit).toHaveBeenCalledWith({
      id: 'argo',
      type: 'argocd',
      name: 'Argo',
      config: { url: 'https://argocd.example.com', insecure_tls: true },
    });
  });

  it('shows an optional field’s default and sends nothing when it is left empty', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'GH');
    await selectType(user, 'github');

    expect(screen.getByLabelText(/api base url/i)).not.toBeRequired();
    expect(screen.getByText(/defaults to https:\/\/api\.github\.com/i)).toBeInTheDocument();
    await user.click(registerButton());
    expect(onSubmit).toHaveBeenCalledWith({ id: 'gh', type: 'github', name: 'GH' });
  });

  it('drops the previous type’s values when the type changes', async () => {
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Switcher');
    await selectType(user, 'prometheus');
    await user.type(screen.getByLabelText(/tenant id/i), 'team-a');
    await selectType(user, 'github');
    await user.click(registerButton());

    expect(onSubmit).toHaveBeenCalledWith({ id: 'switcher', type: 'github', name: 'Switcher' });
  });

  it('renders a type it has never seen from its descriptor alone', async () => {
    mockTypes.mockResolvedValue([
      {
        name: 'acme',
        fields: [{ name: 'endpoint', type: 'url', phase: 'registration', label: 'Acme endpoint' }],
      },
    ]);
    const { onSubmit } = renderForm();
    const user = userEvent.setup();

    await user.type(screen.getByLabelText(/^name$/i), 'Acme');
    await selectType(user, 'acme');
    await user.type(screen.getByLabelText(/acme endpoint/i), 'https://acme.example.com');
    await user.click(registerButton());

    expect(onSubmit).toHaveBeenCalledWith({
      id: 'acme',
      type: 'acme',
      name: 'Acme',
      config: { endpoint: 'https://acme.example.com' },
    });
  });
});
