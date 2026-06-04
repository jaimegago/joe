import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { AssistantTurnView } from './AssistantTurnView';
import type { AssistantTurn } from '@/hooks/useChat';

// Item 8: a denied write does not fail the turn (the LLM still answers), so a
// completed turn carrying a write-failure code renders a dedicated, specific
// notice. A turn with no code renders no notice.
function turn(overrides: Partial<AssistantTurn>): AssistantTurn {
  return {
    steps: [],
    finalAnswer: 'Here is what I found.',
    status: 'completed',
    tokens: 10,
    ...overrides,
  };
}

describe('AssistantTurnView write-failure notice', () => {
  it('shows the zone_denial message on a completed turn with that code', () => {
    render(<AssistantTurnView turn={turn({ writeFailureCode: 'zone_denial' })} />);
    const notice = screen.getByTestId('write-failure-notice');
    expect(notice).toHaveTextContent('Access to this zone has not been granted to you.');
  });

  it('shows the incident_mode message on a completed turn with that code', () => {
    render(<AssistantTurnView turn={turn({ writeFailureCode: 'incident_mode' })} />);
    expect(screen.getByTestId('write-failure-notice')).toHaveTextContent(
      'System is in incident mode. Writes are temporarily blocked.'
    );
  });

  it('renders no notice when there is no write-failure code', () => {
    render(<AssistantTurnView turn={turn({})} />);
    expect(screen.queryByTestId('write-failure-notice')).not.toBeInTheDocument();
  });
});
