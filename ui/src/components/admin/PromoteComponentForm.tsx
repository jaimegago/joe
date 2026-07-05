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
  // kubernetes cluster coordinates, shared by both auth methods.
  const [apiServer, setApiServer] = useState('');
  const [caData, setCaData] = useState('');
  const [namespace, setNamespace] = useState('');
  // The per-component auth method selects the credential source. static-bearer
  // (a long-lived bearer token) is the default; entra-exchange mints a token via
  // an Azure Entra OAuth2 exchange.
  const [authMethod, setAuthMethod] = useState<'static-bearer' | 'entra-exchange'>('static-bearer');
  // static-bearer token source.
  const [bearerEnvVar, setBearerEnvVar] = useState('');
  const [inCluster, setInCluster] = useState(false);
  // entra-exchange source: app-registration coordinates + a client-secret reference.
  const [tenantId, setTenantId] = useState('');
  const [clientId, setClientId] = useState('');
  const [audience, setAudience] = useState('');
  const [clientSecretEnvVar, setClientSecretEnvVar] = useState('');
  // Step-2 confirmation beat.
  const [confirmOpen, setConfirmOpen] = useState(false);

  const verb = component.armed ? 'Re-arm' : 'Promote';

  // With no live candidates the picker has nothing to show, so the compose
  // fallback is the only path; otherwise the operator chooses to compose.
  const inCompose = composeMode || (!candQ.isLoading && candidates.length === 0);
  const composedEnvVar = inCompose ? prefix + label.trim() : pickedEnvVar;
  const labelNotLive =
    inCompose && label.trim() !== '' && !candidates.some((c) => c.env_var_name === composedEnvVar);

  // The at-least-one-of rule is READ from the requirements, not hardcoded, so a
  // backend change to the constraint changes the form's gating with it. For
  // static-bearer it ranges over the two token sources {env_var, in_cluster}.
  const atLeastOne = reqs?.wired
    ? reqs.constraints.find((c) => c.rule === 'at-least-one-of')
    : undefined;
  const fieldPresent = (name: string): boolean => {
    switch (name) {
      case 'in_cluster':
        return inCluster;
      case 'env_var':
        return bearerEnvVar.trim() !== '';
      default:
        return false;
    }
  };
  // static-bearer needs an api-server URL plus at least one token source.
  const tokenSourceReady = atLeastOne
    ? atLeastOne.fields.some(fieldPresent)
    : inCluster || bearerEnvVar.trim() !== '';
  const bearerReady = apiServer.trim() !== '' && tokenSourceReady;

  // entra-exchange needs the cluster api-server plus the full app-registration
  // reference: tenant, client, audience (the scope), and a client-secret variable.
  const entraReady =
    apiServer.trim() !== '' &&
    tenantId.trim() !== '' &&
    clientId.trim() !== '' &&
    audience.trim() !== '' &&
    clientSecretEnvVar.trim() !== '';

  // The kubernetes form (reported kind static-bearer is the wired default) gates on
  // the selected auth method's own readiness.
  const k8sReady = authMethod === 'entra-exchange' ? entraReady : bearerReady;

  const staticReady = inCompose ? label.trim() !== '' && prefix !== '' : pickedEnvVar !== '';

  const canSubmit = kind === 'static' ? staticReady : kind === 'static-bearer' ? k8sReady : false;

  function buildBody(): PromoteRequest | null {
    if (kind === 'static') {
      return { credential_provider: 'static', env_var: composedEnvVar };
    }
    if (kind === 'static-bearer') {
      if (authMethod === 'entra-exchange') {
        return {
          credential_provider: 'entra-exchange',
          auth_method: 'entra-exchange',
          api_server: apiServer.trim(),
          ...(caData.trim() !== '' ? { ca_data: caData.trim() } : {}),
          ...(namespace.trim() !== '' ? { namespace: namespace.trim() } : {}),
          tenant_id: tenantId.trim(),
          client_id: clientId.trim(),
          audience: audience.trim(),
          client_secret_env_var: clientSecretEnvVar.trim(),
        };
      }
      return {
        credential_provider: 'static-bearer',
        auth_method: 'static-bearer',
        api_server: apiServer.trim(),
        ...(caData.trim() !== '' ? { ca_data: caData.trim() } : {}),
        ...(namespace.trim() !== '' ? { namespace: namespace.trim() } : {}),
        ...(bearerEnvVar.trim() !== '' ? { env_var: bearerEnvVar.trim() } : {}),
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
              Arming gives this component a credentialed connection under its assigned zone. You
              supply a <strong>reference</strong> to a credential in Joe&rsquo;s environment — never
              the secret itself.
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
                This component type (<span className="font-mono">{reqs.type}</span>) can&rsquo;t be
                armed yet — Joe has no credential provider wired for it.
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

              {kind === 'static-bearer' && (
                <div className="space-y-3">
                  <div className="space-y-1">
                    <Label htmlFor="promote-apiserver">API-server URL</Label>
                    <Input
                      id="promote-apiserver"
                      value={apiServer}
                      onChange={(e) => setApiServer(e.target.value)}
                      placeholder="https://api.cluster.example.com:6443"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="promote-cadata">
                      CA bundle <span className="text-muted-foreground">(PEM, optional)</span>
                    </Label>
                    <textarea
                      id="promote-cadata"
                      value={caData}
                      onChange={(e) => setCaData(e.target.value)}
                      placeholder="-----BEGIN CERTIFICATE-----&#10;..."
                      rows={4}
                      className="flex w-full rounded-md border border-input bg-background px-3 py-2 font-mono text-xs ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                    />
                    <p className="text-xs text-muted-foreground">
                      The cluster CA certificate, stored inline so the record is self-contained.
                      Leave empty only for a publicly-trusted API server.
                    </p>
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="promote-namespace">
                      Default namespace <span className="text-muted-foreground">(optional)</span>
                    </Label>
                    <Input
                      id="promote-namespace"
                      value={namespace}
                      onChange={(e) => setNamespace(e.target.value)}
                      placeholder="default"
                    />
                  </div>

                  <div className="space-y-1 border-t pt-3">
                    <Label htmlFor="promote-authmethod">Authentication method</Label>
                    <Select
                      value={authMethod}
                      onValueChange={(v) => setAuthMethod(v as 'static-bearer' | 'entra-exchange')}
                    >
                      <SelectTrigger id="promote-authmethod">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="static-bearer">
                          Static bearer token (ServiceAccount / OpenShift)
                        </SelectItem>
                        <SelectItem value="entra-exchange">
                          Entra exchange (AKS — minted via Azure Entra)
                        </SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {authMethod === 'static-bearer' && (
                    <>
                      <div className="space-y-1">
                        <Label htmlFor="promote-bearer-envvar">
                          Bearer-token environment variable
                        </Label>
                        <Input
                          id="promote-bearer-envvar"
                          value={bearerEnvVar}
                          onChange={(e) => setBearerEnvVar(e.target.value)}
                          placeholder="JOE_KUBERNETES_PROD_TOKEN"
                          disabled={inCluster}
                        />
                        <p className="text-xs text-muted-foreground">
                          The variable in Joe&rsquo;s own environment holding the service-account
                          bearer token. Joe stores the name, never the value, and reads it when it
                          connects.
                        </p>
                      </div>
                      <div className="flex items-center gap-2">
                        <Checkbox
                          id="promote-incluster"
                          checked={inCluster}
                          onCheckedChange={(v) => setInCluster(v === true)}
                        />
                        <Label htmlFor="promote-incluster" className="font-normal">
                          Use the in-cluster service-account token instead
                        </Label>
                      </div>
                      {atLeastOne && !bearerReady && (
                        <p className="text-xs text-muted-foreground">
                          Supply an API-server URL and {atLeastOne.message}
                        </p>
                      )}
                    </>
                  )}

                  {authMethod === 'entra-exchange' && (
                    <>
                      <div className="space-y-1">
                        <Label htmlFor="promote-tenant">Azure tenant ID</Label>
                        <Input
                          id="promote-tenant"
                          value={tenantId}
                          onChange={(e) => setTenantId(e.target.value)}
                          placeholder="00000000-0000-0000-0000-000000000000"
                        />
                      </div>
                      <div className="space-y-1">
                        <Label htmlFor="promote-client">Application (client) ID</Label>
                        <Input
                          id="promote-client"
                          value={clientId}
                          onChange={(e) => setClientId(e.target.value)}
                          placeholder="00000000-0000-0000-0000-000000000000"
                        />
                      </div>
                      <div className="space-y-1">
                        <Label htmlFor="promote-audience">Audience (scope)</Label>
                        <Input
                          id="promote-audience"
                          value={audience}
                          onChange={(e) => setAudience(e.target.value)}
                          placeholder="6dae42f8-4368-4678-94ff-3960e28e3630"
                        />
                        <p className="text-xs text-muted-foreground">
                          The AKS resource the minted token is scoped to. Joe requests the{' '}
                          <span className="font-mono">/.default</span> scope for this audience.
                        </p>
                      </div>
                      <div className="space-y-1">
                        <Label htmlFor="promote-client-secret-envvar">
                          Client-secret environment variable
                        </Label>
                        <Input
                          id="promote-client-secret-envvar"
                          value={clientSecretEnvVar}
                          onChange={(e) => setClientSecretEnvVar(e.target.value)}
                          placeholder="JOE_AZURE_APP_SECRET"
                        />
                        <p className="text-xs text-muted-foreground">
                          The variable in Joe&rsquo;s own environment holding the app
                          registration&rsquo;s client secret. Joe stores the name, never the value.
                          One app registration may serve several clusters, so this name may be
                          shared across components.
                        </p>
                      </div>
                      {!entraReady && (
                        <p className="text-xs text-muted-foreground">
                          Supply the API-server URL, tenant ID, client ID, audience, and a
                          client-secret variable.
                        </p>
                      )}
                    </>
                  )}
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
