import { describe, it, expect, vi, beforeEach } from 'vitest';
import { act } from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createWrapper } from '@/test/utils';
import { useChat } from './useChat';
import { ChatWindow } from '@/components/chat/ChatWindow';
import { streamTask } from '@/api/taskStream';
import type { StepEvent, FinalEvent, StreamHandlers } from '@/api/taskStream';
import { fetchMessages } from '@/api/chat';
import type { ChatMessage } from '@/api/types';

// useChat drives the streaming lifecycle; the network is mocked at the
// streamTask + session-api boundary so these tests assert the React-state and
// rendering behavior (steps, token counter, failure handling, per-turn reset).
vi.mock('@/api/taskStream', () => ({ streamTask: vi.fn() }));
vi.mock('@/api/chat', () => ({
  createSession: vi.fn(() => Promise.resolve({ id: 's-test', started_at: '', message_count: 0 })),
  fetchMessages: vi.fn(() => Promise.resolve([])),
}));

const streamTaskMock = vi.mocked(streamTask);

// Each streamTask call captures its handlers and a resolver, so a test can
// drive events for turn N and then settle that turn's promise.
let captured: StreamHandlers[] = [];
let resolvers: (() => void)[] = [];

function installStreamMock() {
  captured = [];
  resolvers = [];
  streamTaskMock.mockImplementation((_body, handlers) => {
    captured.push(handlers);
    return new Promise<void>((res) => resolvers.push(res));
  });
}

function makeStep(
  stepNumber: number,
  opts: { input: number; output: number; tool?: string }
): StepEvent {
  return {
    step_number: stepNumber,
    llm_request: { message_count: stepNumber, tools_available: [] },
    llm_response: {
      content: `reasoning ${stepNumber}`,
      tool_calls: opts.tool ? [{ id: `tc-${stepNumber}`, name: opts.tool, args: {} }] : [],
      usage: { input_tokens: opts.input, output_tokens: opts.output },
    },
    tool_results: opts.tool
      ? [{ id: `tc-${stepNumber}`, name: opts.tool, result: 'ok', duration_ms: 1 }]
      : [],
  };
}

function makeFinal(over: Partial<FinalEvent>): FinalEvent {
  return {
    task_id: 't1',
    session_id: 's-test',
    status: 'completed',
    iterations: 1,
    steps: [],
    final_answer: 'done',
    tools_used: [],
    total_tokens: { input_tokens: 0, output_tokens: 0 },
    duration_ms: 1,
    history_trimmed: false,
    messages_dropped: 0,
    tool_results_truncated: 0,
    user_message_truncated: false,
    context_window_tokens: 0,
    ...over,
  };
}

function Harness({ initialSessionId }: { initialSessionId?: string } = {}) {
  const chat = useChat(initialSessionId);
  return (
    <ChatWindow
      items={chat.messages}
      isSending={chat.isSending}
      onSend={(m) => void chat.send(m)}
    />
  );
}

async function sendMessage(text: string) {
  const before = streamTaskMock.mock.calls.length;
  await userEvent.type(screen.getByRole('textbox'), `${text}{Enter}`);
  await waitFor(() => expect(streamTaskMock.mock.calls.length).toBe(before + 1));
}

describe('useChat streaming lifecycle', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    installStreamMock();
  });

  it('renders a multi-step turn: final answer, tool calls from both steps, and an input-token counter that sums steps and matches the final total', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('investigate');
    expect(screen.getByText('investigate')).toBeInTheDocument(); // optimistic user msg

    // Step 1 lands: input 10 (the badge counts input only — the figure that
    // fills the context window), tool "k8s".
    act(() => captured[0].onStep(makeStep(1, { input: 10, output: 5, tool: 'k8s' })));
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent('10 tokens');
    expect(screen.getByText('k8s')).toBeInTheDocument();

    // Step 2 lands: +20 input → running 30, tool "metrics".
    act(() => captured[0].onStep(makeStep(2, { input: 20, output: 10, tool: 'metrics' })));
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent('30 tokens');
    expect(screen.getByText('metrics')).toBeInTheDocument();

    // Final: authoritative input total 30 (matches the running input sum). No
    // context window on this event, so the badge stays a bare input count.
    act(() => {
      captured[0].onFinal(
        makeFinal({
          final_answer: 'the answer',
          total_tokens: { input_tokens: 30, output_tokens: 15 },
        })
      );
      resolvers[0]();
    });

    await waitFor(() => expect(screen.getByText('the answer')).toBeInTheDocument());
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent('30 tokens');
    // Both steps' tools remain visible in the settled turn.
    expect(screen.getByText('k8s')).toBeInTheDocument();
    expect(screen.getByText('metrics')).toBeInTheDocument();
  });

  it('renders the context-utilization figure (X of Y) when the final carries a context window', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('investigate');
    act(() => {
      captured[0].onFinal(
        makeFinal({
          final_answer: 'the answer',
          total_tokens: { input_tokens: 50000, output_tokens: 15 },
          context_window_tokens: 200000,
        })
      );
      resolvers[0]();
    });

    await waitFor(() => expect(screen.getByText('the answer')).toBeInTheDocument());
    // Input X against window Y, with the fill percentage (50000 / 200000 = 25%).
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent(
      '50,000 of 200,000 tokens · 25% of context'
    );
  });

  it('renders the history-trimmed notice when the final reports history_trimmed', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('long conversation');
    act(() => {
      captured[0].onFinal(
        makeFinal({ final_answer: 'ok', history_trimmed: true, messages_dropped: 3 })
      );
      resolvers[0]();
    });
    await waitFor(() => expect(screen.getByTestId('history-trimmed-notice')).toBeInTheDocument());

    // A turn that did not trim shows no notice.
    await sendMessage('short');
    act(() => {
      captured[1].onFinal(makeFinal({ final_answer: 'ok2' }));
      resolvers[1]();
    });
    await waitFor(() => expect(screen.getByText('ok2')).toBeInTheDocument());
    expect(screen.getAllByTestId('history-trimmed-notice')).toHaveLength(1);
  });

  it('renders the user-message-truncated notice when the final reports it, and not otherwise', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('a very long message');
    act(() => {
      captured[0].onFinal(makeFinal({ final_answer: 'ok', user_message_truncated: true }));
      resolvers[0]();
    });
    await waitFor(() =>
      expect(screen.getByTestId('user-message-truncated-notice')).toBeInTheDocument()
    );

    // A turn whose message was not truncated shows no notice.
    await sendMessage('short');
    act(() => {
      captured[1].onFinal(makeFinal({ final_answer: 'ok2' }));
      resolvers[1]();
    });
    await waitFor(() => expect(screen.getByText('ok2')).toBeInTheDocument());
    expect(screen.getAllByTestId('user-message-truncated-notice')).toHaveLength(1);
  });

  it('renders the context_overflow status with its label and friendly message', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('huge');
    act(() => {
      captured[0].onFinal(
        makeFinal({
          status: 'context_overflow',
          error: 'The conversation or a tool output was too large for the model’s context window.',
          final_answer: '',
        })
      );
      resolvers[0]();
    });

    await waitFor(() =>
      expect(screen.getByText('Stopped — too large for the context window')).toBeInTheDocument()
    );
    expect(screen.getByText(/too large for the model/i)).toBeInTheDocument();
  });

  it('renders a pre-stream non-200 error as a failed turn with no steps', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('bad');
    act(() => {
      captured[0].onError('message is required', true);
      resolvers[0]();
    });

    await waitFor(() => expect(screen.getByText('message is required')).toBeInTheDocument());
    expect(screen.getByText('Request rejected')).toBeInTheDocument();
    // No steps → no tool activity and no token badge.
    expect(screen.queryByTestId('turn-tokens')).not.toBeInTheDocument();
  });

  it('renders an in-stream final with status=error as a failed turn surfacing final.error', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('go');
    act(() => {
      captured[0].onFinal(makeFinal({ status: 'error', error: 'llm exploded', final_answer: '' }));
      resolvers[0]();
    });

    await waitFor(() => expect(screen.getByText('llm exploded')).toBeInTheDocument());
    expect(screen.getByText('Something went wrong')).toBeInTheDocument();
  });

  it('renders a timeout final with its human label', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    await sendMessage('slow');
    act(() => {
      captured[0].onFinal(
        makeFinal({ status: 'timeout', error: 'task timed out', final_answer: '' })
      );
      resolvers[0]();
    });

    await waitFor(() => expect(screen.getByText('Timed out')).toBeInTheDocument());
    expect(screen.getByText('task timed out')).toBeInTheDocument();
  });

  it('does not mask a reopened session: a session created mid-mount must not cache an empty transcript', async () => {
    // Regression: creating a session in the fresh-/chat flow used to fetch its
    // (still-empty) message list the instant its id was set and freeze that []
    // under staleTime: Infinity. Reopening the session later from the route then
    // hit the poisoned cache and rendered a blank chat. The fix: only fetch
    // history for a session the route handed us. A single shared QueryClient
    // across both renders reproduces the cross-mount cache.
    const fetchMessagesMock = vi.mocked(fetchMessages);
    const { Wrapper } = createWrapper();

    // 1) Fresh /chat: no route session id. Send a message; createSession lazily
    //    mints 's-test'. fetchMessages must NOT be called for it.
    const first = render(<Harness />, { wrapper: Wrapper });
    await sendMessage('hello');
    act(() => {
      captured[0].onFinal(makeFinal({ final_answer: 'streamed answer' }));
      resolvers[0]();
    });
    await waitFor(() => expect(screen.getByText('streamed answer')).toBeInTheDocument());
    expect(fetchMessagesMock).not.toHaveBeenCalled();
    first.unmount();

    // 2) The turn is now persisted server-side. Reopen 's-test' from the route.
    const persisted: ChatMessage[] = [
      {
        id: 1,
        session_id: 's-test',
        role: 'user',
        content: 'hello',
        created_at: '2026-06-06T00:00:00Z',
      },
      {
        id: 2,
        session_id: 's-test',
        role: 'assistant',
        content: 'persisted answer',
        created_at: '2026-06-06T00:00:01Z',
      },
    ];
    fetchMessagesMock.mockResolvedValueOnce(persisted);
    render(<Harness initialSessionId="s-test" />, { wrapper: Wrapper });

    // The persisted transcript loads instead of a blank chat.
    await waitFor(() => expect(screen.getByText('persisted answer')).toBeInTheDocument());
    expect(fetchMessagesMock).toHaveBeenCalledWith('s-test');
  });

  it('reopening a route session re-reads the server instead of serving a stale cached transcript', async () => {
    // Regression: a turn that streamed into liveItems on one mount and was then
    // dropped when that mount unmounted (the user navigated away mid-stream)
    // stayed invisible on reopen, because staleTime: Infinity froze the prior
    // mount's cached snapshot and reopening via in-app routing never refetched.
    // It only reappeared after a hard reload wiped the cache — even though it
    // persisted server-side. refetchOnMount: 'always' re-reads on every reopen.
    // A single shared QueryClient across both renders reproduces the cache.
    const fetchMessagesMock = vi.mocked(fetchMessages);
    const { Wrapper } = createWrapper();

    const firstSnapshot: ChatMessage[] = [
      {
        id: 1,
        session_id: 's-existing',
        role: 'user',
        content: 'hi',
        created_at: '2026-06-06T00:00:00Z',
      },
      {
        id: 2,
        session_id: 's-existing',
        role: 'assistant',
        content: 'first reply',
        created_at: '2026-06-06T00:00:01Z',
      },
    ];
    // The server later holds two more rows: a turn that streamed on the first
    // mount, was lost when it unmounted, but persisted server-side.
    const secondSnapshot: ChatMessage[] = [
      ...firstSnapshot,
      {
        id: 3,
        session_id: 's-existing',
        role: 'user',
        content: 'second message',
        created_at: '2026-06-06T00:00:02Z',
      },
      {
        id: 4,
        session_id: 's-existing',
        role: 'assistant',
        content: 'second reply',
        created_at: '2026-06-06T00:00:03Z',
      },
    ];
    fetchMessagesMock.mockResolvedValueOnce(firstSnapshot);
    fetchMessagesMock.mockResolvedValueOnce(secondSnapshot);

    // 1) Open the existing session from the route; its transcript loads and the
    //    query caches under ['messages', 's-existing'].
    const first = render(<Harness initialSessionId="s-existing" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText('first reply')).toBeInTheDocument());
    first.unmount();

    // 2) Reopen the same session from the route against the shared client. The
    //    stale cache holds only the first snapshot; refetchOnMount forces a
    //    re-read, so the turn persisted while we were away now renders.
    render(<Harness initialSessionId="s-existing" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText('second reply')).toBeInTheDocument());
    expect(fetchMessagesMock).toHaveBeenCalledTimes(2);
  });

  it('resets the per-turn input-token counter to 0 at the start of a second message', async () => {
    const { Wrapper } = createWrapper();
    render(<Harness />, { wrapper: Wrapper });

    // First turn accumulates input 10 and completes.
    await sendMessage('first');
    act(() => captured[0].onStep(makeStep(1, { input: 10, output: 5 })));
    act(() => {
      captured[0].onFinal(
        makeFinal({
          final_answer: 'first answer',
          total_tokens: { input_tokens: 10, output_tokens: 5 },
        })
      );
      resolvers[0]();
    });
    await waitFor(() => expect(screen.getByText('first answer')).toBeInTheDocument());
    expect(screen.getByTestId('turn-tokens')).toHaveTextContent('10 tokens');

    // Second turn: a fresh counter starts at 0 before any step accumulates.
    await sendMessage('second');
    let badges = screen.getAllByTestId('turn-tokens');
    expect(badges).toHaveLength(2);
    expect(badges[1]).toHaveTextContent('0 tokens');

    // Then it accumulates independently of the first turn (input only).
    act(() => captured[1].onStep(makeStep(1, { input: 4, output: 3 })));
    badges = screen.getAllByTestId('turn-tokens');
    expect(badges[0]).toHaveTextContent('10 tokens'); // first turn unchanged
    expect(badges[1]).toHaveTextContent('4 tokens');
  });
});
