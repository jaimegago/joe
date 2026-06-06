interface HeaderProps {
  // Usually a plain string; the chat page passes a node so the page title can be
  // edited inline. h1 accepts phrasing content, so an editable title must render
  // as spans/inputs/buttons (no block/form wrappers).
  title: React.ReactNode;
  actions?: React.ReactNode;
}

export function Header({ title, actions }: HeaderProps) {
  return (
    <header className="flex h-16 items-center justify-between border-b bg-background px-6">
      <h1 className="text-xl font-semibold">{title}</h1>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </header>
  );
}
