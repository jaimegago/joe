package k8s

import (
	"encoding/json"
	"fmt"
)

// AuthMethodStaticBearer is the original kubernetes transport auth method: a
// long-lived bearer token applied to a hand-built *rest.Config. It is one value
// the per-component auth_method discriminator carries and maps to
// credential.KindStaticBearer (agent-identity-doc-02, D-0060).
const AuthMethodStaticBearer = "static-bearer"

// AuthMethodEntraExchange is the second kubernetes transport auth method
// (agent-identity-doc-03, D-0063): Joe MINTS a short-lived bearer token via an
// Azure Entra OAuth2 client-credentials exchange (for AKS) and applies it to the
// same hand-built *rest.Config. It maps to credential.KindEntraExchange. The
// stored cluster-coordinate fields (api_server, ca_data, namespace) are unchanged
// from static-bearer; only the credential source differs.
const AuthMethodEntraExchange = "entra-exchange"

// Config holds the Kubernetes-specific cluster coordinates the adapter turns into
// a *rest.Config by hand (agent-identity-doc-02): the api-server URL is the
// rest.Config host, the inline CA bundle is the TLS CA data (stored inline so the
// record is self-contained for a remote fleet — no on-disk CA path), the optional
// default namespace, and the auth_method discriminator that selects the credential
// Kind resolving the bearer token. There is NO kubeconfig path, context, or
// in-cluster flag here: the bearer token's locator (env_var or in_cluster) is a
// credential-provider concern read separately, never a kubeconfig ingestion.
type Config struct {
	APIServer  string `json:"api_server,omitempty"`  // api-server URL → rest.Config.Host
	CAData     string `json:"ca_data,omitempty"`     // inline CA bundle (PEM) → TLSClientConfig.CAData
	Namespace  string `json:"namespace,omitempty"`   // optional default namespace
	AuthMethod string `json:"auth_method,omitempty"` // transport auth method discriminator (static-bearer today)
}

// ParseConfig extracts a K8s Config from raw JSON component config.
func ParseConfig(raw json.RawMessage) (Config, error) {
	var cfg Config
	if len(raw) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse k8s config: %w", err)
	}
	return cfg, nil
}
