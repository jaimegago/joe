package core

import (
	"context"
	"fmt"

	"github.com/jaimegago/joe/internal/llm"
)

// CertManagerCertCRDTypes maps cert-manager certificate resource kinds to their full K8s identifiers.
var CertManagerCertCRDTypes = map[string]string{
	"Certificate":        "certificates.cert-manager.io",
	"CertificateRequest": "certificaterequests.cert-manager.io",
}

// CertManagerIssuerCRDTypes maps cert-manager issuer kinds to their full K8s identifiers.
var CertManagerIssuerCRDTypes = map[string]string{
	"Issuer":        "issuers.cert-manager.io",
	"ClusterIssuer": "clusterissuers.cert-manager.io",
}

// CertManagerK8sClient defines what cert-manager tools need from the K8s client.
type CertManagerK8sClient interface {
	K8sListResources(ctx context.Context, sourceID, resource, namespace string) ([]map[string]any, error)
}

// --- certmanager_certs ---

// CertManagerCertsTool lists cert-manager Certificate and CertificateRequest objects.
type CertManagerCertsTool struct {
	Client CertManagerK8sClient
}

func NewCertManagerCertsTool(c CertManagerK8sClient) *CertManagerCertsTool {
	return &CertManagerCertsTool{Client: c}
}

func (t *CertManagerCertsTool) Name() string { return "certmanager_certs" }

func (t *CertManagerCertsTool) Description() string {
	return "List cert-manager Certificate resources with expiry dates and readiness status. " +
		"Shows DNS names, secret name, issuer reference, and whether the certificate is Ready. " +
		"Use to check certificate health, upcoming renewals, or failed issuances. " +
		"Use source_id of a Kubernetes source where cert-manager is installed. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *CertManagerCertsTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where cert-manager is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter certificates. Omit for all namespaces.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *CertManagerCertsTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	namespace, _ := args["namespace"].(string)

	certs, err := t.Client.K8sListResources(ctx, sourceID, CertManagerCertCRDTypes["Certificate"], namespace)
	if err != nil {
		return nil, fmt.Errorf("list certificates: %w", err)
	}
	if certs == nil {
		certs = []map[string]any{}
	}

	return map[string]any{
		"certificates": extractCertSummaries(certs),
		"count":        len(certs),
		"namespace":    namespace,
		"source_id":    sourceID,
	}, nil
}

// extractCertSummaries extracts concise certificate summaries from Certificate objects.
func extractCertSummaries(certs []map[string]any) []map[string]any {
	var out []map[string]any
	for _, c := range certs {
		summary := map[string]any{}
		if meta, ok := c["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
			summary["namespace"] = meta["namespace"]
		}
		if spec, ok := c["spec"].(map[string]any); ok {
			summary["dns_names"] = spec["dnsNames"]
			summary["secret_name"] = spec["secretName"]
			if issuerRef, ok := spec["issuerRef"].(map[string]any); ok {
				summary["issuer"] = issuerRef["name"]
				summary["issuer_kind"] = issuerRef["kind"]
			}
		}
		if status, ok := c["status"].(map[string]any); ok {
			summary["not_after"] = status["notAfter"]
			summary["not_before"] = status["notBefore"]
			summary["renewal_time"] = status["renewalTime"]
			if conditions, ok := status["conditions"].([]any); ok {
				for _, cond := range conditions {
					if cm, ok := cond.(map[string]any); ok && cm["type"] == "Ready" {
						summary["ready"] = cm["status"]
						summary["reason"] = cm["reason"]
						summary["message"] = cm["message"]
						break
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out
}

// --- certmanager_issuers ---

// CertManagerIssuersTool lists cert-manager Issuer and ClusterIssuer objects.
type CertManagerIssuersTool struct {
	Client CertManagerK8sClient
}

func NewCertManagerIssuersTool(c CertManagerK8sClient) *CertManagerIssuersTool {
	return &CertManagerIssuersTool{Client: c}
}

func (t *CertManagerIssuersTool) Name() string { return "certmanager_issuers" }

func (t *CertManagerIssuersTool) Description() string {
	return "List cert-manager Issuer and ClusterIssuer resources with their status. " +
		"Shows issuer type (ACME, CA, Vault, Venafi), readiness, and error conditions. " +
		"Use to check if certificate issuers are healthy and properly configured. " +
		"Use source_id of a Kubernetes source where cert-manager is installed. " +
		"If you don't know the source_id, call list_sources first."
}

func (t *CertManagerIssuersTool) Parameters() llm.ParameterSchema {
	return llm.ParameterSchema{
		Type: "object",
		Properties: map[string]llm.Property{
			"source_id": {
				Type:        "string",
				Description: "ID of the Kubernetes source (where cert-manager is installed).",
			},
			"namespace": {
				Type:        "string",
				Description: "Namespace to filter namespaced Issuers. ClusterIssuers are always included.",
			},
		},
		Required: []string{"source_id"},
	}
}

func (t *CertManagerIssuersTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	sourceID, ok := args["source_id"].(string)
	if !ok || sourceID == "" {
		return nil, fmt.Errorf("missing required parameter: source_id")
	}
	namespace, _ := args["namespace"].(string)

	result := map[string][]map[string]any{}

	// Namespaced Issuers.
	if issuers, err := t.Client.K8sListResources(ctx, sourceID, CertManagerIssuerCRDTypes["Issuer"], namespace); err == nil {
		result["Issuer"] = extractIssuerSummaries(issuers)
	}

	// ClusterIssuers are cluster-scoped (no namespace).
	if clusterIssuers, err := t.Client.K8sListResources(ctx, sourceID, CertManagerIssuerCRDTypes["ClusterIssuer"], ""); err == nil {
		result["ClusterIssuer"] = extractIssuerSummaries(clusterIssuers)
	}

	total := len(result["Issuer"]) + len(result["ClusterIssuer"])
	return map[string]any{
		"issuers":   result,
		"count":     total,
		"namespace": namespace,
		"source_id": sourceID,
	}, nil
}

// extractIssuerSummaries extracts concise issuer summaries from Issuer/ClusterIssuer objects.
func extractIssuerSummaries(issuers []map[string]any) []map[string]any {
	var out []map[string]any
	for _, iss := range issuers {
		summary := map[string]any{}
		if meta, ok := iss["metadata"].(map[string]any); ok {
			summary["name"] = meta["name"]
			summary["namespace"] = meta["namespace"]
		}
		if spec, ok := iss["spec"].(map[string]any); ok {
			// Detect issuer type from spec keys.
			for _, issuerType := range []string{"acme", "ca", "vault", "venafi", "selfSigned"} {
				if _, found := spec[issuerType]; found {
					summary["type"] = issuerType
					break
				}
			}
		}
		if status, ok := iss["status"].(map[string]any); ok {
			if conditions, ok := status["conditions"].([]any); ok {
				for _, cond := range conditions {
					if cm, ok := cond.(map[string]any); ok && cm["type"] == "Ready" {
						summary["ready"] = cm["status"]
						summary["reason"] = cm["reason"]
						summary["message"] = cm["message"]
						break
					}
				}
			}
		}
		out = append(out, summary)
	}
	return out
}
