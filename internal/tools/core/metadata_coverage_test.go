// metadata_coverage_test.go covers Name()/Description()/Parameters() methods
// that were never directly called, leaving them at 0% coverage.
// The Execute() paths are already covered by the per-tool test files.
// This file reuses mock types defined in those files (same core_test package).
package core_test

import (
	"testing"

	"github.com/jaimegago/joe/internal/tools/core"
)

// assertMeta is a small helper that verifies the basic tool metadata contract.
func assertMeta(t *testing.T, name, wantName, desc string) {
	t.Helper()
	if name != wantName {
		t.Errorf("Name() = %q, want %q", name, wantName)
	}
	if desc == "" {
		t.Error("Description() should not be empty")
	}
}

// ---- ArgoCD tools ----

func TestArgoCDTools_Metadata(t *testing.T) {
	c := &mockArgoCDClient{}

	t.Run("ArgoCDAppsTool", func(t *testing.T) {
		tool := core.NewArgoCDAppsTool(c)
		assertMeta(t, tool.Name(), "argocd_apps", tool.Description())
		tool.Parameters()
	})
	t.Run("ArgoCDGetAppTool", func(t *testing.T) {
		tool := core.NewArgoCDGetAppTool(c)
		assertMeta(t, tool.Name(), "argocd_app", tool.Description())
		tool.Parameters()
	})
	t.Run("ArgoCDDiffTool", func(t *testing.T) {
		tool := core.NewArgoCDDiffTool(c)
		assertMeta(t, tool.Name(), "argocd_diff", tool.Description())
		tool.Parameters()
	})
	t.Run("ArgoCDHistoryTool", func(t *testing.T) {
		tool := core.NewArgoCDHistoryTool(c)
		assertMeta(t, tool.Name(), "argocd_history", tool.Description())
		tool.Parameters()
	})
}

// ---- cert-manager tools ----

func TestCertManagerTools_Metadata(t *testing.T) {
	c := &mockCRDK8sClient{}

	t.Run("CertManagerCertsTool", func(t *testing.T) {
		tool := core.NewCertManagerCertsTool(c)
		assertMeta(t, tool.Name(), "certmanager_certs", tool.Description())
		tool.Parameters()
	})
	t.Run("CertManagerIssuersTool", func(t *testing.T) {
		tool := core.NewCertManagerIssuersTool(c)
		assertMeta(t, tool.Name(), "certmanager_issuers", tool.Description())
		tool.Parameters()
	})
}

// ---- Cilium tools ----

func TestCiliumTools_Metadata(t *testing.T) {
	c := &mockCRDK8sClient{}

	t.Run("CiliumPoliciesTool", func(t *testing.T) {
		tool := core.NewCiliumPoliciesTool(c)
		assertMeta(t, tool.Name(), "cilium_policies", tool.Description())
		tool.Parameters()
	})
	t.Run("CiliumEndpointsTool", func(t *testing.T) {
		tool := core.NewCiliumEndpointsTool(c)
		assertMeta(t, tool.Name(), "cilium_endpoints", tool.Description())
		tool.Parameters()
	})
}

// ---- Crossplane tools ----

func TestCrossplaneTools_Metadata(t *testing.T) {
	c := &mockCRDK8sClient{}

	t.Run("CrossplaneProvidersTool", func(t *testing.T) {
		tool := core.NewCrossplaneProvidersTool(c)
		assertMeta(t, tool.Name(), "crossplane_providers", tool.Description())
		tool.Parameters()
	})
	t.Run("CrossplaneResourcesTool", func(t *testing.T) {
		tool := core.NewCrossplaneResourcesTool(c)
		assertMeta(t, tool.Name(), "crossplane_resources", tool.Description())
		tool.Parameters()
	})
}

// ---- Flux tools ----

func TestFluxTools_Metadata(t *testing.T) {
	c := &mockFluxClient{}

	t.Run("FluxStatusTool", func(t *testing.T) {
		tool := core.NewFluxStatusTool(c)
		assertMeta(t, tool.Name(), "flux_status", tool.Description())
		tool.Parameters()
	})
	t.Run("FluxResourceTool", func(t *testing.T) {
		tool := core.NewFluxResourceTool(c)
		assertMeta(t, tool.Name(), "flux_resource", tool.Description())
		tool.Parameters()
	})
}

// ---- GitDiff tool ----

func TestGitDiffTool_Metadata(t *testing.T) {
	tool := core.NewGitDiffTool(&fakeGitClient{})
	assertMeta(t, tool.Name(), "git_diff", tool.Description())
	tool.Parameters()
}

// ---- GitLog / GitRead tool parameters ----

func TestGitLogTool_Parameters(t *testing.T) {
	p := core.NewGitLogTool(&fakeGitLogClient{}).Parameters()
	if p.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", p.Type)
	}
}

func TestGitReadTool_Parameters(t *testing.T) {
	p := core.NewGitReadTool(&fakeGitReadClient{}).Parameters()
	if p.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", p.Type)
	}
}

// ---- Helm tools ----

func TestHelmTools_Metadata(t *testing.T) {
	c := &mockHelmClient{}

	t.Run("HelmReleasesTool", func(t *testing.T) {
		tool := core.NewHelmReleasesTool(c)
		assertMeta(t, tool.Name(), "helm_releases", tool.Description())
		tool.Parameters()
	})
	t.Run("HelmGetReleaseTool", func(t *testing.T) {
		tool := core.NewHelmGetReleaseTool(c)
		assertMeta(t, tool.Name(), "helm_release", tool.Description())
		tool.Parameters()
	})
	t.Run("HelmHistoryTool", func(t *testing.T) {
		tool := core.NewHelmHistoryTool(c)
		assertMeta(t, tool.Name(), "helm_history", tool.Description())
		tool.Parameters()
	})
}

// ---- Istio tools ----

func TestIstioTools_Metadata(t *testing.T) {
	c := &mockCRDK8sClient{}

	t.Run("IstioConfigTool", func(t *testing.T) {
		tool := core.NewIstioConfigTool(c)
		assertMeta(t, tool.Name(), "istio_config", tool.Description())
		tool.Parameters()
	})
	t.Run("IstioResourceTool", func(t *testing.T) {
		tool := core.NewIstioResourceTool(c)
		assertMeta(t, tool.Name(), "istio_resource", tool.Description())
		tool.Parameters()
	})
}

// ---- Kafka tools ----

func TestKafkaTools_Metadata(t *testing.T) {
	c := &mockKafkaClient{}

	t.Run("KafkaBrokersTool", func(t *testing.T) {
		tool := core.NewKafkaBrokersTool(c)
		assertMeta(t, tool.Name(), "kafka_brokers", tool.Description())
		tool.Parameters()
	})
	t.Run("KafkaConsumerGroupsTool", func(t *testing.T) {
		tool := core.NewKafkaConsumerGroupsTool(c)
		assertMeta(t, tool.Name(), "kafka_consumers", tool.Description())
		tool.Parameters()
	})
}

// ---- KEDA tools ----

func TestKEDATools_Metadata(t *testing.T) {
	tool := core.NewKEDAScaledObjectsTool(&mockCRDK8sClient{})
	assertMeta(t, tool.Name(), "keda_scaledobjects", tool.Description())
	tool.Parameters()
}

// ---- ListComponents tool ----

func TestListComponentsTool_Parameters(t *testing.T) {
	p := core.NewListComponentsTool(&fakeListComponentsClient{}).Parameters()
	if p.Type != "object" {
		t.Errorf("Parameters().Type = %q, want object", p.Type)
	}
}

// ---- MySQL tools ----

func TestMySQLQueryTool_Metadata(t *testing.T) {
	tool := core.NewMySQLQueryTool(&mockMySQLClient{})
	assertMeta(t, tool.Name(), "mysql_query", tool.Description())
	tool.Parameters()
}

// ---- OPA tools ----

func TestOPATools_Metadata(t *testing.T) {
	c := &mockCRDK8sClient{}

	t.Run("OPAConstraintsTool", func(t *testing.T) {
		tool := core.NewOPAConstraintsTool(c)
		assertMeta(t, tool.Name(), "opa_constraints", tool.Description())
		tool.Parameters()
	})
	t.Run("OPAViolationsTool", func(t *testing.T) {
		tool := core.NewOPAViolationsTool(c)
		assertMeta(t, tool.Name(), "opa_violations", tool.Description())
		tool.Parameters()
	})
}

// ---- Postgres tools ----

func TestPostgresQueryTool_Metadata(t *testing.T) {
	tool := core.NewPostgresQueryTool(&mockPostgresClient{})
	assertMeta(t, tool.Name(), "postgres_query", tool.Description())
	tool.Parameters()
}

// ---- Redis tools ----

func TestRedisSlowLogTool_Metadata(t *testing.T) {
	tool := core.NewRedisSlowLogTool(&mockRedisClient{})
	assertMeta(t, tool.Name(), "redis_slowlog", tool.Description())
	tool.Parameters()
}

// ---- Registry tools ----

func TestRegistryTools_Metadata(t *testing.T) {
	t.Run("RegistryQueryTool", func(t *testing.T) {
		tool := core.NewRegistryQueryTool(&mockOCIClient{})
		assertMeta(t, tool.Name(), "registry_query", tool.Description())
		tool.Parameters()
	})
	t.Run("ArtifactoryQueryTool", func(t *testing.T) {
		tool := core.NewArtifactoryQueryTool(&mockArtifactoryClient{})
		assertMeta(t, tool.Name(), "artifactory_query", tool.Description())
		tool.Parameters()
	})
	t.Run("ECRQueryTool", func(t *testing.T) {
		tool := core.NewECRQueryTool(&mockECRQueryClient{})
		assertMeta(t, tool.Name(), "ecr_query", tool.Description())
		tool.Parameters()
	})
}

// ---- Terraform tools ----

func TestTerraformTools_Metadata(t *testing.T) {
	c := &mockTerraformClient{}

	t.Run("TerraformStateTool", func(t *testing.T) {
		tool := core.NewTerraformStateTool(c)
		assertMeta(t, tool.Name(), "terraform_state", tool.Description())
		tool.Parameters()
	})
	t.Run("TerraformResourceTool", func(t *testing.T) {
		tool := core.NewTerraformResourceTool(c)
		assertMeta(t, tool.Name(), "terraform_resource", tool.Description())
		tool.Parameters()
	})
	t.Run("TerraformOutputsTool", func(t *testing.T) {
		tool := core.NewTerraformOutputsTool(c)
		assertMeta(t, tool.Name(), "terraform_outputs", tool.Description())
		tool.Parameters()
	})
}
