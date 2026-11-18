package access

import (
	"context"
	"errors"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/rbac"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// errK8sResourceRequired refuses a Kubernetes list or get that names no
// resource type. The adapter would refuse it too; refusing here keeps a
// caller's empty argument from reaching the permit chokepoint as an operation
// with nothing to record.
var errK8sResourceRequired = errors.New("kubernetes resource type is required")

// K8sListResources lists Kubernetes resources of the given type in a namespace.
func (a *Accessor) K8sListResources(ctx context.Context, principal rbac.Principal, sourceID, resource, namespace string) ([]unstructured.Unstructured, error) {
	if resource == "" {
		return nil, errK8sResourceRequired
	}
	ad, err := guard[k8s.KubernetesAdapter](a, ctx, principal, sourceID, rbac.ActionRead, k8sOperation("list", resource, "", "", namespace), "kubernetes")
	if err != nil {
		return nil, err
	}
	return ad.ListResources(ctx, resource, namespace)
}

// K8sGetResource fetches a single Kubernetes resource by name.
func (a *Accessor) K8sGetResource(ctx context.Context, principal rbac.Principal, sourceID, resource, namespace, name string) (*unstructured.Unstructured, error) {
	if resource == "" {
		return nil, errK8sResourceRequired
	}
	ad, err := guard[k8s.KubernetesAdapter](a, ctx, principal, sourceID, rbac.ActionRead, k8sOperation("get", resource, "", name, namespace), "kubernetes")
	if err != nil {
		return nil, err
	}
	return ad.GetResource(ctx, resource, namespace, name)
}

// K8sGetPodLogs returns logs for a pod container.
func (a *Accessor) K8sGetPodLogs(ctx context.Context, principal rbac.Principal, sourceID, namespace, pod, container string, tailLines int) (string, error) {
	ad, err := guard[k8s.KubernetesAdapter](a, ctx, principal, sourceID, rbac.ActionRead, k8sOperation("get", "pods", "log", pod, namespace), "kubernetes")
	if err != nil {
		return "", err
	}
	return ad.GetPodLogs(ctx, namespace, pod, container, tailLines)
}
