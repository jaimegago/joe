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
import { Checkbox } from '@/components/ui/checkbox';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { fetchComponentTypes, fetchComponents } from '@/api/components';
import type { Component, RegistrationConfig, RegistrationField } from '@/api/types';

interface ComponentRegisterFormData {
  id: string;
  type: string;
  name: string;
  // Non-credential routing config: only the fields the selected type's
  // descriptor declares, each typed as declared. Omitted when no field is set.
  config?: RegistrationConfig;
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

// Client-side mirrors of the registration contract's shape rules
// (componentgov.ValidateRegistrationConfig, D-0168). The server is
// authoritative; these give immediate feedback and gate submit.
const INTEGER_PATTERN = /^-?\d+$/;
const ABSOLUTE_URL_PATTERN = /^[a-zA-Z][a-zA-Z0-9+.-]*:\/\/./;

function isAbsoluteUrl(value: string): boolean {
  if (!ABSOLUTE_URL_PATTERN.test(value)) return false;
  try {
    new URL(value);
    return true;
  } catch {
    return false;
  }
}

// fieldError says why a non-empty text value does not have its field's
// declared shape, or returns null when it does.
function fieldError(field: RegistrationField, value: string): string | null {
  if (field.type === 'url' && !isAbsoluteUrl(value)) {
    return 'Must be an absolute URL, such as https://host:port.';
  }
  if (field.type === 'integer' && !INTEGER_PATTERN.test(value)) {
    return 'Must be a whole number.';
  }
  return null;
}

// fieldHint is the line under a field: its own help, and the adapter's default
// for an optional field that has one.
function fieldHint(field: RegistrationField): string | null {
  const parts: string[] = [];
  if (field.help) parts.push(field.help);
  if (field.phase === 'optional' && field.default !== undefined && field.type !== 'boolean') {
    parts.push(`Defaults to ${String(field.default)}.`);
  }
  return parts.length > 0 ? parts.join(' ') : null;
}

const TEXTAREA_CLASS =
  'flex w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs ring-offset-background placeholder:text-muted-foreground focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50';

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
  // Routing-field values for the selected type, keyed by field name: text for
  // every field but a boolean's checkbox. Reset whenever the type changes.
  const [values, setValues] = useState<Record<string, string | boolean>>({});

  // The type selector and every type's fields come from the backend's
  // registration descriptors — never a hardcoded TS list or a per-type branch.
  // Only fetched once the dialog is opened.
  const typesQ = useQuery({
    queryKey: ['component-types'],
    queryFn: fetchComponentTypes,
    enabled: open,
  });
  const descriptors = typesQ.data ?? [];
  const descriptor = descriptors.find((d) => d.name === type);
  const allFields = descriptor?.fields ?? [];
  // Fields required at promotion are collected by the Promote form, not here.
  const fields = allFields.filter((f) => f.phase !== 'promotion');
  const promotionFields = allFields.filter((f) => f.phase === 'promotion');
  const needsComponents = fields.some((f) => (f.refers_to ?? []).length > 0);

  // A field that names another component offers the registered components of
  // the types it may refer to, so the operator picks one rather than typing an
  // ID. Only fetched when such a field is showing.
  const componentsQ = useQuery({
    queryKey: ['components'],
    queryFn: fetchComponents,
    enabled: open && needsComponents,
  });

  function textValue(field: RegistrationField): string {
    const v = values[field.name];
    return typeof v === 'string' ? v.trim() : '';
  }

  const fieldsValid = fields.every((f) => {
    if (f.type === 'boolean') return true;
    const v = textValue(f);
    if (v === '') return f.phase !== 'registration';
    return fieldError(f, v) === null;
  });

  const idValid = isValidComponentId(id);

  // Live validity: all three base fields are required by the governed create
  // endpoint, the ID must satisfy the format rule the backend enforces, and the
  // type's registration-required fields must be present and well-formed.
  const canSubmit = idValid && type !== '' && name.trim() !== '' && fieldsValid;

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

  function handleTypeChange(value: string) {
    setType(value);
    setValues({});
  }

  function setValue(field: string, value: string | boolean) {
    setValues((prev) => ({ ...prev, [field]: value }));
  }

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    // Each set field is sent in its declared JSON type: a number for an
    // integer, true for a checked boolean, a trimmed string otherwise. An empty
    // field is left out — absent and empty mean the same to the server, and the
    // adapter then uses its default. An unchecked boolean is false, the zero
    // value, and is likewise left out.
    const config: RegistrationConfig = {};
    for (const f of fields) {
      if (f.type === 'boolean') {
        if (values[f.name] === true) config[f.name] = true;
        continue;
      }
      const v = textValue(f);
      if (v === '') continue;
      config[f.name] = f.type === 'integer' ? Number.parseInt(v, 10) : v;
    }
    const base = { id, type, name: name.trim() };
    // A type with nothing set submits no config at all, exactly the payload a
    // config-less registration always sent (D-0057).
    onSubmit(Object.keys(config).length > 0 ? { ...base, config } : base);
  }

  function renderField(field: RegistrationField) {
    const inputId = `register-field-${field.name}`;
    const required = field.phase === 'registration';
    const label = (
      <Label htmlFor={inputId}>
        {field.label}
        {required ? (
          <span className="text-destructive" aria-hidden="true">
            {' '}
            *
          </span>
        ) : (
          <span className="text-muted-foreground"> (optional)</span>
        )}
      </Label>
    );
    const hint = fieldHint(field);

    if (field.type === 'boolean') {
      const checked =
        values[field.name] === true || (values[field.name] === undefined && field.default === true);
      return (
        <div key={field.name} className="space-y-1">
          <div className="flex items-center gap-2">
            <Checkbox
              id={inputId}
              checked={checked}
              onCheckedChange={(c) => setValue(field.name, c === true)}
            />
            {label}
          </div>
          {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
        </div>
      );
    }

    const refersTo = field.refers_to ?? [];
    if (refersTo.length > 0) {
      const candidates = (componentsQ.data ?? []).filter((c: Component) =>
        refersTo.includes(c.type)
      );
      const current = values[field.name];
      return (
        <div key={field.name} className="space-y-1">
          {label}
          <Select
            value={typeof current === 'string' ? current : ''}
            onValueChange={(v) => setValue(field.name, v)}
          >
            <SelectTrigger id={inputId}>
              <SelectValue
                placeholder={
                  componentsQ.isLoading
                    ? 'Loading components…'
                    : candidates.length === 0
                      ? `No ${refersTo.join(' or ')} components registered`
                      : 'None'
                }
              />
            </SelectTrigger>
            <SelectContent>
              {candidates.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.name} ({c.type})
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
        </div>
      );
    }

    const raw = values[field.name];
    const text = typeof raw === 'string' ? raw : '';
    const error = text.trim() === '' ? null : fieldError(field, text.trim());
    const placeholder =
      field.type === 'url'
        ? field.default !== undefined
          ? String(field.default)
          : 'https://…'
        : '';
    return (
      <div key={field.name} className="space-y-1">
        {label}
        {field.multiline ? (
          <textarea
            id={inputId}
            value={text}
            onChange={(e) => setValue(field.name, e.target.value)}
            rows={4}
            required={required}
            aria-invalid={error !== null}
            className={TEXTAREA_CLASS}
          />
        ) : (
          <Input
            id={inputId}
            value={text}
            onChange={(e) => setValue(field.name, e.target.value)}
            inputMode={field.type === 'integer' ? 'numeric' : field.type === 'url' ? 'url' : 'text'}
            placeholder={placeholder}
            required={required}
            aria-invalid={error !== null}
          />
        )}
        {error ? (
          <p className="text-xs text-destructive">{error}</p>
        ) : (
          hint && <p className="text-xs text-muted-foreground">{hint}</p>
        )}
      </div>
    );
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
            <Select value={type} onValueChange={handleTypeChange}>
              <SelectTrigger id="component-type">
                <SelectValue placeholder={typesQ.isLoading ? 'Loading types…' : 'Select a type'} />
              </SelectTrigger>
              <SelectContent>
                {descriptors.map((d) => (
                  <SelectItem key={d.name} value={d.name}>
                    {d.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          {fields.map(renderField)}
          {promotionFields.length > 0 && (
            <p className="text-xs text-muted-foreground">
              Supplied at promotion: {promotionFields.map((f) => f.label).join(', ')}.
            </p>
          )}
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
