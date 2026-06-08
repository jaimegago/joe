import { NavLink } from 'react-router-dom';
import { LayoutDashboard, Database, MessageSquare, MessagesSquare, ShieldCheck, Users, Cpu, LogOut } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { useAuth } from '@/auth/AuthContext';
import { Button } from '@/components/ui/button';

interface NavItem {
  to: string;
  icon: typeof LayoutDashboard;
  label: string;
  end: boolean;
  adminOnly?: boolean;
}

// The Graph page (/graph route in App.tsx) is intentionally NOT listed here.
// The full-graph visualization isn't a daily-use view, but the route is kept
// reachable by direct URL for demos that explain Joe's dependency graph DB.
const navItems: NavItem[] = [
  { to: '/', icon: LayoutDashboard, label: 'Dashboard', end: true },
  { to: '/components', icon: Database, label: 'Components', end: false },
  { to: '/chat', icon: MessageSquare, label: 'Chat', end: false },
  { to: '/sessions', icon: MessagesSquare, label: 'Sessions', end: false },
  { to: '/admin', icon: ShieldCheck, label: 'Admin', end: false, adminOnly: true },
  { to: '/users', icon: Users, label: 'Users', end: false, adminOnly: true },
  { to: '/llm-settings', icon: Cpu, label: 'LLM Settings', end: false, adminOnly: true },
];

export function Sidebar() {
  const meQ = useCurrentUser();
  const { rbacEnabled, principal, logout } = useAuth();
  // is_admin is true in auth-disabled local mode, so this single predicate
  // surfaces admin entries there too. Until the query resolves, isAdmin is
  // false so admin-only entries never flash before status is known.
  const isAdmin = meQ.data?.is_admin === true;
  const visibleItems = navItems.filter((item) => !item.adminOnly || isAdmin);

  return (
    <aside className="fixed inset-y-0 left-0 z-10 flex w-60 flex-col border-r bg-background">
      <div className="flex h-16 items-center border-b px-6">
        <span className="text-lg font-bold tracking-tight">Joe</span>
        <span className="ml-1 text-xs text-muted-foreground">infra copilot</span>
      </div>
      <nav className="flex-1 space-y-1 p-3">
        {visibleItems.map(({ to, icon: Icon, label, end }) => (
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

      {/* Logout is only meaningful when RBAC is enforced; in permit-all local
          dev there is no credential to drop, so the control stays hidden. */}
      {rbacEnabled && (
        <div className="border-t p-3">
          {principal && (
            <p className="mb-2 truncate px-3 text-xs text-muted-foreground" title={principal}>
              {principal}
            </p>
          )}
          <Button variant="ghost" size="sm" className="w-full justify-start gap-3" onClick={logout}>
            <LogOut className="h-4 w-4" />
            Log out
          </Button>
        </div>
      )}
    </aside>
  );
}
