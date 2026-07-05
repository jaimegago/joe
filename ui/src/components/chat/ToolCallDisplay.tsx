import { useState } from 'react';
import { ChevronDown, ChevronRight } from 'lucide-react';
import { Badge } from '@/components/ui/badge';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import type { ToolCall } from '@/api/types';

interface ToolCallDisplayProps {
  toolCall: ToolCall;
}

export function ToolCallDisplay({ toolCall }: ToolCallDisplayProps) {
  const [expanded, setExpanded] = useState(false);
  const argsPreview = JSON.stringify(toolCall.arguments).slice(0, 60);

  return (
    <div className="rounded border border-gray-200 bg-gray-50 text-xs">
      <button
        className="flex w-full items-center gap-1.5 px-3 py-1.5 text-left"
        onClick={() => setExpanded((v) => !v)}
      >
        {expanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
        <span className="text-gray-500">🔧</span>
        <span className="font-mono font-medium">{toolCall.name}</span>
        <span className="text-gray-400">
          ({argsPreview}
          {argsPreview.length >= 60 ? '…' : ''})
        </span>
        <span className="ml-auto">
          {toolCall.status === 'pending' && <LoadingSpinner size="sm" className="inline-block" />}
          {toolCall.status === 'success' && (
            <Badge variant="success" className="text-xs">
              ok
            </Badge>
          )}
          {toolCall.status === 'error' && (
            <Badge variant="destructive" className="text-xs">
              error
            </Badge>
          )}
        </span>
      </button>
      {expanded && toolCall.result && (
        <div className="border-t border-gray-200 bg-white px-3 py-2">
          <pre className="whitespace-pre-wrap font-mono text-xs text-gray-600 max-h-40 overflow-y-auto">
            {toolCall.result}
          </pre>
        </div>
      )}
    </div>
  );
}
