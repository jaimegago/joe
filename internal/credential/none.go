package credential

import (
	"context"
	"encoding/json"
	"fmt"
)

// noneConfig is the no-credential provider's view of a component config. It
// carries the discriminator and NOTHING else: KindNone's reference is the
// discriminator itself, so there is no locator to parse. The struct exists so
// the Kind participates in the same reflection-derived machinery as every other
// provider (kindConfigStruct, KindLocatorFields) rather than being special-cased
// at each of those sites.
type noneConfig struct {
	CredentialProvider Kind `json:"credential_provider,omitempty"`
}

// NoneProvider is the explicit no-credential provider (D-0150). It resolves to a
// resolution carrying NO credential: every typed accessor reports false, so an
// adapter that needs a credential cannot mistake this for one. Its purpose is
// declarative — a component armed with it has been deliberately, auditably
// authorized to reach its backend unauthenticated.
type NoneProvider struct{}

// NewNoneProvider constructs a NoneProvider. It has no dependencies: there is no
// environment to read and no backend to contact.
func NewNoneProvider() *NoneProvider { return &NoneProvider{} }

// Resolve reaches StageMintSucceeded with no credential attached. There is
// nothing to look up and nothing that can fail, so this never returns an error
// and never reports a failure diagnostic.
func (p *NoneProvider) Resolve(_ context.Context, componentID string, _ json.RawMessage) (*Resolution, error) {
	return &Resolution{Diagnostic: Diagnostic{
		ComponentID: componentID,
		Provider:    KindNone,
		Stage:       StageMintSucceeded,
		OK:          true,
	}}, nil
}

// Probe is a no-op success: there is no credential whose validity a backend
// could confirm, so a resolved no-credential component advances to
// StageConnectivityProbed unconditionally. Whether the backend is reachable
// anonymously is the adapter's Connect to discover, not this provider's.
func (p *NoneProvider) Probe(_ context.Context, res *Resolution) (*Resolution, error) {
	if res == nil {
		return nil, fmt.Errorf("credential: probe requires a resolution")
	}
	if res.Diagnostic.Provider != KindNone {
		return nil, fmt.Errorf("credential: probe requires a no-credential resolution")
	}
	out := *res
	out.Diagnostic.Stage = StageConnectivityProbed
	out.Diagnostic.OK = true
	out.Diagnostic.Reason = ""
	return &out, nil
}

// Describe reports the no-credential provider's descriptor. There is no audience
// or expiry to report.
func (p *NoneProvider) Describe(_ json.RawMessage) (Descriptor, error) {
	return Descriptor{Provider: KindNone}, nil
}

// AvailableReferences reports not-applicable: there is no reference to choose.
// Candidates is non-nil so it serializes as [] not null, matching the contract
// every other provider honours.
func (p *NoneProvider) AvailableReferences(_ string) (References, error) {
	return References{Applicable: false, Candidates: []Candidate{}}, nil
}
