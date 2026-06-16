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
// provider Kind its adapter selects by default. kubernetes selects the
// kubeconfig-exec provider; every other wired type defaults to the static provider
// (a single long-lived token resolved through StaticValue).
//
// A003-W1 seeded the set with {github, kubernetes, gitlab}. A003-W2 closed out the
// remaining static-token adapters: the HTTP telemetry backends (prometheus, mimir,
// loki, tempo, jaeger), the alerting backends (alertmanager, pagerduty, grafana),
// the gitops/security single-token backends (argocd, falco, splunk, dynatrace,
// newrelic). mimir shares the prometheus adapter, so both type strings map here
// even though one adapter Connect serves them.
//
// Deliberately ABSENT (re-verified at W2, not wired): git (auth_type-discriminated
// ssh-key-path / http-token, not a single static token), datadog (api_key + app_key
// pair), oci_registry, dockerhub, and artifactory (registry-auth shape — a token-or-
// basic-auth pair, not an unambiguous single static token: oci_registry/dockerhub
// carry username/password and artifactory is bimodal between an X-JFrog-Art-Api
// single-token header and a username basic-auth fallback, so all three need a
// dedicated registry credential provider rather than the static seam), helm and
// nginx-ingress (kubeconfig-shaped, not a static token), terraform and envoy (no
// credential — local state file / unauthenticated admin API). The registry is the
// promotion endpoint's reject-unwired authority, so it lists only types whose
// adapter Connect actually resolves through the seam.
var wiredTypes = map[string]Kind{
	store.ComponentTypeGitHub:       KindStatic,
	store.ComponentTypeGitLab:       KindStatic,
	store.ComponentTypeKubernetes:   KindKubeconfigExec,
	store.ComponentTypePrometheus:   KindStatic,
	store.ComponentTypeMimir:        KindStatic,
	store.ComponentTypeLoki:         KindStatic,
	store.ComponentTypeTempo:        KindStatic,
	store.ComponentTypeJaeger:       KindStatic,
	store.ComponentTypeSplunk:       KindStatic,
	store.ComponentTypeDynatrace:    KindStatic,
	store.ComponentTypeNewRelic:     KindStatic,
	store.ComponentTypeAlertmanager: KindStatic,
	store.ComponentTypePagerDuty:    KindStatic,
	store.ComponentTypeGrafana:      KindStatic,
	store.ComponentTypeFalco:        KindStatic,
	store.ComponentTypeArgoCd:       KindStatic,
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
