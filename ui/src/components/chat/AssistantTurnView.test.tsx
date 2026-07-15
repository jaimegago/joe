import { describe, it, expect, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
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
    inputTokens: 8,
    contextWindow: 0,
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

// loop-budget-exhaustion: a COMPLETED turn whose answer was synthesized after
// the loop hit its step budget carries stop_reason 'max_iterations' and renders
// an amber truncation notice — distinct from the red failure banner, which is
// reserved for the max_iterations_reached STATUS (synthesis-failure path).
describe('AssistantTurnView max-iterations truncation notice', () => {
  it('shows the amber notice on a completed turn with stopReason max_iterations', () => {
    render(<AssistantTurnView turn={turn({ stopReason: 'max_iterations' })} />);
    const notice = screen.getByTestId('max-iterations-notice');
    expect(notice).toHaveTextContent('Joe reached its step limit for this task.');
    // The real answer still renders — this is a completed turn, not a failure.
    expect(screen.getByText('Here is what I found.')).toBeInTheDocument();
  });

  it('renders no truncation notice for a normally-completed turn', () => {
    render(<AssistantTurnView turn={turn({})} />);
    expect(screen.queryByTestId('max-iterations-notice')).not.toBeInTheDocument();
  });

  it('does not show the truncation notice on a failed turn', () => {
    render(
      <AssistantTurnView
        turn={turn({ status: 'failed', stopReason: 'max_iterations', failureLabel: 'Stopped' })}
      />
    );
    expect(screen.queryByTestId('max-iterations-notice')).not.toBeInTheDocument();
  });
});

// A turn the user stopped in flight carries stop_reason 'cancelled' and renders
// an amber "stopped" notice (not the red failure banner). Its answer bubble is
// suppressed — the marker content is not shown — and a resend affordance appears
// when the parent wires onResend.
describe('AssistantTurnView cancelled notice', () => {
  it('shows the cancelled notice and suppresses the answer bubble', () => {
    render(
      <AssistantTurnView turn={turn({ stopReason: 'cancelled', finalAnswer: 'marker text' })} />
    );
    expect(screen.getByTestId('cancelled-notice')).toBeInTheDocument();
    // The (minimal marker) answer bubble is not shown for a cancelled turn.
    expect(screen.queryByText('marker text')).not.toBeInTheDocument();
    // Not a failure — no red failure banner text.
    expect(screen.queryByText('Something went wrong')).not.toBeInTheDocument();
  });

  it('offers a resend affordance on a cancelled turn when onResend is provided', async () => {
    const onResend = vi.fn();
    render(<AssistantTurnView turn={turn({ stopReason: 'cancelled' })} onResend={onResend} />);
    await userEvent.click(screen.getByTestId('resend-button'));
    expect(onResend).toHaveBeenCalledTimes(1);
  });

  it('offers a resend affordance on a failed turn when onResend is provided', async () => {
    const onResend = vi.fn();
    render(
      <AssistantTurnView
        turn={turn({ status: 'failed', failureLabel: 'Something went wrong', finalAnswer: '' })}
        onResend={onResend}
      />
    );
    await userEvent.click(screen.getByTestId('resend-button'));
    expect(onResend).toHaveBeenCalledTimes(1);
  });

  it('shows no resend affordance on a normally-completed turn', () => {
    const onResend = vi.fn();
    render(<AssistantTurnView turn={turn({})} onResend={onResend} />);
    expect(screen.queryByTestId('resend-button')).not.toBeInTheDocument();
  });
});

describe('AssistantTurnView context-utilization badge', () => {
  it('renders input tokens against the context window with a percentage', () => {
    render(<AssistantTurnView turn={turn({ inputTokens: 30000, contextWindow: 200000 })} />);
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent(
      '30,000 of 200,000 tokens · 15% of context'
    );
  });

  it('falls back to a bare input-token count when the window is unknown (0)', () => {
    render(<AssistantTurnView turn={turn({ inputTokens: 1234, contextWindow: 0 })} />);
    const badge = screen.getByTestId('turn-tokens');
    expect(badge).toHaveTextContent('1,234 tokens');
    expect(badge).not.toHaveTextContent('of context');
  });
});

describe('AssistantTurnView answer-echo de-duplication', () => {
  it('renders the final answer once when the terminal step echoes it', () => {
    // The backend emits a final step whose content it also returns as the
    // final answer (single-step, no-tool turn). The italic step line must be
    // suppressed so the text shows only in the answer bubble.
    render(
      <AssistantTurnView
        turn={turn({
          finalAnswer: 'I can help you with a variety of tasks.',
          steps: [
            { stepNumber: 1, content: 'I can help you with a variety of tasks.', toolCalls: [] },
          ],
        })}
      />
    );
    expect(screen.getAllByText('I can help you with a variety of tasks.')).toHaveLength(1);
  });

  it('keeps a reasoning line that precedes tool calls', () => {
    render(
      <AssistantTurnView
        turn={turn({
          finalAnswer: 'Done.',
          steps: [
            {
              stepNumber: 1,
              content: 'Let me check the logs.',
              toolCalls: [{ id: 't1', name: 'logs', args: {}, done: true, result: 'ok' }],
            },
          ],
        })}
      />
    );
    expect(screen.getByText('Let me check the logs.')).toBeInTheDocument();
  });
});
