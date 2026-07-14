import { useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { fetchMessages, createSession } from '@/api/chat';
import { streamTask } from '@/api/taskStream';
import type { StepEvent, TaskStatus } from '@/api/taskStream';
import { QUERY_KEYS } from '@/lib/queryKeys';
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
  // tokens is the per-turn running sum of input+output usage, snapped to the
  // authoritative server total on the final event. Retained as the underlying
  // counter (its accumulate/snap behavior is unchanged); the badge now renders
  // the context-utilization figure from inputTokens/contextWindow instead.
  tokens: number;
  // inputTokens is the per-turn running sum of INPUT usage only — the figure
  // that actually fills the context window — snapped to the server's
  // authoritative input total on the final event. It is the numerator X of the
  // context-utilization badge (X of Y). Same accumulate-then-snap mechanism as
  // tokens, applied to the input dimension alone.
  inputTokens: number;
  // contextWindow is the active model's total context-window capacity in tokens
  // (denominator Y of the utilization badge), carried on the final event from
  // the server's capabilities registry. 0 until the final event lands (and when
  // the server omits it); the view falls back to a bare input-token count then.
  contextWindow: number;
  // historyTrimmed is true when this turn's history pruning dropped earlier
  // messages from the model's context (token budget or count backstop). The
  // view renders a single unobtrusive notice when it is set.
  historyTrimmed?: boolean;
  // userMessageTruncated is true when this turn's incoming user message was
  // shortened at ingestion to fit its share of the context budget. The view
  // renders a single unobtrusive notice when it is set.
  userMessageTruncated?: boolean;
  // writeFailureCode is the backend's typed reason a write was denied this
  // turn ('zone_denial' | 'incident_mode' | 'safe_mode' | 'observation' | 'internal_error'). A denied write
  // does NOT fail the turn (the LLM still answers), so the view renders a
  // dedicated notice — distinct from a generic failure — explaining why.
  writeFailureCode?: string;
  // stopReason marks a COMPLETED turn that did not end on a naturally
  // tool-call-free answer (currently only 'max_iterations': the loop hit its
  // step budget and the answer was synthesized from evidence in hand). The view
  // renders an amber truncation notice when it is set — distinct from the red
  // failure banner. Set on both a live turn (final event) and a reloaded one
  // (persisted marker).
  stopReason?: string;
}

// writeFailureMessage maps a backend write-failure code to the user-facing
// sentence shown in chat (Item 8). Returns undefined for an unknown/absent
// code so callers fall back to the generic error text. Kept pure and exported
// so the dispatch is unit-tested independently of the streaming machinery.
export function writeFailureMessage(code: string | undefined): string | undefined {
  switch (code) {
    case 'zone_denial':
      return 'Access to this zone has not been granted to you. Ask your administrator.';
    case 'incident_mode':
      return 'System is in incident mode. Writes are temporarily blocked.';
    case 'safe_mode':
      return 'System is in safe mode. Only read-only operations are permitted — run `joe unlock` to resume writes.';
    case 'observation':
      return 'Joe is in observation mode — it can read and explain but will not make changes. This is the intended read-only posture.';
    case 'internal_error':
      return 'Unexpected error. Please try again.';
    default:
      return undefined;
  }
}

export type DisplayItem =
  | { kind: 'user'; id: string; content: string; createdAt: string }
  | { kind: 'assistant'; id: string; turn: AssistantTurn };

// Human labels for the seven terminal statuses. `completed` never surfaces a
// label (it renders the answer); the rest render as failed turns.
const STATUS_LABELS: Record<TaskStatus, string> = {
  completed: 'Completed',
  timeout: 'Timed out',
  max_iterations_reached: 'Stopped — reached the step limit',
  runaway_terminated: 'Stopped — runaway protection tripped',
  cost_limit_exceeded: 'Stopped — cost limit reached',
  context_overflow: 'Stopped — too large for the context window',
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
      turn: {
        steps: [],
        finalAnswer: m.content,
        status: 'completed',
        tokens: 0,
        inputTokens: 0,
        contextWindow: 0,
        // Carry the persisted truncation marker so a reloaded max-iterations
        // turn shows the same amber notice the live turn did.
        stopReason: m.stop_reason,
      },
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
  // The in-flight stream's AbortController plus the session it belongs to.
  // Switching sessions or unmounting aborts it, so an abandoned turn's request
  // is cancelled and its callbacks never fire for the session the user moved to.
  // Null when nothing streams.
  const abortRef = useRef<{ controller: AbortController; session: string | null } | null>(null);
  // abortInFlight cancels the current stream (if any) and clears the ref. Used
  // by the session-switch effect and the unmount cleanup. It does NOT touch
  // isSending — the callers own that, so they can reset it in the right order.
  const abortInFlight = useCallback(() => {
    abortRef.current?.controller.abort();
    abortRef.current = null;
  }, []);
  // The id minted by this mount's own createSession() call (see send). History
  // is never fetched for it: its transcript lives in liveItems, and the server
  // only persists it as the turn completes, so a fetch would cache an empty list
  // that staleTime: Infinity would later freeze in place of the real transcript
  // when the session is reopened. Held as state (not a ref) because `enabled`
  // reads it during render; it survives the URL being synced to the new id.
  const [locallyCreatedId, setLocallyCreatedId] = useState<string | null>(null);

  // The id the route handed us (null on a fresh /chat). When it changes to a
  // *different* session — the user opened another session without this component
  // remounting — reset the view to it and drop the previous mount's live turns.
  // When the URL is merely updated to match a session we just created locally
  // (routeSessionId === sessionId), keep everything: it is the same session, now
  // reflected in the address bar.
  const routeSessionId = initialSessionId ?? null;
  const [prevRouteId, setPrevRouteId] = useState(routeSessionId);
  if (routeSessionId !== prevRouteId) {
    setPrevRouteId(routeSessionId);
    if (routeSessionId !== sessionId) {
      // Switching to a different session mid-stream: re-enable the composer
      // immediately and drop the previous mount's live turns. The abandoned
      // stream is aborted by the sessionId effect below once this commits.
      setIsSending(false);
      setSessionId(routeSessionId);
      setLiveItems([]);
      setLocallyCreatedId(null);
    }
  }

  // Abort any in-flight stream that belongs to a session other than the one now
  // in view — i.e. the user switched sessions mid-stream. The request is
  // cancelled and its callbacks can't mutate state for the new session
  // (streamTask returns silently once aborted). Keyed on sessionId so it runs
  // exactly when the in-view session changes, ref access kept out of render.
  useEffect(() => {
    if (abortRef.current && abortRef.current.session !== sessionId) {
      abortInFlight();
    }
  }, [sessionId, abortInFlight]);
  // Abort on unmount so a turn abandoned by navigating away does not keep a
  // dangling reader/request alive after the component is gone.
  useEffect(() => {
    return () => abortInFlight();
  }, [abortInFlight]);

  // Persisted history is loaded once per MOUNT and frozen for that mount's
  // lifetime (staleTime: Infinity, no window-focus refetch). Because it is
  // frozen at session-open, it can never overlap the live turns appended
  // afterwards — this is how we avoid the streamed turn duplicating its
  // persisted copy without a post-completion message-list refetch.
  //
  // refetchOnMount: 'always' is the one deliberate exception: it re-reads the
  // server every time the session is (re)opened, ignoring the cached snapshot a
  // prior mount left behind. Without it, reopening a session via in-app routing
  // serves stale cached history — so a turn that streamed into liveItems on an
  // earlier mount and was then dropped when that mount unmounted (e.g. the user
  // navigated away mid-stream) stays invisible until a hard reload wipes the
  // cache, even though it persisted server-side. The refetch is mount-scoped, so
  // it never fires mid-mount and thus never duplicates a currently-live turn:
  // liveItems do not survive a remount, so the refreshed history can't overlap
  // them.
  //
  // We fetch history for any session that existed before this mount — never for
  // one created locally this mount (see locallyCreatedId above).
  const messagesQ = useQuery({
    queryKey: ['messages', sessionId],
    queryFn: () => fetchMessages(sessionId!),
    enabled: sessionId != null && sessionId !== locallyCreatedId,
    staleTime: Infinity,
    refetchOnMount: 'always',
    refetchOnWindowFocus: false,
  });

  // updateTurn applies fn to the assistant turn with the given id, in place.
  const updateTurn = useCallback((id: string, fn: (turn: AssistantTurn) => AssistantTurn) => {
    setLiveItems((prev) =>
      prev.map((it) =>
        it.kind === 'assistant' && it.id === id ? { kind: 'assistant', id, turn: fn(it.turn) } : it
      )
    );
  }, []);

  const send = useCallback(
    async (content: string) => {
      setIsSending(true);

      // A fresh AbortController for this turn. If the user switches sessions or
      // unmounts before the stream ends, abortInFlight() aborts this controller
      // and streamTask returns silently, so none of the callbacks below fire. It
      // is tagged with the session it belongs to (filled in once resolved below)
      // so the sessionId effect can tell an abandoned turn from the current one.
      const controller = new AbortController();

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
          turn: {
            steps: [],
            finalAnswer: '',
            status: 'streaming',
            tokens: 0,
            inputTokens: 0,
            contextWindow: 0,
          },
        },
      ]);

      // Resolve (or lazily create) the session id.
      let sid = sessionId;
      if (!sid) {
        try {
          const session = await createSession();
          setLocallyCreatedId(session.id);
          setSessionId(session.id);
          sid = session.id;
        } catch {
          // A caller-initiated abort during creation means this turn was
          // discarded — leave state alone (the switch already reset it).
          if (controller.signal.aborted) return;
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

      // Register this turn's controller against its resolved session so the
      // session-switch effect can abort it if the user navigates away.
      abortRef.current = { controller, session: sid };

      await streamTask(
        { session_id: sid, message: content },
        {
          onStep: (step) =>
            updateTurn(turnId, (t) => ({
              ...t,
              steps: [...t.steps, stepToTurnStep(step)],
              // Running sum of per-step usage while the turn streams.
              tokens:
                t.tokens +
                step.llm_response.usage.input_tokens +
                step.llm_response.usage.output_tokens,
              // Running sum of INPUT-only usage — the badge's numerator X.
              inputTokens: t.inputTokens + step.llm_response.usage.input_tokens,
            })),
          onFinal: (final) =>
            updateTurn(turnId, (t) => {
              const failed = final.status !== 'completed';
              // A typed write-failure code yields a specific message; it wins
              // over the raw server error text on a failed turn, and drives a
              // dedicated notice on an otherwise-completed turn (a denied write
              // does not fail the turn — the LLM still answers).
              const specific = writeFailureMessage(final.error_code);
              return {
                ...t,
                finalAnswer: final.final_answer,
                status: failed ? 'failed' : 'completed',
                failureLabel: failed ? STATUS_LABELS[final.status] : undefined,
                errorMessage: failed
                  ? (specific ?? final.error ?? STATUS_LABELS[final.status])
                  : undefined,
                // Snap the counters to the authoritative server totals.
                tokens: final.total_tokens.input_tokens + final.total_tokens.output_tokens,
                inputTokens: final.total_tokens.input_tokens,
                // Denominator Y of the context-utilization badge, from the
                // server's capabilities registry (0 when the server omits it).
                contextWindow: final.context_window_tokens,
                historyTrimmed: final.history_trimmed,
                userMessageTruncated: final.user_message_truncated,
                writeFailureCode: final.error_code,
                stopReason: final.stop_reason,
              };
            }),
          onError: (message, preStream, code) =>
            updateTurn(turnId, (t) => ({
              ...t,
              status: 'failed',
              failureLabel: preStream ? 'Request rejected' : 'Connection lost',
              // A typed code (e.g. a pre-stream 403 carrying zone_denial /
              // incident_mode) maps to the specific message; otherwise the raw
              // server/transport message is shown.
              errorMessage: writeFailureMessage(code) ?? message,
            })),
        },
        controller.signal
      );

      // This stream was aborted (the user switched sessions or unmounted): the
      // switch/unmount handler already reset isSending and cleared the view, so
      // do not touch state or invalidate queries for the abandoned turn.
      if (controller.signal.aborted) return;

      // The first message of a fresh session titles it server-side (an async
      // LLM call that lands shortly after this turn). Refresh the session-list
      // and detail queries — otherwise frozen — so the refreshed last-activity
      // appears now; ChatPage polls the detail query separately until the async
      // title lands and swaps out the "New chat" placeholder.
      void qc.invalidateQueries({ queryKey: QUERY_KEYS.sessions });
      if (sid) void qc.invalidateQueries({ queryKey: [...QUERY_KEYS.session, sid] });

      // This turn's controller is settled; clear the ref so a later switch does
      // not abort an already-finished controller.
      if (abortRef.current?.controller === controller) abortRef.current = null;
      setIsSending(false);
    },
    [sessionId, updateTurn, qc]
  );

  const startNewSession = useCallback(() => {
    setSessionId(null);
    setLiveItems([]);
    setLocallyCreatedId(null);
    void qc.removeQueries({ queryKey: ['messages'] });
  }, [qc]);

  const messages = useMemo<DisplayItem[]>(() => {
    const history = historyToItems(messagesQ.data ?? []);
    return [...history, ...liveItems];
  }, [messagesQ.data, liveItems]);

  return {
    sessionId,
    // The id this mount minted via createSession (null otherwise). Exposed so the
    // session-metadata layer can treat a transient read-after-write 404 on a
    // just-created session as benign rather than a dead/inaccessible session.
    locallyCreatedId,
    messages,
    isLoading: messagesQ.isLoading,
    isSending,
    send,
    startNewSession,
  };
}
