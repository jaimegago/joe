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
import type { SecurityZone } from '@/api/types';

interface PolicyFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  zones: SecurityZone[];
  onSubmit: (principal: string, zoneId: string) => void;
  isLoading?: boolean;
}

export function PolicyForm({ open, onOpenChange, zones, onSubmit, isLoading }: PolicyFormProps) {
  const [principal, setPrincipal] = useState('');
  const [selectedZone, setSelectedZone] = useState('');

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
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
            <Label htmlFor="policy-principal">Principal</Label>
            <Input
              id="policy-principal"
              value={principal}
              onChange={(e) => setPrincipal(e.target.value)}
              placeholder="sre-team"
              required
            />
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
            <Button type="submit" disabled={isLoading || !principal || !selectedZone}>
              Create
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
