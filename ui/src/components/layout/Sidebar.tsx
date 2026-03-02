import { NavLink } from 'react-router-dom';
import { LayoutDashboard, Network, Database, MessageSquare, ShieldCheck } from 'lucide-react';
import { cn } from '@/lib/utils';

const navItems = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/graph', icon: Network, label: 'Graph', end: false },
  { to: '/sources', icon: Database, label: 'Sources', end: false },
  { to: '/chat', icon: MessageSquare, label: 'Chat', end: false },
  { to: '/admin', icon: ShieldCheck, label: 'Admin', end: false },
];

export function Sidebar() {
  return (
    <aside className="fixed inset-y-0 left-0 z-10 flex w-60 flex-col border-r bg-background">
      <div className="flex h-16 items-center border-b px-6">
        <span className="text-lg font-bold tracking-tight">Joe</span>
        <span className="ml-1 text-xs text-muted-foreground">infra copilot</span>
      </div>
      <nav className="flex-1 space-y-1 p-3">
        {navItems.map(({ to, icon: Icon, label, end }) => (
          <NavLink
            key={to}
            to={to}
            end={end}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'bg-accent text-accent-foreground'
                  : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
              )
            }
          >
            <Icon className="h-4 w-4" />
            {label}
          </NavLink>
        ))}
      </nav>

    </aside>
  );
}
