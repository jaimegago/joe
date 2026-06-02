import type { ReactNode } from 'react';
import { useAuth } from '@/auth/AuthContext';
import { LoginPage } from '@/auth/LoginPage';
import { LoadingPage } from '@/components/common/LoadingSpinner';

// AuthGate renders the logged-out shell. While auth state is resolving it
// shows a neutral loading state (not the app, not the login form); while
// unauthed it shows LoginPage instead of the application shell; once authed
// it renders the app. In RBAC-off local dev, status is always 'authed', so
// this is transparent.
export function AuthGate({ children }: { children: ReactNode }) {
  const { status } = useAuth();

  if (status === 'loading') {
    return (
      <div className="flex min-h-screen items-center justify-center bg-background">
        <LoadingPage />
      </div>
    );
  }

  if (status === 'unauthed') {
    return <LoginPage />;
  }

  return <>{children}</>;
}
