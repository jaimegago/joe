import { Outlet } from 'react-router-dom';
import { Sidebar } from './Sidebar';
import { IncidentBanner } from './IncidentBanner';

export function AppShell() {
  return (
    <div className="flex min-h-screen bg-background">
      <Sidebar />
      <div className="ml-60 flex flex-1 flex-col">
        <IncidentBanner />
        <main className="flex-1">
          <Outlet />
        </main>
      </div>
    </div>
  );
}
