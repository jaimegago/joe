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

interface ComponentRegisterFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onSubmit: (data: ComponentRegisterFormData) => void;
  isLoading?: boolean;
}

// Mirrors the backend rule in internal/componentgov (ValidateComponentID):
// lowercase letters, digits, and hyphens; must start and end with a letter or
// digit; 1-63 characters. The backend rule is authoritative — this only gives
// immediate feedback and gates submit.
const COMPONENT_ID_PATTERN = /^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/;
const COMPONENT_ID_MAX_LENGTH = 63;

function isValidComponentId(id: string): boolean {
  return id.length <= COMPONENT_ID_MAX_LENGTH && COMPONENT_ID_PATTERN.test(id);
}

// slugifyComponentId derives a rule-conforming ID from a human name:
// lowercase, invalid-character runs collapse to a single hyphen, edge hyphens
// trimmed, truncated to the max length (re-trimming any hyphen the cut
// exposes).
function slugifyComponentId(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, COMPONENT_ID_MAX_LENGTH)
    .replace(/-+$/, '');
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
  // The ID is auto-slugified from Name until the operator unlocks the field
  // and edits it by hand; from that point on (for the rest of this dialog
  // session — the key-prop remount in ComponentsPage resets it) Name changes
  // stop touching the ID.
  const [idUnlocked, setIdUnlocked] = useState(false);
  const [idManuallyEdited, setIdManuallyEdited] = useState(false);

  // Type selector is populated from the authoritative backend enum — never a
  // hardcoded TS list. Only fetched once the dialog is opened.
  const typesQ = useQuery({
    queryKey: ['component-types'],
    queryFn: fetchComponentTypes,
    enabled: open,
  });
  const types = typesQ.data ?? [];

  const idValid = isValidComponentId(id);

  // Live validity: all three fields are required by the governed create
  // endpoint, and the ID must satisfy the format rule the backend enforces.
  const canSubmit = idValid && type !== '' && name.trim() !== '';

  function handleNameChange(value: string) {
    setName(value);
    if (!idManuallyEdited) {
      setId(slugifyComponentId(value));
    }
  }

  function handleIdChange(value: string) {
    setId(value);
    setIdManuallyEdited(true);
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    onSubmit({ id, type, name: name.trim() });
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
            <Label htmlFor="component-name">Name</Label>
            <Input
              id="component-name"
              value={name}
              onChange={(e) => handleNameChange(e.target.value)}
              placeholder="Component name"
              required
            />
          </div>
          <div className="space-y-1">
            <Label htmlFor="component-id">Component ID</Label>
            <div className="flex gap-2">
              <Input
                id="component-id"
                value={id}
                onChange={(e) => handleIdChange(e.target.value)}
                placeholder="component-id"
                readOnly={!idUnlocked}
                aria-invalid={id !== '' && !idValid}
                required
              />
              {!idUnlocked && (
                <Button
                  type="button"
                  variant="outline"
                  onClick={() => setIdUnlocked(true)}
                  aria-label="Edit component ID"
                >
                  Edit
                </Button>
              )}
            </div>
            <p
              className={
                id !== '' && !idValid ? 'text-xs text-destructive' : 'text-xs text-muted-foreground'
              }
            >
              Permanent identifier used in URLs, zone assignments, and audit records — lowercase
              letters, digits, and hyphens; must start and end with a letter or digit; max 63
              characters.
            </p>
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
