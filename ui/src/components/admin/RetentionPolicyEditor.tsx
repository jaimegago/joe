import { useState } from 'react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import { toast } from 'sonner';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Card } from '@/components/ui/card';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import { useRetentionPolicy } from '@/hooks/useAdminSessions';
import { updateRetentionPolicy } from '@/api/adminSessions';
import { AlertTriangle } from 'lucide-react';

type TerminalAction = 'trash_then_purge' | 'archive';

// RetentionPolicyEditor is the §12.5 retention-policy editor: the three knobs and
// their defaults (inactivity window default OFF, trash-grace default 30 days,
// terminal-action selector trash_then_purge|archive). It round-trips the policy
// through the admin retention-policy get/put endpoints and changes no backend
// behavior. Selecting the `archive` terminal action surfaces the §12.6 dependency
// that archive requires a configured archive directory.
export function RetentionPolicyEditor() {
  const qc = useQueryClient();
  const policyQ = useRetentionPolicy();

  // Local form state seeded once the policy loads, so editing a knob never races
  // the query. `seeded` guards the one-time seed.
  const [seeded, setSeeded] = useState(false);
  const [inactivityOff, setInactivityOff] = useState(true);
  const [inactivityDays, setInactivityDays] = useState('30');
  const [trashGraceDays, setTrashGraceDays] = useState('30');
  const [terminalAction, setTerminalAction] = useState<TerminalAction>('trash_then_purge');

  if (policyQ.data && !seeded) {
    const p = policyQ.data;
    setInactivityOff(p.inactivity_days == null);
    if (p.inactivity_days != null) setInactivityDays(String(p.inactivity_days));
    setTrashGraceDays(String(p.trash_grace_days));
    setTerminalAction(p.terminal_action);
    setSeeded(true);
  }

  const saveMut = useMutation({
    mutationFn: () =>
      updateRetentionPolicy({
        inactivity_days: inactivityOff ? null : Math.max(0, parseInt(inactivityDays, 10) || 0),
        trash_grace_days: Math.max(0, parseInt(trashGraceDays, 10) || 0),
        terminal_action: terminalAction,
      }),
    onSuccess: () => {
      toast.success('Retention policy saved');
      void qc.invalidateQueries({ queryKey: ['retention-policy'] });
    },
    onError: (e: Error) => toast.error(e.message),
  });

  if (policyQ.isLoading) return <LoadingSpinner />;

  return (
    <Card className="max-w-2xl space-y-5 p-5">
      <div>
        <h3 className="text-sm font-semibold">Retention policy</h3>
        <p className="text-xs text-muted-foreground">
          The single retention policy the background sweeper applies (§12.5).
        </p>
      </div>

      {/* Inactivity window — default OFF. Auto-expiration is opt-in for the
          regulated posture: nothing auto-expires until an admin sets a window. */}
      <div className="space-y-2">
        <Label htmlFor="inactivity-days">Inactivity window</Label>
        <div className="flex items-center gap-2">
          <Checkbox
            id="inactivity-off"
            checked={inactivityOff}
            onCheckedChange={(c) => setInactivityOff(c === true)}
          />
          <Label htmlFor="inactivity-off" className="text-sm font-normal">
            Off (nothing auto-expires)
          </Label>
        </div>
        {!inactivityOff && (
          <div className="flex items-center gap-2">
            <Input
              id="inactivity-days"
              type="number"
              min={0}
              value={inactivityDays}
              onChange={(e) => setInactivityDays(e.target.value)}
              className="h-8 w-28"
              aria-label="Inactivity window in days"
            />
            <span className="text-sm text-muted-foreground">days of inactivity</span>
          </div>
        )}
      </div>

      {/* Trash-grace — default 30 days. */}
      <div className="space-y-2">
        <Label htmlFor="trash-grace-days">Trash grace period</Label>
        <div className="flex items-center gap-2">
          <Input
            id="trash-grace-days"
            type="number"
            min={0}
            value={trashGraceDays}
            onChange={(e) => setTrashGraceDays(e.target.value)}
            className="h-8 w-28"
            aria-label="Trash grace in days"
          />
          <span className="text-sm text-muted-foreground">
            days in trash before automatic purge
          </span>
        </div>
      </div>

      {/* Terminal action — trash_then_purge | archive. */}
      <div className="space-y-2">
        <Label htmlFor="terminal-action">Terminal action</Label>
        <Select
          value={terminalAction}
          onValueChange={(v) => setTerminalAction(v as TerminalAction)}
        >
          <SelectTrigger id="terminal-action" className="h-8 w-64" aria-label="Terminal action">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="trash_then_purge">Trash, then purge</SelectItem>
            <SelectItem value="archive">Archive</SelectItem>
          </SelectContent>
        </Select>
        {terminalAction === 'archive' && (
          <p className="flex items-start gap-1.5 text-xs text-amber-700 dark:text-amber-300">
            <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0" aria-hidden="true" />
            The <span className="font-medium">archive</span> action requires a configured archive
            directory (§12.6). Without one, archive operations will fail with “archive provider not
            configured”.
          </p>
        )}
      </div>

      <Button size="sm" onClick={() => saveMut.mutate()} disabled={saveMut.isPending}>
        Save policy
      </Button>
    </Card>
  );
}
