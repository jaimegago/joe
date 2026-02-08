package k8s

import (
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ListResources lists resources of the given type in a namespace.
// If namespace is empty, lists across all namespaces.
func (a *Adapter) ListResources(ctx context.Context, resource, namespace string) ([]unstructured.Unstructured, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	gvr, err := ResolveGVR(resource)
	if err != nil {
		return nil, err
	}

	list, err := a.dynClient.Resource(gvr).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", resource, err)
	}

	return list.Items, nil
}

// GetResource gets a single resource by name.
func (a *Adapter) GetResource(ctx context.Context, resource, namespace, name string) (*unstructured.Unstructured, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if !a.connected {
		return nil, fmt.Errorf("adapter not connected")
	}

	gvr, err := ResolveGVR(resource)
	if err != nil {
		return nil, err
	}

	obj, err := a.dynClient.Resource(gvr).Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s/%s: %w", resource, name, err)
	}

	return obj, nil
}
