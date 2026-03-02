import { useState } from 'react';
import {
  Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Checkbox } from '@/components/ui/checkbox';

const ALL_ACTIONS = ['read', 'query', 'mutate', 'delete'] as const;
type Action = typeof ALL_ACTIONS[number];

interface ZoneFormData {
  id: string;
  name: string;
  description: string;
  allowed_actions: string[];
}

interface ZoneFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  initial?: Partial<ZoneFormData>;
  onSubmit: (data: ZoneFormData) => void;
  isLoading?: boolean;
}

export function ZoneForm({ open, onOpenChange, initial, onSubmit, isLoading }: ZoneFormProps) {
  const [id, setId] = useState(initial?.id ?? '');
  const [name, setName] = useState(initial?.name ?? '');
  const [description, setDescription] = useState(initial?.description ?? '');
  const [actions, setActions] = useState<Action[]>(
    (initial?.allowed_actions as Action[] | undefined) ?? ['read']
  );

  function toggleAction(a: Action) {
    setActions((prev) => prev.includes(a) ? prev.filter((x) => x !== a) : [...prev, a]);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    onSubmit({ id, name, description, allowed_actions: actions });
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{initial?.id ? 'Edit Zone' : 'Create Zone'}</DialogTitle>
        </DialogHeader>
        <form onSubmit={handleSubmit} className="space-y-4">
          <div className="space-y-1">
            <Label htmlFor="zone-id">Zone ID</Label>
            <Input
              id="zone-id"
              value={id}
              onChange={(e) => setId(e.target.value)}
              placeholder="prod-readonly"
              disabled={!!initial?.id}
              required
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="zone-name">Name</Label>
            <Input
              id="zone-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Production Read-Only"
              required
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="zone-desc">Description</Label>
            <Input
              id="zone-desc"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="Read-only access to production sources"
            />
          </div>
          <div className="space-y-2">
            <Label>Allowed Actions</Label>
            <div className="flex gap-4">
              {ALL_ACTIONS.map((a) => (
                <label key={a} className="flex items-center gap-1.5 text-sm cursor-pointer">
                  <Checkbox
                    checked={actions.includes(a)}
                    onCheckedChange={() => toggleAction(a)}
                  />
                  {a}
                </label>
              ))}
            </div>
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={isLoading || !id || !name}>
              {initial?.id ? 'Save' : 'Create'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
