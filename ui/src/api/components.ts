import { apiClient } from './client';
import {
  ComponentSchema,
  ComponentTypesSchema,
  CreatedComponentSchema,
  PromotionRequirementsSchema,
  PromotionCandidatesSchema,
  PromoteResponseSchema,
  TestComponentResponseSchema,
} from './schemas';
import { z } from 'zod';
import type {
  Component,
  ComponentTypeDescriptor,
  CreatedComponent,
  RegistrationConfig,
  PromotionRequirements,
  PromotionCandidates,
  PromoteResponse,
} from './types';

export function fetchComponents(): Promise<Component[]> {
  return apiClient
    .get<unknown>('/api/v1/components')
    .then((r) => z.object({ components: z.array(ComponentSchema) }).parse(r).components);
}

// fetchComponentTypes returns one registration descriptor per registrable type
// from the backend (GET /api/v1/component-types): the type's name and its
// declared routing fields. The registration form renders its type selector and
// each type's fields from it, so it hardcodes neither the set nor any field.
export function fetchComponentTypes(): Promise<ComponentTypeDescriptor[]> {
  return apiClient
    .get<unknown>('/api/v1/component-types')
    .then((r) => ComponentTypesSchema.parse(r).descriptors);
}

// createComponent registers a new, inert component via the admin-gated governed
// create endpoint (POST /api/v1/components). It carries id/type/name plus the
// type's NON-CREDENTIAL routing config — only the fields the type's descriptor
// declares, each typed as declared (D-0168). Never a credential: credentials
// enter at a separate promotion step, and the backend refuses a credential or a
// secret here. The component lands credential-less in the unassigned zone under
// the read-only floor and can do nothing until it is separately promoted and
// zone-assigned.
export function createComponent(input: {
  id: string;
  type: string;
  name: string;
  config?: RegistrationConfig;
}): Promise<CreatedComponent> {
  return apiClient
    .post<unknown>('/api/v1/components', input)
    .then((r) => CreatedComponentSchema.parse(r));
}

export function testComponent(id: string): Promise<{ ok: boolean; message?: string }> {
  return apiClient
    .post<unknown>(`/api/v1/components/${encodeURIComponent(id)}/test`, {})
    .then((r) => TestComponentResponseSchema.parse(r));
}

export function deleteComponent(id: string): Promise<void> {
  return apiClient.delete<void>(`/api/v1/components/${encodeURIComponent(id)}`);
}

// fetchPromotionRequirements describes the SHAPE of the credential reference an
// admin must supply to arm this component (which locator fields, which
// cross-field rules) — or, for a type with no wired provider, the set of types
// that CAN be armed. Drives the provider-conditional promotion form.
export function fetchPromotionRequirements(id: string): Promise<PromotionRequirements> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promotion-requirements`)
    .then((r) => PromotionRequirementsSchema.parse(r));
}

// fetchPromotionCandidates lists the LIVE credential references the admin can
// choose for this component right now (for the static provider, the env var
// names matching the type's prefix that are currently set in Joe's
// environment). Names/labels only — never a credential value.
export function fetchPromotionCandidates(id: string): Promise<PromotionCandidates> {
  return apiClient
    .get<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promotion-candidates`)
    .then((r) => PromotionCandidatesSchema.parse(r));
}

// PromoteRequest is a credential REFERENCE — the provider discriminator plus
// the relevant locator fields. It NEVER carries an inline `value`: the armed
// record points Joe at a credential in its own environment, it does not store
// the secret. The static reference is an env_var name; the kubernetes reference
// is the cluster coordinates (api_server, ca_data, namespace) plus auth_method
// and a credential source that depends on the method. The static-bearer method's
// source is a bearer-token env_var name OR the in-cluster service-account token;
// the entra-exchange method's source is tenant_id/client_id/audience plus a
// client_secret_env_var reference (the secret is resolved by name, never stored,
// and uses a DISTINCT field so shared Azure app registrations are allowed). There
// is no kubeconfig ingestion.
// A git component additionally takes the explicit `none` kind, which carries NO
// locator at all: the discriminator IS the reference. It is a deliberate,
// audited statement that this component may reach its repository with no
// credential, not a defaulted absence — a public repository still requires
// promotion before it can be read.
export type PromoteRequest =
  | { credential_provider: 'static'; env_var: string }
  | { credential_provider: 'none' }
  | {
      credential_provider: 'static-bearer';
      auth_method: 'static-bearer';
      api_server: string;
      ca_data?: string;
      namespace?: string;
      env_var?: string;
      in_cluster?: boolean;
    }
  | {
      credential_provider: 'entra-exchange';
      auth_method: 'entra-exchange';
      api_server: string;
      ca_data?: string;
      namespace?: string;
      tenant_id: string;
      client_id: string;
      audience: string;
      client_secret_env_var: string;
    };

// promoteComponent transitions an inert component to armed (or rotates an
// already-armed component's reference) via the admin-gated, audited promote
// endpoint. The response is outcome-only and never echoes the reference.
export function promoteComponent(id: string, body: PromoteRequest): Promise<PromoteResponse> {
  return apiClient
    .post<unknown>(`/api/v1/components/${encodeURIComponent(id)}/promote`, body)
    .then((r) => PromoteResponseSchema.parse(r));
}
