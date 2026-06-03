import { AlertTriangle } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import { ToolCallDisplay } from './ToolCallDisplay';
import { Markdown } from './Markdown';
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
  const showTokens = turn.tokens > 0 || isStreaming;

  return (
    <div className="flex justify-start">
      <div className="w-full min-w-0 max-w-[80%] space-y-2">
        {/* Stepwise work: a reasoning line per step, then its tool calls. */}
        {turn.steps.map((step) => (
          <div key={step.stepNumber} className="space-y-1">
            {step.content && (
              <p className="whitespace-pre-wrap px-1 text-xs italic text-muted-foreground">{step.content}</p>
            )}
            {step.toolCalls.map((tc) => (
              <ToolCallDisplay key={tc.id} toolCall={toToolCall(tc)} />
            ))}
          </div>
        ))}

        {/* Live working indicator until the final event lands. */}
        {isStreaming && (
          <div className="flex items-center gap-2 px-1 text-sm text-muted-foreground">
            <LoadingSpinner size="sm" />
            <span>Joe is working…</span>
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

        {/* Per-turn token counter — climbs with each step, snaps to the total. */}
        {showTokens && (
          <div className="px-1">
            <Badge variant="secondary" className="text-xs" data-testid="turn-tokens">
              {turn.tokens.toLocaleString()} tokens
            </Badge>
          </div>
        )}
      </div>
    </div>
  );
}
