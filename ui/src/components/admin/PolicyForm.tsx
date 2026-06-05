import { useState } from 'react';
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from '@/components/ui/select';
import { hasReservedPrefix } from '@/lib/principals';
import type { SecurityZone, PrincipalRecord } from '@/api/types';

interface PolicyFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  zones: SecurityZone[];
  // Known principals from the identity registry (GET /admin/principals). Empty
  // until at least one user has logged in; the form falls back to manual entry.
  principals: PrincipalRecord[];
  onSubmit: (principal: string, zoneId: string) => void;
  isLoading?: boolean;
}

export function PolicyForm({ open, onOpenChange, zones, principals, onSubmit, isLoading }: PolicyFormProps) {
  // A principal only appears in the registry after its first OIDC login, but a
  // policy can be granted to a not-yet-seen principal (the backend accepts any
  // reserved-prefixed identity). So the picker offers known principals AND a
  // manual-entry fallback for pre-login provisioning — never a dead end when the
  // registry is empty. Manual mode is forced when there are no known principals.
  const [manual, setManual] = useState(false);
  const [picked, setPicked] = useState('');
  const [typed, setTyped] = useState('');
  const [selectedZone, setSelectedZone] = useState('');

  const manualMode = manual || principals.length === 0;
  const principal = manualMode ? typed.trim() : picked;
  // The backend rejects an unprefixed principal (rbac.HasReservedPrefix); known
  // principals always carry a prefix, so only validate the manual path.
  const manualInvalid = manualMode && typed.trim() !== '' && !hasReservedPrefix(typed.trim());
  const canSubmit = principal !== '' && selectedZone !== '' && !manualInvalid;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit(principal, selectedZone);
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Create Policy</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <div className="flex items-center justify-between">
              <Label htmlFor="policy-principal">Principal</Label>
              {principals.length > 0 && (
                <button
                  type="button"
                  className="text-xs text-muted-foreground underline-offset-2 hover:underline"
                  onClick={() => setManual((m) => !m)}
                >
                  {manualMode ? 'Choose from list' : 'Enter manually'}
                </button>
              )}
            </div>
            {manualMode ? (
              <>
                <Input
                  id="policy-principal"
                  value={typed}
                  onChange={(e) => setTyped(e.target.value)}
                  placeholder="user:alice@example.com"
                  required
                />
                {manualInvalid ? (
                  <p className="text-xs text-destructive">
                    Principal must start with a reserved prefix: user:, group:, or svc:
                  </p>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    For a user who has not logged in yet. Prefix with user:, group:, or svc:
                  </p>
                )}
              </>
            ) : (
              <Select value={picked} onValueChange={setPicked}>
                <SelectTrigger id="policy-principal">
                  <SelectValue placeholder="Select a principal" />
                </SelectTrigger>
                <SelectContent>
                  {principals.map((p) => (
                    <SelectItem key={p.principal} value={p.principal}>
                      <span>{p.display_name ?? p.principal}</span>
                      {p.display_name && (
                        <span className="ml-2 text-muted-foreground">— {p.principal}</span>
                      )}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </div>
          <div className="space-y-2">
            <Label>Zone</Label>
            <Select value={selectedZone} onValueChange={setSelectedZone}>
              <SelectTrigger>
                <SelectValue placeholder="Select a zone" />
              </SelectTrigger>
              <SelectContent>
                {zones.map((z) => (
                  <SelectItem key={z.id} value={z.id}>
                    <span>{z.name || z.id}</span>
                    {z.description && (
                      <span className="ml-2 text-muted-foreground">— {z.description}</span>
                    )}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={(isLoading ?? false) || !canSubmit}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
