// Package tildeguard holds no production code. It is a neutral test-only
// location that imports both internal/adapters/k8s and internal/credential so a
// single test can assert their two hand-copied tilde-expansion helpers never
// diverge. See guard_test.go and D-0026.
package tildeguard
