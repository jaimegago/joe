package access

import (
	"context"

	envoyadapter "github.com/jaimegago/joe/internal/adapters/networking/envoy"
	nginxadapter "github.com/jaimegago/joe/internal/adapters/networking/nginx"
	"github.com/jaimegago/joe/internal/rbac"
)

// --- Nginx ---

func (a *Accessor) NginxListIngresses(ctx context.Context, principal rbac.Principal, sourceID, namespace string) ([]nginxadapter.Ingress, error) {
	ad, err := guard[nginxadapter.NginxAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "nginx")
	if err != nil {
		return nil, err
	}
	return ad.ListIngresses(ctx, namespace)
}

func (a *Accessor) NginxGetStatus(ctx context.Context, principal rbac.Principal, sourceID string) (*nginxadapter.NginxStatus, error) {
	ad, err := guard[nginxadapter.NginxAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "nginx")
	if err != nil {
		return nil, err
	}
	return ad.GetNginxStatus(ctx)
}

func (a *Accessor) NginxListConfigMaps(ctx context.Context, principal rbac.Principal, sourceID, namespace string) ([]nginxadapter.ConfigMapSummary, error) {
	ad, err := guard[nginxadapter.NginxAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "nginx")
	if err != nil {
		return nil, err
	}
	return ad.ListConfigMaps(ctx, namespace)
}

// --- Envoy ---

func (a *Accessor) EnvoyClusters(ctx context.Context, principal rbac.Principal, sourceID string) ([]envoyadapter.ClusterStatus, error) {
	ad, err := guard[envoyadapter.EnvoyAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "envoy")
	if err != nil {
		return nil, err
	}
	return ad.Clusters(ctx)
}

func (a *Accessor) EnvoyConfigDump(ctx context.Context, principal rbac.Principal, sourceID, section string) (map[string]any, error) {
	ad, err := guard[envoyadapter.EnvoyAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "envoy")
	if err != nil {
		return nil, err
	}
	return ad.ConfigDump(ctx, section)
}

func (a *Accessor) EnvoyStats(ctx context.Context, principal rbac.Principal, sourceID, filter string) ([]envoyadapter.Stat, error) {
	ad, err := guard[envoyadapter.EnvoyAdapter](a, ctx, principal, sourceID, rbac.ActionRead, "envoy")
	if err != nil {
		return nil, err
	}
	return ad.Stats(ctx, filter)
}
