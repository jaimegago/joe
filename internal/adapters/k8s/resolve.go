package k8s

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// knownResources maps common short names to their GroupVersionResource.
var knownResources = map[string]schema.GroupVersionResource{
	"pods":            {Group: "", Version: "v1", Resource: "pods"},
	"services":        {Group: "", Version: "v1", Resource: "services"},
	"configmaps":      {Group: "", Version: "v1", Resource: "configmaps"},
	"secrets":         {Group: "", Version: "v1", Resource: "secrets"},
	"namespaces":      {Group: "", Version: "v1", Resource: "namespaces"},
	"nodes":           {Group: "", Version: "v1", Resource: "nodes"},
	"events":          {Group: "", Version: "v1", Resource: "events"},
	"deployments":     {Group: "apps", Version: "v1", Resource: "deployments"},
	"statefulsets":    {Group: "apps", Version: "v1", Resource: "statefulsets"},
	"daemonsets":      {Group: "apps", Version: "v1", Resource: "daemonsets"},
	"replicasets":     {Group: "apps", Version: "v1", Resource: "replicasets"},
	"ingresses":       {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
	"jobs":            {Group: "batch", Version: "v1", Resource: "jobs"},
	"cronjobs":        {Group: "batch", Version: "v1", Resource: "cronjobs"},
	"serviceaccounts": {Group: "", Version: "v1", Resource: "serviceaccounts"},
}

// ResolveGVR maps a resource name to a GroupVersionResource.
// Accepts common short names (e.g. "pods", "deployments") or
// the explicit "group/version/resource" format for CRDs.
func ResolveGVR(resource string) (schema.GroupVersionResource, error) {
	// Check known resources first (case-insensitive)
	lower := strings.ToLower(resource)
	if gvr, ok := knownResources[lower]; ok {
		return gvr, nil
	}

	// Try explicit "group/version/resource" format
	parts := strings.Split(resource, "/")
	if len(parts) == 3 {
		return schema.GroupVersionResource{
			Group:    parts[0],
			Version:  parts[1],
			Resource: parts[2],
		}, nil
	}

	return schema.GroupVersionResource{}, fmt.Errorf("unknown resource type %q; use a known name (pods, deployments, ...) or group/version/resource format", resource)
}
