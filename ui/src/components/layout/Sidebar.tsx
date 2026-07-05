import { useState } from 'react';
import { NavLink } from 'react-router-dom';
import {
  Database,
  MessageSquare,
  MessagesSquare,
  ShieldCheck,
  Users,
  UserCog,
  KeyRound,
  Cpu,
  Boxes,
  Scale,
  Eye,
  Puzzle,
  ChevronDown,
  ChevronRight,
  LogOut,
} from 'lucide-react';
import type { LucideIcon } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useCurrentUser } from '@/hooks/useCurrentUser';
import { useAuth } from '@/auth/AuthContext';
import { Button } from '@/components/ui/button';
import { DeclareIncidentButton } from '@/components/incident/DeclareIncidentButton';

interface NavItem {
  to: string;
  icon: LucideIcon;
  label: string;
  end: boolean;
}

// Navigation model (session admin-nav-consolidation, see docs/project/DECISIONS.md):
// operator surfaces are flat top-level entries; the admin-only surfaces that
// have no operator view live under a single expandable Admin subgroup. Surfaces
// with an operator view (Components, Sessions) appear ONCE as top-level operator
// entries and reveal their admin affordances INLINE when the caller is an admin
// — they are deliberately NOT duplicated under the Admin subgroup.

// The Graph page (/graph route in App.tsx) is intentionally NOT listed here.
// The full-graph visualization isn't a daily-use view, but the route is kept
// reachable by direct URL for demos that explain Joe's dependency graph DB.
const operatorNav: NavItem[] = [
  { to: '/chat', icon: MessageSquare, label: 'Chat', end: false },
  { to: '/sessions', icon: MessagesSquare, label: 'Sessions', end: false },
  { to: '/components', icon: Database, label: 'Components', end: false },
];

// Admin-only surfaces with NO operator view, grouped under the expandable Admin
// subgroup. Each is its own route (the former in-page Admin tab row was removed).
// Rendered only when the caller is an admin. Credentials lives here too (session
// admin-nav-consolidation-01, correcting D-0039): its backing
// /api/v1/admin/credential-status endpoint is server-gated to admins and it has
// no operator-visible subset, so it is a plain admin-only child like the rest.
const adminNav: NavItem[] = [
  { to: '/admin/zones', icon: Boxes, label: 'Zones', end: false },
  { to: '/admin/policies', icon: Scale, label: 'Policies', end: false },
  { to: '/admin/autonomous-reads', icon: Eye, label: 'Autonomous Reads', end: false },
  { to: '/admin/skills', icon: Puzzle, label: 'Skills', end: false },
  { to: '/admin/admins', icon: UserCog, label: 'Admins', end: false },
  { to: '/admin/users', icon: Users, label: 'Users', end: false },
  { to: '/credentials', icon: KeyRound, label: 'Credentials', end: false },
  { to: '/llm-settings', icon: Cpu, label: 'LLM Settings', end: false },
];

function navLinkClasses({ isActive }: { isActive: boolean }): string {
  return cn(
    'flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors',
    isActive
      ? 'bg-accent text-accent-foreground'
      : 'text-muted-foreground hover:bg-accent/50 hover:text-foreground'
  );
}

function NavRow({ item }: { item: NavItem }) {
  const Icon = item.icon;
  return (
    <NavLink to={item.to} end={item.end} className={navLinkClasses}>
      <Icon className="h-4 w-4" />
      {item.label}
    </NavLink>
  );
}

export function Sidebar() {
  const meQ = useCurrentUser();
  const { rbacEnabled, principal, logout } = useAuth();
  // is_admin is true in auth-disabled local mode, so this single predicate
  // surfaces admin entries there too. Until the query resolves, isAdmin is
  // false so admin-only entries never flash before status is known.
  const isAdmin = meQ.data?.is_admin === true;

  // The Admin subgroup is open by default so its children are discoverable; the
  // header toggles it. It only renders for admins.
  const [adminOpen, setAdminOpen] = useState(true);

  return (
    <aside className="fixed inset-y-0 left-0 z-10 flex w-60 flex-col border-r bg-background">
      <div className="flex h-16 items-center border-b px-6">
        <span className="text-lg font-bold tracking-tight">Joe</span>
        <span className="ml-1 text-xs text-muted-foreground">infra copilot</span>
      </div>
      <nav className="flex-1 space-y-1 overflow-y-auto p-3">
        {operatorNav.map((item) => (
          <NavRow key={item.to} item={item} />
        ))}

        {isAdmin && (
          <div className="pt-2">
            <button
              type="button"
              onClick={() => setAdminOpen((v) => !v)}
              aria-expanded={adminOpen}
              className="flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground"
            >
              <ShieldCheck className="h-4 w-4" />
              <span className="flex-1 text-left">Admin</span>
              {adminOpen ? (
                <ChevronDown className="h-4 w-4" />
              ) : (
                <ChevronRight className="h-4 w-4" />
              )}
            </button>
            {adminOpen && (
              <div className="mt-1 space-y-1 border-l pl-3">
                {adminNav.map((item) => (
                  <NavRow key={item.to} item={item} />
                ))}
              </div>
            )}
          </div>
        )}
      </nav>

      {/* Global, always-reachable declare-incident control (§12.10). Routes to the
          sessions area's promote-or-start-new disambiguation; hidden while an
          incident is already active. */}
      <div className="px-3 pb-2">
        <DeclareIncidentButton />
      </div>

      {/* Identity footer. The ADMIN badge is the persistent admin indicator,
          driven by the same /me is_admin flag RequireAdmin gates on (isAdmin
          above), so it is rendered whenever the caller is an admin — including
          permit-all local dev, where there is no principal to display. Non-admin
          and unauthenticated callers render no badge. Logout stays RBAC-only: in
          permit-all local dev there is no credential to drop. */}
      {(rbacEnabled || isAdmin) && (
        <div className="border-t p-3">
          <div className="mb-2 flex items-center gap-2 px-3">
            {principal && (
              <p className="truncate text-xs text-muted-foreground" title={principal}>
                {principal}
              </p>
            )}
            {isAdmin && (
              <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[10px] font-semibold tracking-wide text-primary">
                ADMIN
              </span>
            )}
          </div>
          {rbacEnabled && (
            <Button
              variant="ghost"
              size="sm"
              className="w-full justify-start gap-3"
              onClick={logout}
            >
              <LogOut className="h-4 w-4" />
              Log out
            </Button>
          )}
        </div>
      )}
    </aside>
  );
}
