import { useState } from 'react';
import type { FormEvent } from 'react';
import { ApiRequestError } from '@/api/client';
import { useAuth } from '@/auth/AuthContext';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';

// LoginPage is the logged-out shell shown when the app is unauthed (RBAC on,
// no valid credential). Behaviour by OIDC-configured state (Stream H2):
//   - oidcEnabled=true → OIDC is the primary path: a single "Sign in"
//     button triggers the full-page navigation to the IdP. The
//     service-account-key field is hidden behind a small disclosure
//     ("Use a service-account key") for break-glass access.
//   - oidcEnabled=false → token/break-glass is the only mechanism: the
//     service-account-key field shows directly with no OIDC button.
// The key path hands the key to the auth context, which persists it and
// re-fetches /me; a 401 surfaces inline and the invalid token is not kept.
// oidcRoundTripError reads a one-shot auth-failure signal off the URL. The
// server's OIDC callback renders its own error page on failure, but if a
// deployment routes a failed round-trip back to the SPA with an `error`
// query param, surface it as a clear, non-cryptic banner rather than a
// silent logged-out shell. Returns null when there is no such signal.
function oidcRoundTripError(): string | null {
  if (typeof window === 'undefined') return null;
  const params = new URLSearchParams(window.location.search);
  return params.has('error') ? 'Sign-in did not complete. Please try again.' : null;
}

export function LoginPage() {
  const { login, loginWithOIDC, oidcEnabled } = useAuth();
  const [token, setToken] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [oidcError] = useState(oidcRoundTripError);
  // When OIDC is the primary path, the key field starts hidden behind a
  // disclosure. When OIDC is unavailable it is always shown.
  const [showKeyField, setShowKeyField] = useState(false);
  const keyFieldVisible = !oidcEnabled || showKeyField;

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
            <span className="text-xs font-normal text-muted-foreground">Joe Operates Everything</span>
          </CardTitle>
          <CardDescription>
            {oidcEnabled
              ? 'Sign in with your organization account to continue.'
              : 'Sign in with a service-account key to continue.'}
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {oidcError && (
            <p role="alert" className="text-sm text-destructive">
              {oidcError}
            </p>
          )}

          {oidcEnabled && (
            <Button type="button" className="w-full" onClick={loginWithOIDC}>
              Sign in
            </Button>
          )}

          {oidcEnabled && !showKeyField && (
            <button
              type="button"
              className="w-full text-center text-xs text-muted-foreground underline-offset-4 hover:underline"
              onClick={() => setShowKeyField(true)}
            >
              Use a service-account key
            </button>
          )}

          {keyFieldVisible && (
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
              <Button
                type="submit"
                variant={oidcEnabled ? 'secondary' : 'default'}
                className="w-full"
                disabled={submitting || !token.trim()}
              >
                {submitting ? 'Signing in…' : 'Sign in with key'}
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
