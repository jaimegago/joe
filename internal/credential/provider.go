package credential

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Provider is the credential-provider abstraction (D-0026): exactly three
// operations, no Refresh and no Rotate. A provider selects WHICH credential
// source a component's adapter consumes; returning a value is the degenerate
// case the static provider uses internally.
type Provider interface {
	// Resolve produces the credential source the adapter will consume from the
	// component's decoded config, plus the typed staged result. It reaches at
	// most StageMintSucceeded — never StageConnectivityProbed — because Resolve
	// never contacts the backend.
	Resolve(ctx context.Context, componentID string, config json.RawMessage) (*Resolution, error)

	// Probe attempts connectivity against the backend for an already-resolved
	// source and returns a result reaching StageConnectivityProbed (or a failure
	// stage with a non-sensitive reason). Probe is the ONLY operation that touches
	// the backend and is OPTIONAL to invoke — its separateness is what makes
	// lazy-connectivity ("minted, not yet proven") a legal state.
	Probe(ctx context.Context, res *Resolution) (*Resolution, error)

	// Describe is pure and side-effect-free: it returns the non-sensitive
	// descriptor (provider kind, audience, context name, expiry when known) for
	// UI rendering, without calling Resolve.
	Describe(config json.RawMessage) (Descriptor, error)

	// AvailableReferences answers, in the provider's OWN terms, which credential
	// references an admin may choose for a component of componentType right now —
	// the live candidate set behind the promotion reference picker. The static
	// provider enumerates the process environment scoped to the type's
	// JOE_<SEGMENT>_ prefix, returning each match as a {label, env_var_name}
	// candidate (NAMES ONLY — never a value, never the environment at large). A
	// provider whose reference is not an enumerable set (kubeconfig-exec: a file
	// path) returns Applicable=false with no candidates. It takes componentType —
	// not config — because the answer is type-scoped, not config-scoped, and it
	// needs no store handle: the caller resolves the type and hands it in. This is
	// the seam that lets a future DB/external-manager provider answer the same
	// question against its own backing store without the endpoint or form changing.
	AvailableReferences(componentType string) (References, error)
}

// Descriptor is the non-sensitive, config-derived fact a provider reports for UI
// rendering, distinct from the live last-resolution outcome.
type Descriptor struct {
	Provider  Kind       `json:"provider"`
	Audience  string     `json:"audience,omitempty"`
	Context   string     `json:"context,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// discriminator reads only the provider-kind field from a component config.
type discriminator struct {
	CredentialProvider Kind `json:"credential_provider"`
}

// KindFromConfig reads the "credential_provider" discriminator from a component
// config. When absent (or the config is empty) it defaults to KindStatic,
// preserving existing components' behavior.
func KindFromConfig(config json.RawMessage) (Kind, error) {
	if len(config) == 0 {
		return KindStatic, nil
	}
	var d discriminator
	if err := json.Unmarshal(config, &d); err != nil {
		return "", fmt.Errorf("credential: parse provider kind: %w", err)
	}
	if d.CredentialProvider == "" {
		return KindStatic, nil
	}
	return d.CredentialProvider, nil
}

// Select returns the Provider chosen by the component config's
// "credential_provider" discriminator. This is the only place selection happens;
// resolution logic never branches on static-vs-refreshing past this point.
func Select(config json.RawMessage) (Provider, error) {
	kind, err := KindFromConfig(config)
	if err != nil {
		return nil, err
	}
	return ProviderForKind(kind)
}

// ProviderForKind constructs the Provider for a known provider Kind, with no
// config to read a discriminator from. It is the constructor the promotion
// reference picker uses: the candidates endpoint resolves a component type to its
// wired Kind (credential.WiredProvider) and asks that Kind's provider for its
// AvailableReferences. Select is the config-driven wrapper over this.
func ProviderForKind(kind Kind) (Provider, error) {
	switch kind {
	case KindStatic:
		return NewStaticProvider(), nil
	case KindKubeconfigExec:
		return NewKubeconfigExecProvider(), nil
	default:
		return nil, fmt.Errorf("credential: unknown provider kind %q", kind)
	}
}
