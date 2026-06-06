import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { IncidentBanner } from './IncidentBanner';
import { SafeModeBanner } from './SafeModeBanner';

export function AppShell() {
  return (
    <div className="flex min-h-screen bg-background">
      <Sidebar />
      <div className="ml-60 flex flex-1 flex-col">
        {/* Safe mode and incident mode are independent flags; both can show at
            once. Safe mode renders first (above) — it is the more restrictive
            state. Neither banner suppresses the other. */}
        <SafeModeBanner />
        <IncidentBanner />
        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
