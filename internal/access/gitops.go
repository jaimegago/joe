package access

import (
	"context"

	argocdadapter "github.com/jaimegago/joe/internal/adapters/gitops/argocd"
	terraformadapter "github.com/jaimegago/joe/internal/adapters/iac/terraform"
	helmadapter "github.com/jaimegago/joe/internal/adapters/packaging/helm"
	"github.com/jaimegago/joe/internal/audit"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Argo CD ---

func (a *Accessor) ArgoCDApps(ctx context.Context, principal rbac.Principal, sourceID, project string) ([]argocdadapter.App, error) {
	ad, err := guard[argocdadapter.ArgoCDAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "applications"}, "argocd")
	if err != nil {
		return nil, err
	}
	return ad.Apps(ctx, project)
}

func (a *Accessor) ArgoCDGetApp(ctx context.Context, principal rbac.Principal, sourceID, name string) (*argocdadapter.AppDetail, error) {
	ad, err := guard[argocdadapter.ArgoCDAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "applications", Name: name}, "argocd")
	if err != nil {
		return nil, err
	}
	return ad.GetApp(ctx, name)
}

func (a *Accessor) ArgoCDGetDiff(ctx context.Context, principal rbac.Principal, sourceID, name string) (*argocdadapter.Diff, error) {
	ad, err := guard[argocdadapter.ArgoCDAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "applications", Subresource: "diff", Name: name}, "argocd")
	if err != nil {
		return nil, err
	}
	return ad.GetDiff(ctx, name)
}

func (a *Accessor) ArgoCDGetHistory(ctx context.Context, principal rbac.Principal, sourceID, name string, limit int) ([]argocdadapter.SyncOperation, error) {
	ad, err := guard[argocdadapter.ArgoCDAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "applications", Subresource: "history", Name: name}, "argocd")
	if err != nil {
		return nil, err
	}
	return ad.GetHistory(ctx, name, limit)
}

// --- Terraform ---

func (a *Accessor) TerraformResources(ctx context.Context, principal rbac.Principal, sourceID, resourceType string) ([]terraformadapter.Resource, error) {
	ad, err := guard[terraformadapter.TerraformAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "resources"}, "terraform")
	if err != nil {
		return nil, err
	}
	return ad.Resources(ctx, resourceType)
}

func (a *Accessor) TerraformGetResource(ctx context.Context, principal rbac.Principal, sourceID, address string) (*terraformadapter.Resource, error) {
	ad, err := guard[terraformadapter.TerraformAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "resources", Name: address}, "terraform")
	if err != nil {
		return nil, err
	}
	return ad.GetResource(ctx, address)
}

func (a *Accessor) TerraformOutputs(ctx context.Context, principal rbac.Principal, sourceID string) (map[string]terraformadapter.Output, error) {
	ad, err := guard[terraformadapter.TerraformAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "outputs"}, "terraform")
	if err != nil {
		return nil, err
	}
	return ad.Outputs(ctx)
}

// --- Helm ---

func (a *Accessor) HelmReleases(ctx context.Context, principal rbac.Principal, sourceID, namespace string) ([]helmadapter.Release, error) {
	ad, err := guard[helmadapter.HelmAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "list", Resource: "releases", Namespace: namespace}, "helm")
	if err != nil {
		return nil, err
	}
	return ad.Releases(ctx, namespace)
}

func (a *Accessor) HelmGetRelease(ctx context.Context, principal rbac.Principal, sourceID, namespace, name string) (*helmadapter.ReleaseDetail, error) {
	ad, err := guard[helmadapter.HelmAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "releases", Name: name, Namespace: namespace}, "helm")
	if err != nil {
		return nil, err
	}
	return ad.GetRelease(ctx, namespace, name)
}

func (a *Accessor) HelmHistory(ctx context.Context, principal rbac.Principal, sourceID, namespace, name string, limit int) ([]helmadapter.RevisionEntry, error) {
	ad, err := guard[helmadapter.HelmAdapter](a, ctx, principal, sourceID, rbac.ActionRead, audit.Operation{Verb: "get", Resource: "releases", Subresource: "history", Name: name, Namespace: namespace}, "helm")
	if err != nil {
		return nil, err
	}
	return ad.History(ctx, namespace, name, limit)
}
