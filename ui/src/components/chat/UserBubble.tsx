// UserBubble renders a user message: a right-aligned bubble with a timestamp.
export function UserBubble({ content, createdAt }: { content: string; createdAt: string }) {
  return (
    <div className="flex justify-end">
      <div className="max-w-[80%] min-w-0 space-y-1">
        <div className="rounded-2xl bg-primary px-4 py-2 text-sm text-primary-foreground">
          <p className="whitespace-pre-wrap">{content}</p>
        </div>
        <p className="px-1 text-right text-xs text-muted-foreground">
          {new Date(createdAt).toLocaleTimeString()}
        </p>
      </div>
    </div>
  );
}
