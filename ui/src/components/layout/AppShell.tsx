import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { IncidentBanner } from './IncidentBanner';
import { ObservationBanner } from './ObservationBanner';
import { SafeModeBanner } from './SafeModeBanner';

export function AppShell() {
  return (
    <div className="flex h-screen bg-background">
      <Sidebar />
      {/* The shell column is a fixed-viewport-height (h-screen) flex column:
          the banner strips take their natural height and <main> fills exactly
          the space that remains. A full-height page (chat, graph) sizes to
          <main> with h-full, so its pinned chat input stays on-screen whether or
          not a banner is present — the page never assumes the whole viewport.
          Document-tall pages scroll inside <main>, not the window. */}
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
        <main className="flex-1 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
