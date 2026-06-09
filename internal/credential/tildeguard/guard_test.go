package tildeguard

import (
	"testing"

	"github.com/jaimegago/joe/internal/adapters/k8s"
	"github.com/jaimegago/joe/internal/credential"
)

// TestTildeHelpersDoNotDiverge guards the invariant that k8s.expandPath (the
// canonical adapter helper) and credential.expandKubeconfigPath (a deliberate
// hand-copy) produce IDENTICAL output for every input.
//
// The duplication exists because internal/adapters/k8s imports
// internal/credential (D-0026 unit 2), so credential cannot import k8s back to
// share the helper without an import cycle. Nothing else makes the two stay in
// sync — a future edit to either could silently diverge them. This test bites
// the moment that happens.
//
// It lives in this neutral, purpose-built package (not the k8s or credential
// suite) because it is the only place that can import both. It reaches the
// otherwise-unexported helpers through the minimal ExpandPathForTest /
// ExpandKubeconfigPathForTest wrappers, which exist solely for this guard.
//
// NOTE: this is intentionally NOT a unification of the two helpers onto a shared
// implementation — that reconciliation (and folding in paths.ExpandPath) is a
// deferred backlog target, see docs/backlog. This guard is what makes deferring
// it safe.
func TestTildeHelpersDoNotDiverge(t *testing.T) {
	cases := []string{
		"~/foo/bar",   // tilde-prefixed: expands to home/foo/bar
		"~",           // bare tilde: expands to home
		"/etc/kube/c", // absolute: unchanged
		"relative/c",  // relative: unchanged (no Abs in either helper)
		"~/.kube/config",
		"plain",
	}

	for _, in := range cases {
		gotK8s, errK8s := k8s.ExpandPathForTest(in)
		gotCred, errCred := credential.ExpandKubeconfigPathForTest(in)

		if (errK8s == nil) != (errCred == nil) {
			t.Errorf("input %q: error mismatch: k8s err=%v, credential err=%v", in, errK8s, errCred)
			continue
		}
		if gotK8s != gotCred {
			t.Errorf("input %q: helpers diverged: k8s=%q, credential=%q", in, gotK8s, gotCred)
		}
	}
}
