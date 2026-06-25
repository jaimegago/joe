import { AlertTriangle } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import { ToolCallDisplay } from './ToolCallDisplay';
import { Markdown } from './Markdown';
import { writeFailureMessage } from '@/hooks/useChat';
import type { AssistantTurn, TurnToolCall } from '@/hooks/useChat';
import type { ToolCall } from '@/api/types';

// Adapt a streamed tool call into the shape ToolCallDisplay already renders,
// so the live "watch the agent work" view reuses the existing component.
function toToolCall(tc: TurnToolCall): ToolCall {
  return {
    id: tc.id,
    name: tc.name,
    arguments: tc.args,
    result: tc.error ?? tc.result,
    status: tc.error ? 'error' : tc.done ? 'success' : 'pending',
  };
}

interface AssistantTurnViewProps {
  turn: AssistantTurn;
}

// AssistantTurnView renders an agentic turn Claude-Code-style: each step's
// reasoning line and tool activity appears as it streams in, a per-turn token
// counter climbs alongside, and the final answer (or a failure box) settles at
// the end.
export function AssistantTurnView({ turn }: AssistantTurnViewProps) {
  const isFailed = turn.status === 'failed';
  const isStreaming = turn.status === 'streaming';
  // Context-utilization badge: input tokens (X) against the model's context
  // window (Y). X is the per-turn input count — the figure that fills the
  // window — climbing while streaming and snapping to the server total at the
  // end. Y arrives on the final event from the capabilities registry; until
  // then (and if the server omits it) it is 0 and we render a bare input count.
  const used = turn.inputTokens;
  const capacity = turn.contextWindow;
  const showTokens = used > 0 || isStreaming;
  const tokenLabel =
    capacity > 0
      ? `${used.toLocaleString()} of ${capacity.toLocaleString()} tokens · ${Math.round(
          (used / capacity) * 100
        )}% of context`
      : `${used.toLocaleString()} tokens`;
  // A denied write does not fail the turn (the LLM still answers), so surface
  // its specific reason as a dedicated notice on the completed turn. On a
  // failed turn the failure box already carries the mapped message.
  const writeFailure = !isFailed ? writeFailureMessage(turn.writeFailureCode) : undefined;

  return (
    <div className="flex justify-start">
      <div className="w-full min-w-0 max-w-[80%] space-y-2">
        {/* Stepwise work: a reasoning line per step, then its tool calls. The
            terminal answer-emitting step carries no tool calls and its content
            IS the final answer (the backend emits a final step event whose
            content it also returns as final_answer). Rendering that step's line
            would duplicate the answer bubble below, so suppress it. */}
        {turn.steps.map((step) => {
          const isAnswerEcho = step.toolCalls.length === 0 && step.content === turn.finalAnswer;
          return (
            <div key={step.stepNumber} className="space-y-1">
              {step.content && !isAnswerEcho && (
                <p className="whitespace-pre-wrap px-1 text-xs italic text-muted-foreground">
                  {step.content}
                </p>
              )}
              {step.toolCalls.map((tc) => (
                <ToolCallDisplay key={tc.id} toolCall={toToolCall(tc)} />
              ))}
            </div>
          );
        })}

        {/* Live working indicator until the final event lands. */}
        {isStreaming && (
          <div className="flex items-center gap-2 px-1 text-sm text-muted-foreground">
            <LoadingSpinner size="sm" />
            <span>Joe is working…</span>
          </div>
        )}

        {/* Unobtrusive notice when earlier messages fell out of context this
            turn (history pruning to fit the model's context budget). */}
        {turn.historyTrimmed && (
          <p
            className="px-1 text-xs italic text-muted-foreground"
            data-testid="history-trimmed-notice"
          >
            Some earlier messages are no longer in context.
          </p>
        )}

        {/* Unobtrusive notice when this turn's message was shortened to fit the
            context budget (oversized tool results carry their own inline
            marker, so they need no separate notice). */}
        {turn.userMessageTruncated && (
          <p
            className="px-1 text-xs italic text-muted-foreground"
            data-testid="user-message-truncated-notice"
          >
            Your message was shortened to fit the context budget.
          </p>
        )}

        {/* Differentiated write-failure notice: a tool write was denied this
            turn (RBAC zone or incident-mode), even though the turn completed. */}
        {writeFailure && (
          <div
            className="flex items-start gap-2 rounded-2xl border border-amber-300 bg-amber-50 px-4 py-2 text-sm text-amber-900 dark:border-amber-700 dark:bg-amber-950 dark:text-amber-200"
            data-testid="write-failure-notice"
          >
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <p className="min-w-0 whitespace-pre-wrap break-words">{writeFailure}</p>
          </div>
        )}

        {/* Final answer for a completed turn. */}
        {!isFailed && turn.finalAnswer && (
          <div className="rounded-2xl bg-muted px-4 py-2 text-sm text-foreground">
            <Markdown content={turn.finalAnswer} />
          </div>
        )}

        {/* Failure box: human status label plus the underlying error. */}
        {isFailed && (
          <div className="flex items-start gap-2 rounded-2xl border border-destructive/40 bg-destructive/10 px-4 py-2 text-sm text-destructive">
            <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0" />
            <div className="min-w-0">
              <p className="font-medium">{turn.failureLabel ?? 'Something went wrong'}</p>
              {turn.errorMessage && (
                <p className="mt-0.5 whitespace-pre-wrap break-words">{turn.errorMessage}</p>
              )}
            </div>
          </div>
        )}

        {/* Per-turn context-utilization badge — input tokens climb with each
            step and snap to the server total; once the final event lands it
            reads as "X of Y tokens · N% of context" against the model's
            context window. */}
        {showTokens && (
          <div className="px-1">
            <Badge variant="secondary" className="text-xs" data-testid="turn-tokens">
              {tokenLabel}
            </Badge>
          </div>
        )}
      </div>
    </div>
  );
}
