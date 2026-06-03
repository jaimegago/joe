import { useState, useCallback, useMemo, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchMessages, createSession } from '@/api/chat';
import { streamTask } from '@/api/taskStream';
import type { StepEvent, TaskStatus } from '@/api/taskStream';
import type { ChatMessage } from '@/api/types';

// --- Display model ------------------------------------------------------
//
// The transcript is rendered from a list of DisplayItems. Persisted history
// (loaded once when a session is opened) and the live turns produced this
// mount are both projected into this single shape so the view renders them
// uniformly. A live assistant turn evolves in React state as `step` events
// arrive and settles on the `final` event.

// TurnToolCall is one tool invocation within an agentic step, joined with its
// result (results arrive in the same step event that issued the call).
export interface TurnToolCall {
  id: string;
  name: string;
  args: Record<string, unknown>;
  result?: string;
  error?: string;
  done: boolean;
}

export interface TurnStep {
  stepNumber: number;
  content: string;
  toolCalls: TurnToolCall[];
}

export type TurnStatus = 'streaming' | 'completed' | 'failed';

// AssistantTurn is the evolving agentic response: the steps observed so far,
// the final answer once available, a per-turn token counter, and — when the
// turn did not complete — a human failure label plus the raw error message.
export interface AssistantTurn {
  steps: TurnStep[];
  finalAnswer: string;
  status: TurnStatus;
  failureLabel?: string;
  errorMessage?: string;
  tokens: number;
}

export type DisplayItem =
  | { kind: 'user'; id: string; content: string; createdAt: string }
  | { kind: 'assistant'; id: string; turn: AssistantTurn };

// Human labels for the six terminal statuses. `completed` never surfaces a
// label (it renders the answer); the rest render as failed turns.
const STATUS_LABELS: Record<TaskStatus, string> = {
  completed: 'Completed',
  timeout: 'Timed out',
  max_iterations_reached: 'Stopped — reached the step limit',
  runaway_terminated: 'Stopped — runaway protection tripped',
  cost_limit_exceeded: 'Stopped — cost limit reached',
  error: 'Something went wrong',
};

function stringifyResult(result: unknown): string | undefined {
  if (result === undefined || result === null) return undefined;
  if (typeof result === 'string') return result;
  try {
    return JSON.stringify(result, null, 2);
  } catch {
    return '[unserializable result]';
  }
}

// stepToTurnStep projects a wire step event into the display shape, joining
// each tool call with its matching result by id.
function stepToTurnStep(step: StepEvent): TurnStep {
  const toolCalls: TurnToolCall[] = step.llm_response.tool_calls.map((tc) => {
    const res = step.tool_results.find((r) => r.id === tc.id);
    return {
      id: tc.id,
      name: tc.name,
      args: tc.args,
      result: res ? stringifyResult(res.result) : undefined,
      error: res?.error,
      done: res != null,
    };
  });
  return { stepNumber: step.step_number, content: step.llm_response.content, toolCalls };
}

// historyToItems projects persisted session messages into DisplayItems.
// Persisted assistant messages only carry the final answer text (the store
// does not keep step detail), so historical turns render the answer alone.
function historyToItems(messages: ChatMessage[]): DisplayItem[] {
  return messages.map((m): DisplayItem => {
    if (m.role === 'user') {
      return { kind: 'user', id: `h-${m.id}`, content: m.content, createdAt: m.created_at };
    }
    return {
      kind: 'assistant',
      id: `h-${m.id}`,
      turn: { steps: [], finalAnswer: m.content, status: 'completed', tokens: 0 },
    };
  });
}

export function useChat(initialSessionId?: string) {
  const qc = useQueryClient();
  const [sessionId, setSessionId] = useState<string | null>(initialSessionId ?? null);
  const [isSending, setIsSending] = useState(false);
  // Live turns produced this mount: optimistic user messages and the evolving
  // assistant turns. The persisted store remains the source of truth for prior
  // history; live turns are the source of truth for the current turn.
  const [liveItems, setLiveItems] = useState<DisplayItem[]>([]);
  // Monotonic counter for unique live-turn ids (avoids Date.now collisions
  // when two turns start in the same millisecond).
  const seqRef = useRef(0);

  // Persisted history is loaded ONCE per session (staleTime: Infinity, no
  // window-focus refetch) and never invalidated. Because it is frozen at
  // session-open, it can never overlap the live turns appended afterwards —
  // this is how we avoid the streamed turn duplicating its persisted copy
  // without a post-completion message-list refetch.
  const messagesQ = useQuery({
    queryKey: ['messages', sessionId],
    queryFn: () => fetchMessages(sessionId!),
    enabled: sessionId != null,
    staleTime: Infinity,
    refetchOnWindowFocus: false,
  });

  // updateTurn applies fn to the assistant turn with the given id, in place.
  const updateTurn = useCallback((id: string, fn: (turn: AssistantTurn) => AssistantTurn) => {
    setLiveItems((prev) =>
      prev.map((it) => (it.kind === 'assistant' && it.id === id ? { kind: 'assistant', id, turn: fn(it.turn) } : it)),
    );
  }, []);

  const send = useCallback(
    async (content: string) => {
      setIsSending(true);

      const seq = seqRef.current++;
      const userId = `u-${seq}`;
      const turnId = `a-${seq}`;

      // Optimistic user message + a fresh streaming assistant turn. The token
      // counter starts at 0 for every new turn (per-turn, not cumulative).
      setLiveItems((prev) => [
        ...prev,
        { kind: 'user', id: userId, content, createdAt: new Date().toISOString() },
        {
          kind: 'assistant',
          id: turnId,
          turn: { steps: [], finalAnswer: '', status: 'streaming', tokens: 0 },
        },
      ]);

      // Resolve (or lazily create) the session id.
      let sid = sessionId;
      if (!sid) {
        try {
          const session = await createSession();
          setSessionId(session.id);
          sid = session.id;
        } catch {
          updateTurn(turnId, (t) => ({
            ...t,
            status: 'failed',
            failureLabel: 'Could not start a session',
            errorMessage: 'Could not reach the Joe server. Make sure it is running and try again.',
          }));
          setIsSending(false);
          return;
        }
      }

      await streamTask(
        { session_id: sid, message: content },
        {
          onStep: (step) =>
            updateTurn(turnId, (t) => ({
              ...t,
              steps: [...t.steps, stepToTurnStep(step)],
              // Running sum of per-step usage while the turn streams.
              tokens: t.tokens + step.llm_response.usage.input_tokens + step.llm_response.usage.output_tokens,
            })),
          onFinal: (final) =>
            updateTurn(turnId, (t) => ({
              ...t,
              finalAnswer: final.final_answer,
              status: final.status === 'completed' ? 'completed' : 'failed',
              failureLabel: final.status === 'completed' ? undefined : STATUS_LABELS[final.status],
              errorMessage: final.status === 'completed' ? undefined : final.error ?? STATUS_LABELS[final.status],
              // Snap the counter to the authoritative server total.
              tokens: final.total_tokens.input_tokens + final.total_tokens.output_tokens,
            })),
          onError: (message, preStream) =>
            updateTurn(turnId, (t) => ({
              ...t,
              status: 'failed',
              failureLabel: preStream ? 'Request rejected' : 'Connection lost',
              errorMessage: message,
            })),
        },
      );

      setIsSending(false);
    },
    [sessionId, updateTurn],
  );

  const startNewSession = useCallback(() => {
    setSessionId(null);
    setLiveItems([]);
    void qc.removeQueries({ queryKey: ['messages'] });
  }, [qc]);

  const messages = useMemo<DisplayItem[]>(() => {
    const history = historyToItems(messagesQ.data ?? []);
    return [...history, ...liveItems];
  }, [messagesQ.data, liveItems]);

  return {
    sessionId,
    messages,
    isLoading: messagesQ.isLoading,
    isSending,
    send,
    startNewSession,
  };
}
