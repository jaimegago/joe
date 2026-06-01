import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { ProvidersTab } from './ProvidersTab';
import type { LLMProvider } from '@/api/types';

const providers: LLMProvider[] = [
  { name: 'claude', provider: 'anthropic', model: 'claude-x', configured: true, key_present: true },
  { name: 'gpt', provider: 'openai', model: 'gpt-x', configured: true, key_present: false },
];

describe('ProvidersTab', () => {
  it('renders key-presence as a boolean status and highlights the current selection', () => {
    render(<ProvidersTab providers={providers} current="claude" />);
    expect(screen.getByText('Present')).toBeInTheDocument();
    expect(screen.getByText('Absent')).toBeInTheDocument();
    expect(screen.getByText('Current')).toBeInTheDocument();
  });

  it('renders no key material — only presence booleans', () => {
    // The contract carries only booleans; assert no value-like field leaks.
    const { container } = render(<ProvidersTab providers={providers} current="claude" />);
    const text = container.textContent ?? '';
    expect(text).not.toMatch(/sk-/); // no API-key prefix
    expect(text).not.toMatch(/key_present/); // no raw field name
    // Presence is conveyed as a status word, never a key.
    expect(screen.getByText('Present')).toBeInTheDocument();
  });

  it('renders an empty state when no providers are configured', () => {
    render(<ProvidersTab providers={[]} current="" />);
    expect(screen.getByText('No providers')).toBeInTheDocument();
  });
});
