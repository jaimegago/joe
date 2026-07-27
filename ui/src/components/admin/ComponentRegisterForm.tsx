import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { fetchComponentTypes } from '@/api/components';

interface ComponentRegisterFormData {
  id: string;
  type: string;
  name: string;
}

// TYPE_PLACEHOLDER_EXAMPLES gives the ID/Name inputs a domain-relevant example
// for the types most likely to be walked from the public guides. Any type not
// listed here (including a future backend-added type) falls to
// GENERIC_PLACEHOLDER_EXAMPLE rather than showing an unrelated example.
const TYPE_PLACEHOLDER_EXAMPLES: Record<string, { id: string; name: string }> = {
  kubernetes: { id: 'prod-cluster', name: 'Production Cluster' },
  prometheus: { id: 'prod-prometheus', name: 'Production Prometheus' },
  grafana: { id: 'prod-grafana', name: 'Production Grafana' },
  github: { id: 'prod-github', name: 'Production GitHub' },
  postgresql: { id: 'prod-postgres', name: 'Production Postgres' },
};
const GENERIC_PLACEHOLDER_EXAMPLE = { id: 'component-id', name: 'Component name' };

interface ComponentRegisterFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: ComponentRegisterFormData) => void;
  isLoading?: boolean;
}

export function ComponentRegisterForm({
  open,
  onOpenChange,
  onSubmit,
  isLoading,
}: ComponentRegisterFormProps) {
  const [id, setId] = useState('');
  const [type, setType] = useState('');
  const [name, setName] = useState('');

  // Type selector is populated from the authoritative backend enum — never a
  // hardcoded TS list. Only fetched once the dialog is opened.
  const typesQ = useQuery({
    queryKey: ['component-types'],
    queryFn: fetchComponentTypes,
    enabled: open,
  });
  const types = typesQ.data ?? [];

  // Live validity: all three fields are required by the governed create
  // endpoint (id is operator-supplied, not generated), so submit stays disabled
  // until each is present.
  const canSubmit = id.trim() !== '' && type !== '' && name.trim() !== '';
  const placeholderExample = TYPE_PLACEHOLDER_EXAMPLES[type] ?? GENERIC_PLACEHOLDER_EXAMPLE;

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit({ id: id.trim(), type, name: name.trim() });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Register Component</DialogTitle>
          <DialogDescription>
            Registration records an <strong>inert</strong> component only: it lands in the{' '}
            <strong>unassigned zone</strong> under the read-only floor with{' '}
            <strong>no credentials</strong> and can take no action. No credentials are collected
            here — they are supplied later, at promotion. After registering you must separately{' '}
            <strong>promote</strong> it (to supply credentials) and{' '}
            <strong>assign it a zone</strong> before it can do anything.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <Label htmlFor="component-id">Component ID</Label>
            <Input
              id="component-id"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder={placeholderExample.id}
              required
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="component-type">Type</Label>
            <Select value={type} onValueChange={setType}>
              <SelectTrigger id="component-type">
                <SelectValue placeholder={typesQ.isLoading ? 'Loading types…' : 'Select a type'} />
              </SelectTrigger>
              <SelectContent>
                {types.map((t) => (
                  <SelectItem key={t} value={t}>
                    {t}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1">
            <Label htmlFor="component-name">Name</Label>
            <Input
              id="component-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder={placeholderExample.name}
              required
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={(isLoading ?? false) || !canSubmit}>
              Register
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
