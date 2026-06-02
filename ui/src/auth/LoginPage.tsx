import { useState } from 'react';
import type { FormEvent } from 'react';
import { ApiRequestError } from '@/api/client';
import { useAuth } from '@/auth/AuthContext';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

// LoginPage is the dev / break-glass entry point shown when the app is
// unauthed (RBAC on, no valid credential). It is token entry only — no
// OIDC affordance (deferred to H2). On submit it hands the key to the auth
// context, which persists it and re-fetches /me; a 401 surfaces inline and
// the invalid token is not kept.
export function LoginPage() {
  const { login } = useAuth();
  const [token, setToken] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    if (!token.trim() || submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      await login(token.trim());
    } catch (err) {
      setError(
        err instanceof ApiRequestError && err.status === 401
          ? 'Authentication failed — check your service-account key.'
          : 'Could not sign in. Please try again.'
      );
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle className="flex items-baseline gap-2">
            <span>Joe</span>
            <span className="text-xs font-normal text-muted-foreground">infra copilot</span>
          </CardTitle>
          <CardDescription>Sign in with a service-account key to continue.</CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={(e) => void handleSubmit(e)} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="service-account-key">Service-account key</Label>
              <Input
                id="service-account-key"
                type="password"
                autoComplete="off"
                autoFocus
                value={token}
                onChange={(e) => setToken(e.target.value)}
                placeholder="Paste your key"
                aria-invalid={error ? true : undefined}
              />
            </div>
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <Button type="submit" className="w-full" disabled={submitting || !token.trim()}>
              {submitting ? 'Signing in…' : 'Sign in'}
            </Button>
          </form>
        </CardContent>
      </Card>
    </div>
  );
}
