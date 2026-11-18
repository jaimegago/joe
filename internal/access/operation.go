package access

import (
	"fmt"
	"strings"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/audit"
)

// Every dispatch method that reaches an adapter declares the operation it is
// about to perform as an audit.Operation, at the guard call, beside the action
// and the adapter method (design §2.8). That declaration is what the act's
// audit row records (D-0171). The helpers here build the ones that are not a
// plain literal.

// k8sOperation is the operation of a Kubernetes dispatch, at the resolution a
// Kubernetes audit event records it: the API verb, the resource's API group
// and plural name, the subresource, the object name and the namespace.
//
// The resource a caller names ("pods", "Deployments",
// "cert-manager.io/v1/certificates") is resolved through the adapter's own
// resolver, so the row carries the group and resource the API server will see
// rather than the caller's spelling. A resource the resolver does not know is
// recorded as the caller wrote it, lower-cased: the adapter refuses it a
// moment later for the same reason, and the row still says what was asked.
func k8sOperation(verb, resource, subresource, name, namespace string) audit.Operation {
	op := audit.Operation{
		Verb:        verb,
		Resource:    strings.ToLower(resource),
		Subresource: subresource,
		Name:        name,
		Namespace:   namespace,
	}
	if gvr, err := k8s.ResolveGVR(resource); err == nil {
		op.APIGroup = gvr.Group
		op.Resource = gvr.Resource
	}
	return op
}

// githubPRName names a pull request the way GitHub writes it: owner/repo#N.
func githubPRName(owner, repo string, number int) string {
	return fmt.Sprintf("%s/%s#%d", owner, repo, number)
}

// gitlabMRName names a merge request the way GitLab writes it: project!N.
func gitlabMRName(projectID string, iid int) string {
	return fmt.Sprintf("%s!%d", projectID, iid)
}
