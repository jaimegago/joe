import { useState } from 'react';
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { hasReservedPrefix } from '@/lib/principals';

interface AdminFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (principal: string, reason: string) => void;
  isLoading?: boolean;
}

export function AdminForm({ open, onOpenChange, onSubmit, isLoading }: AdminFormProps) {
  const [principal, setPrincipal] = useState('');
  const [reason, setReason] = useState('');

  const trimmed = principal.trim();
  // Mirror the backend's reserved-prefix rule so an obvious typo fails before
  // the request; the server validates identically and 400s if it disagrees.
  const invalid = trimmed !== '' && !hasReservedPrefix(trimmed);
  const canSubmit = trimmed !== '' && !invalid;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit(trimmed, reason.trim());
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Add Admin</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <Label htmlFor="admin-principal">Principal</Label>
            <Input
              id="admin-principal"
              value={principal}
              onChange={(e) => setPrincipal(e.target.value)}
              placeholder="user:alice@example.com"
              required
            />
            {invalid ? (
              <p className="text-xs text-destructive">
                Principal must start with a reserved prefix: user:, group:, or svc:
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                Prefix with user:, group:, or svc:. Granting admin removes the
                principal&apos;s redundant per-zone grants.
              </p>
            )}
          </div>
          <div className="space-y-1">
            <Label htmlFor="admin-reason">Reason (optional)</Label>
            <Input
              id="admin-reason"
              value={reason}
              onChange={(e) => setReason(e.target.value)}
              placeholder="on-call escalation owner"
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
            <Button type="submit" disabled={(isLoading ?? false) || !canSubmit}>
              Add Admin
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
