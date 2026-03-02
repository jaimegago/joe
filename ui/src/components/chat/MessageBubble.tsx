import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Check, Copy } from 'lucide-react';
import type { ChatMessage } from '@/api/types';
import { ToolCallDisplay } from './ToolCallDisplay';
import { cn } from '@/lib/utils';

interface MessageBubbleProps {
  message: ChatMessage;
}

function CodeBlock({ children }: { children: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    navigator.clipboard.writeText(children).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  return (
    <div className="relative group my-2">
      <pre className="bg-zinc-900 text-zinc-100 rounded p-3 overflow-x-auto text-xs leading-relaxed">
        <code>{children}</code>
      </pre>
      <button
        onClick={handleCopy}
        className="absolute top-2 right-2 p-1 rounded bg-zinc-700 text-zinc-300 opacity-0 group-hover:opacity-100 transition-opacity hover:bg-zinc-600"
        title="Copy"
      >
        {copied ? <Check size={14} /> : <Copy size={14} />}
      </button>
    </div>
  );
}

export function MessageBubble({ message }: MessageBubbleProps) {
  const isUser = message.role === 'user';

  return (
    <div className={cn('flex', isUser ? 'justify-end' : 'justify-start')}>
      <div className={cn('max-w-[80%] min-w-0 space-y-1', isUser ? 'items-end' : 'items-start')}>
        <div
          className={cn(
            'rounded-2xl px-4 py-2 text-sm',
            isUser
              ? 'bg-primary text-primary-foreground'
              : 'bg-muted text-foreground'
          )}
        >
          {isUser ? (
            <p className="whitespace-pre-wrap">{message.content}</p>
          ) : (
            <div className="prose prose-sm dark:prose-invert max-w-none [&_code:not(pre_code)]:bg-zinc-200 [&_code:not(pre_code)]:dark:bg-zinc-700 [&_code:not(pre_code)]:rounded [&_code:not(pre_code)]:px-1 [&_code:not(pre_code)]:py-0.5 [&_code:not(pre_code)]:text-xs">
              <ReactMarkdown
                remarkPlugins={[remarkGfm]}
                components={{
                  pre({ children }) {
                    // Extract the text content from the code element
                    const codeEl = (children as React.ReactElement<{ children?: string }>);
                    const text = codeEl?.props?.children ?? '';
                    return <CodeBlock>{String(text)}</CodeBlock>;
                  },
                  // Prevent prose from wrapping our custom pre in another pre
                  code({ children, className }) {
                    const isBlock = !className && typeof children === 'string' && !children.includes('\n');
                    if (isBlock) {
                      return (
                        <code className="bg-zinc-200 dark:bg-zinc-700 rounded px-1 py-0.5 text-xs">
                          {children}
                        </code>
                      );
                    }
                    return <code className={className}>{children}</code>;
                  },
                }}
              >
                {message.content}
              </ReactMarkdown>
            </div>
          )}
        </div>
        {message.toolCalls && message.toolCalls.length > 0 && (
          <div className="space-y-1 w-full">
            {message.toolCalls.map((tc) => (
              <ToolCallDisplay key={tc.id} toolCall={tc} />
            ))}
          </div>
        )}
        <p className="text-xs text-muted-foreground px-1">
          {new Date(message.created_at).toLocaleTimeString()}
        </p>
      </div>
    </div>
  );
}
