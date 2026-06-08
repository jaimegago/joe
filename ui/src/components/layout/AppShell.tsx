import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { IncidentBanner } from './IncidentBanner';
import { ObservationBanner } from './ObservationBanner';
import { SafeModeBanner } from './SafeModeBanner';

export function AppShell() {
  return (
    <div className="flex min-h-screen bg-background">
      <Sidebar />
      <div className="ml-60 flex flex-1 flex-col">
        {/* Three app-shell strips. Safe mode and incident mode are independent
            flags; both can show at once, and safe mode renders first (above) as
            the more restrictive state. ObservationBanner sits on the same
            write-floor axis as safe mode and is mutually exclusive with it (the
            floor carries one reason), so it can never co-render with
            SafeModeBanner — it is ordered just below it. Incident is an
            independent axis and stays last. Neither banner suppresses another. */}
        <SafeModeBanner />
        <ObservationBanner />
        <IncidentBanner />
        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
