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
import { Checkbox } from '@/components/ui/checkbox';
import { Badge } from '@/components/ui/badge';
import { ConfirmDialog } from '@/components/common/ConfirmDialog';
import { LoadingSpinner } from '@/components/common/LoadingSpinner';
import { fetchPromotionRequirements, fetchPromotionCandidates } from '@/api/components';
import type { PromoteRequest } from '@/api/components';
import type { Component } from '@/api/types';

interface PromoteComponentFormProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  component: Component;
  onSubmit: (body: PromoteRequest) => void;
  isLoading?: boolean;
}

// PromoteComponentForm is the operator-facing arm surface (A002): an admin
// supplies a credential REFERENCE — never a secret — to transition an inert
// component to armed. It is provider-conditional on the wired credential kind
// (read live from promotion-requirements) and two-step: collect the reference,
// then a distinct arm-confirmation beat about the PRIVILEGE before the POST
// fires. It offers no secret/value field by construction.
export function PromoteComponentForm({
  open,
  onOpenChange,
  component,
  onSubmit,
  isLoading,
}: PromoteComponentFormProps) {
  // The requirements describe the form to render (which provider, which
  // fields, which cross-field rules). Fetched once the dialog is opened.
  const reqQ = useQuery({
    queryKey: ['promotion-requirements', component.id],
    queryFn: () => fetchPromotionRequirements(component.id),
    enabled: open,
  });
  const reqs = reqQ.data;
  const wired = reqs?.wired === true;
  const kind = reqs?.wired ? reqs.kind : undefined;

  // Live candidate references are only an enumerable set for the static
  // provider; kubeconfig-exec answers not-applicable, so don't fetch them.
  const candQ = useQuery({
    queryKey: ['promotion-candidates', component.id],
    queryFn: () => fetchPromotionCandidates(component.id),
    enabled: open && wired && kind === 'static',
  });
  const candData = candQ.data?.wired ? candQ.data : null;
  const candidates = candData?.candidates ?? [];
  const prefix = candData?.prefix ?? '';

  // Step-1 reference state.
  const [composeMode, setComposeMode] = useState(false);
  const [pickedEnvVar, setPickedEnvVar] = useState('');
  const [label, setLabel] = useState('');
  const [kubeconfig, setKubeconfig] = useState('');
  const [context, setContext] = useState('');
  const [inCluster, setInCluster] = useState(false);
  // Step-2 confirmation beat.
  const [confirmOpen, setConfirmOpen] = useState(false);

  const verb = component.armed ? 'Re-arm' : 'Promote';

  // With no live candidates the picker has nothing to show, so the compose
  // fallback is the only path; otherwise the operator chooses to compose.
  const inCompose = composeMode || (!candQ.isLoading && candidates.length === 0);
  const composedEnvVar = inCompose ? prefix + label.trim() : pickedEnvVar;
  const labelNotLive =
    inCompose &&
    label.trim() !== '' &&
    !candidates.some((c) => c.env_var_name === composedEnvVar);

  // The at-least-one-of rule is READ from the requirements, not hardcoded, so a
  // backend change to the constraint changes the form's gating with it.
  const atLeastOne = reqs?.wired
    ? reqs.constraints.find((c) => c.rule === 'at-least-one-of')
    : undefined;
  const fieldPresent = (name: string): boolean => {
    switch (name) {
      case 'in_cluster':
        return inCluster;
      case 'kubeconfig':
        return kubeconfig.trim() !== '';
      case 'context':
        return context.trim() !== '';
      default:
        return false;
    }
  };
  const kubeReady = atLeastOne
    ? atLeastOne.fields.some(fieldPresent)
    : inCluster || kubeconfig.trim() !== '';

  const staticReady = inCompose ? label.trim() !== '' && prefix !== '' : pickedEnvVar !== '';

  const canSubmit =
    kind === 'static' ? staticReady : kind === 'kubeconfig-exec' ? kubeReady : false;

  function buildBody(): PromoteRequest | null {
    if (kind === 'static') {
      return { credential_provider: 'static', env_var: composedEnvVar };
    }
    if (kind === 'kubeconfig-exec') {
      return {
        credential_provider: 'kubeconfig-exec',
        ...(kubeconfig.trim() !== '' ? { kubeconfig: kubeconfig.trim() } : {}),
        ...(context.trim() !== '' ? { context: context.trim() } : {}),
        ...(inCluster ? { in_cluster: true } : {}),
      };
    }
    return null;
  }

  function handleContinue(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;
    setConfirmOpen(true);
  }

  function handleConfirm() {
    const body = buildBody();
    if (body) onSubmit(body);
  }

  return (
    <>
      <Dialog open={open && !confirmOpen} onOpenChange={onOpenChange}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {verb} {component.id}
            </DialogTitle>
            <DialogDescription>
              Arming gives this component a credentialed connection under its assigned zone.
              You supply a <strong>reference</strong> to a credential in Joe&rsquo;s
              environment — never the secret itself.
            </DialogDescription>
          </DialogHeader>

          {reqQ.isLoading && (
            <div className="flex justify-center py-6">
              <LoadingSpinner />
            </div>
          )}

          {reqQ.isError && (
            <p className="rounded bg-destructive/10 px-2 py-1 text-sm text-destructive">
              Couldn&rsquo;t load promotion requirements: {reqQ.error.message}
            </p>
          )}

          {/* Unwired type: surface that it can't be armed yet + which types can.
              No form fields are rendered. */}
          {reqs && !reqs.wired && (
            <div className="space-y-3">
              <p className="text-sm">
                This component type (<span className="font-mono">{reqs.type}</span>) can&rsquo;t
                be armed yet — Joe has no credential provider wired for it.
              </p>
              <div>
                <p className="mb-1 text-xs text-muted-foreground">Types that can be armed</p>
                <div className="flex flex-wrap gap-1">
                  {reqs.armable_types.map((t) => (
                    <Badge key={t} variant="secondary">
                      {t}
                    </Badge>
                  ))}
                </div>
              </div>
              <DialogFooter>
                <Button variant="outline" onClick={() => onOpenChange(false)}>
                  Close
                </Button>
              </DialogFooter>
            </div>
          )}

          {reqs && reqs.wired && (
            <form onSubmit={handleContinue} className="space-y-4">
              {kind === 'static' && (
                <div className="space-y-3">
                  {candidates.length > 0 && !inCompose && (
                    <div className="space-y-1">
                      <Label htmlFor="promote-ref">Credential reference</Label>
                      <Select value={pickedEnvVar} onValueChange={setPickedEnvVar}>
                        <SelectTrigger id="promote-ref">
                          <SelectValue
                            placeholder={
                              candQ.isLoading ? 'Loading references…' : 'Choose a reference'
                            }
                          />
                        </SelectTrigger>
                        <SelectContent>
                          {candidates.map((c) => (
                            <SelectItem key={c.env_var_name} value={c.env_var_name}>
                              {c.label}
                            </SelectItem>
                          ))}
                        </SelectContent>
                      </Select>
                    </div>
                  )}

                  {candidates.length > 0 && !inCompose ? (
                    <Button
                      type="button"
                      variant="link"
                      className="h-auto p-0 text-xs"
                      onClick={() => setComposeMode(true)}
                    >
                      Don&rsquo;t see it? Enter a label
                    </Button>
                  ) : (
                    <div className="space-y-1">
                      <Label htmlFor="promote-label">Reference label</Label>
                      <Input
                        id="promote-label"
                        value={label}
                        onChange={(e) => setLabel(e.target.value)}
                        placeholder="PROD"
                      />
                      {prefix !== '' && (
                        <p className="text-xs text-muted-foreground">
                          Resolves to{' '}
                          <span className="font-mono">
                            {prefix}
                            {label.trim() || '<label>'}
                          </span>
                        </p>
                      )}
                      {labelNotLive && (
                        <p className="text-xs text-yellow-700">
                          <span className="font-mono">{composedEnvVar}</span> isn&rsquo;t currently
                          set in Joe&rsquo;s environment. You can still arm — set the variable where
                          Joe runs before it connects.
                        </p>
                      )}
                      {candidates.length > 0 && (
                        <Button
                          type="button"
                          variant="link"
                          className="h-auto p-0 text-xs"
                          onClick={() => {
                            setComposeMode(false);
                            setLabel('');
                          }}
                        >
                          Choose from existing references instead
                        </Button>
                      )}
                    </div>
                  )}

                  <p className="text-xs text-muted-foreground">
                    This names the credential Joe will use to reach this component. Joe reads it
                    from a variable in its own environment — where Joe runs — when it connects, so
                    that variable must exist there.
                  </p>
                </div>
              )}

              {kind === 'kubeconfig-exec' && (
                <div className="space-y-3">
                  <div className="space-y-1">
                    <Label htmlFor="promote-kubeconfig">Kubeconfig path</Label>
                    <Input
                      id="promote-kubeconfig"
                      value={kubeconfig}
                      onChange={(e) => setKubeconfig(e.target.value)}
                      placeholder="/etc/joe/kubeconfig"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="promote-context">
                      Context <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                      id="promote-context"
                      value={context}
                      onChange={(e) => setContext(e.target.value)}
                      placeholder="prod-cluster"
                    />
                  </div>
                  <div className="flex items-center gap-2">
                    <Checkbox
                      id="promote-incluster"
                      checked={inCluster}
                      onCheckedChange={(v) => setInCluster(v === true)}
                    />
                    <Label htmlFor="promote-incluster" className="font-normal">
                      Use the in-cluster service account
                    </Label>
                  </div>
                  {atLeastOne && !kubeReady && (
                    <p className="text-xs text-muted-foreground">{atLeastOne.message}</p>
                  )}
                  <p className="text-xs text-muted-foreground">
                    This points Joe at the credential it will use to reach this component. Joe
                    reads the kubeconfig (or in-cluster identity) from its own environment — where
                    Joe runs — when it connects, so it must exist there.
                  </p>
                </div>
              )}

              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={!canSubmit || (isLoading ?? false)}>
                  Continue
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      {/* The arm-confirmation beat: about the PRIVILEGE, not the reference. It
          names the component + provider kind and never echoes the locator
          value. Only on confirm does the POST fire. */}
      <ConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        title={`${verb} ${component.id}?`}
        description={`${
          component.armed ? 'Re-arming' : 'Arming'
        } grants ${component.name} a credentialed connection under its assigned zone using the ${
          kind ?? ''
        } provider. This is a privileged, audited change.`}
        confirmLabel={verb}
        onConfirm={handleConfirm}
      />
    </>
  );
}
