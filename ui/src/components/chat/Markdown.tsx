import { useState } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Check, Copy } from 'lucide-react';

function CodeBlock({ children }: { children: string }) {
  const [copied, setCopied] = useState(false);

  const handleCopy = () => {
    void navigator.clipboard.writeText(children).then(() => {
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

// Markdown renders assistant prose with GitHub-flavored markdown, code blocks
// with a copy button, and inline-code styling. Extracted so both finished
// turns and streamed final answers render identically.
export function Markdown({ content }: { content: string }) {
  return (
    <div className="prose prose-sm dark:prose-invert max-w-none [&_code:not(pre_code)]:bg-zinc-200 [&_code:not(pre_code)]:dark:bg-zinc-700 [&_code:not(pre_code)]:rounded [&_code:not(pre_code)]:px-1 [&_code:not(pre_code)]:py-0.5 [&_code:not(pre_code)]:text-xs">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          pre({ children }) {
            const codeEl = children as React.ReactElement<{ children?: string }>;
            const text = codeEl?.props?.children ?? '';
            return <CodeBlock>{String(text)}</CodeBlock>;
          },
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
        {content}
      </ReactMarkdown>
    </div>
  );
}
