import { useState } from 'react';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { useComponents } from '@/hooks/useComponents';
import type { AutoRunSelection } from '@/api/types';

// ScopeForm is the one write form the Auto Joe page has, in two shapes: a
// trigger takes a scope and nothing else; an install takes a scope and a
// cadence — what the install route takes and nothing more. Budget comes from
// deployment defaults, so there is no budget field. The scope is a component
// selection, picked from the registered components; a refusal is shown at the
// point of the act with the reason the API gave (`error`), and the caller
// re-reads the lists afterwards so what the page shows is the record's.
//
// Rendered only for admins: the page mounts it on is_admin, so a reader sees
// no control rather than a disabled one.
interface ScopeFormProps {
  kind: 'trigger' | 'install';
  onSubmit: (scope: AutoRunSelection, intervalMinutes: number) => void;
  isPending: boolean;
  error: string | null;
}

export function ScopeForm({ kind, onSubmit, isPending, error }: ScopeFormProps) {
  const componentsQ = useComponents();
  const [picked, setPicked] = useState<string[]>([]);
  const [interval, setInterval] = useState('60');
  const components = componentsQ.data ?? [];

  function toggle(id: string) {
    setPicked((prev) => (prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id]));
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({ components: picked }, Number.parseInt(interval, 10));
  }

  const prefix = kind === 'trigger' ? 'trigger' : 'install';
  return (
    <form
      onSubmit={handleSubmit}
      className="mb-3 space-y-2 rounded-md border border-dashed p-3 text-sm"
      data-testid={`${prefix}-form`}
      aria-label={kind === 'trigger' ? 'Trigger a run' : 'Install a schedule'}
    >
      <p className="font-medium">{kind === 'trigger' ? 'Trigger a run' : 'Install a schedule'}</p>
      <fieldset className="space-y-1">
        <legend className="text-xs text-muted-foreground">Scope: components</legend>
        {componentsQ.isLoading && (
          <p className="text-xs text-muted-foreground">Loading components…</p>
        )}
        {componentsQ.isError && (
          <p className="text-xs text-destructive">Couldn&apos;t load components.</p>
        )}
        {components.length === 0 && componentsQ.isSuccess && (
          <p className="text-xs text-muted-foreground">No components registered.</p>
        )}
        {components.map((c) => {
          const id = `${prefix}-scope-${c.id}`;
          return (
            <div key={c.id} className="flex items-center gap-2">
              <Checkbox
                id={id}
                checked={picked.includes(c.id)}
                onCheckedChange={() => toggle(c.id)}
                data-testid={`${prefix}-scope-${c.id}`}
              />
              <Label htmlFor={id} className="font-mono text-xs">
                {c.id}
              </Label>
            </div>
          );
        })}
      </fieldset>
      {kind === 'install' && (
        <div className="flex items-center gap-2">
          <Label htmlFor="install-interval" className="text-xs">
            Every
          </Label>
          <Input
            id="install-interval"
            type="number"
            min={1}
            step={1}
            value={interval}
            onChange={(e) => setInterval(e.target.value)}
            className="w-24"
            data-testid="install-interval"
            required
          />
          <span className="text-xs text-muted-foreground">minutes</span>
        </div>
      )}
      <div className="flex items-center gap-3">
        <Button type="submit" size="sm" disabled={isPending} data-testid={`${prefix}-submit`}>
          {kind === 'trigger' ? 'Trigger run' : 'Install schedule'}
        </Button>
        {error && (
          <p role="alert" className="text-xs text-destructive" data-testid={`${prefix}-error`}>
            {error}
          </p>
        )}
      </div>
    </form>
  );
}
