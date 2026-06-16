package credential

import "github.com/jaimegago/joe/internal/store"

// wiring.go is the SINGLE declared source of truth for which component types
// consume the credential-provider seam at Connect (D-0026 / A003-W1). Before this
// registry existed, that fact was knowable only by reading each adapter's Connect.
// It is now a structural property: a literal enumeration, not a runtime scan of
// adapters and not a comment.
//
// Invariant: a component type appears here IFF its adapter's Connect resolves its
// credential through credential.Select + Provider.Resolve. If an adapter is wired
// but missing here (or present but unwired), that is a bug — the registry's tests
// assert the membership directly.
//
// The query functions below are shaped for the future promotion endpoint (a
// separate A003 stream): it will reject promotion of a component whose type is not
// wired. This file declares the fact only; it builds no promotion logic.

// wiredTypes maps each component type wired to the credential-provider seam to the
// provider Kind its adapter selects by default. After A003-W1 the wired set is
// exactly {github, kubernetes, gitlab}: github and gitlab default to the static
// provider (single token), kubernetes to the kubeconfig-exec provider.
var wiredTypes = map[string]Kind{
	store.ComponentTypeGitHub:     KindStatic,
	store.ComponentTypeGitLab:     KindStatic,
	store.ComponentTypeKubernetes: KindKubeconfigExec,
}

// WiredProvider reports the default credential-provider Kind for a component type
// and whether that type is wired to the credential-provider seam at all. The bool
// is the authoritative "is type T wired?" answer; the Kind is the provider its
// adapter selects when the component config carries no discriminator. The future
// promotion endpoint calls this to reject promotion of an unwired type.
func WiredProvider(componentType string) (Kind, bool) {
	k, ok := wiredTypes[componentType]
	return k, ok
}

// IsWired reports whether a component type resolves its credential through the
// credential-provider seam. It is the boolean convenience over WiredProvider.
func IsWired(componentType string) bool {
	_, ok := wiredTypes[componentType]
	return ok
}

// WiredTypes returns the component types wired to the credential-provider seam.
// The order is unspecified; callers that need determinism sort the result. It
// exists so tests can assert the exact wired set without reaching into the map.
func WiredTypes() []string {
	out := make([]string, 0, len(wiredTypes))
	for t := range wiredTypes {
		out = append(out, t)
	}
	return out
}
