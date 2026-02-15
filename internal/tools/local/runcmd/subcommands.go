package runcmd

import "fmt"

// subcommandAllowlists defines the permitted subcommands for mutation-capable
// commands. These are hardcoded at compile time — the LLM cannot change them.
// Only read-only subcommands are allowed. Everything else is denied.
//
// If a command is in this map, its first argument (the subcommand) must be in
// the allowed set. If a command is NOT in this map, no subcommand filtering is
// applied (it's treated as a read-only command like ls, grep, etc.).
var subcommandAllowlists = map[string]map[string]bool{
	"kubectl": {
		"get":           true,
		"describe":      true,
		"logs":          true,
		"top":           true,
		"explain":       true,
		"api-resources": true,
		"api-versions":  true,
		"version":       true,
		"cluster-info":  true,
		"config":        true, // read-only: current-context, get-contexts, view
		"auth":          true, // read-only: can-i, whoami
	},
	"helm": {
		"list":       true,
		"status":     true,
		"get":        true,
		"history":    true,
		"show":       true,
		"search":     true,
		"version":    true,
		"env":        true,
		"repo":       true, // read-only: list
		"template":   true, // dry-run rendering, no mutations
		"dependency": true, // read-only: list
	},
	"argocd": {
		"app":     true, // filtered further below
		"cluster": true, // read-only: list, get
		"repo":    true, // read-only: list, get
		"proj":    true, // read-only: list, get
		"account": true, // read-only: get, list
		"version": true,
	},
}

// argocdAppAllowedSubcommands further restricts argocd app subcommands.
// "argocd app sync", "argocd app delete", etc. are blocked.
var argocdAppAllowedSubcommands = map[string]bool{
	"list":      true,
	"get":       true,
	"diff":      true,
	"manifests": true,
	"logs":      true,
	"history":   true,
	"resources": true,
}

// ValidateSubcommand checks whether the given arguments are allowed for a
// mutation-capable command. Returns nil if the command has no subcommand
// restrictions or if the subcommand is permitted. Returns an error if the
// subcommand is blocked or missing.
func ValidateSubcommand(command string, args []string) error {
	allowed, hasList := subcommandAllowlists[command]
	if !hasList {
		return nil // not a mutation-capable command, no filtering needed
	}

	if len(args) == 0 {
		return fmt.Errorf("command '%s' requires a subcommand (e.g., get, describe, logs)", command)
	}

	subcommand := args[0]

	// Strip leading flags before subcommand (e.g., kubectl -n foo get pods)
	idx := 0
	for idx < len(args) && len(args[idx]) > 0 && args[idx][0] == '-' {
		idx++
		// If flag takes a value (e.g., -n kube-system), skip the value too
		if idx < len(args) && idx > 0 && isFlagWithValue(args[idx-1]) {
			idx++
		}
	}
	if idx < len(args) {
		subcommand = args[idx]
	} else {
		return fmt.Errorf("command '%s' requires a subcommand after flags", command)
	}

	if !allowed[subcommand] {
		return &SubcommandDeniedError{
			Command:    command,
			Subcommand: subcommand,
			Allowed:    allowedKeys(allowed),
		}
	}

	// Additional filtering for argocd app subcommands
	if command == "argocd" && subcommand == "app" {
		return validateArgocdApp(args, idx)
	}

	return nil
}

// validateArgocdApp checks the second-level subcommand for "argocd app <action>".
func validateArgocdApp(args []string, appIdx int) error {
	// Find the subcommand after "app", skipping any flags
	nextIdx := appIdx + 1
	for nextIdx < len(args) && len(args[nextIdx]) > 0 && args[nextIdx][0] == '-' {
		nextIdx++
		if nextIdx < len(args) && nextIdx > 0 && isFlagWithValue(args[nextIdx-1]) {
			nextIdx++
		}
	}

	if nextIdx >= len(args) {
		return fmt.Errorf("'argocd app' requires a subcommand (e.g., list, get, diff)")
	}

	action := args[nextIdx]
	if !argocdAppAllowedSubcommands[action] {
		return &SubcommandDeniedError{
			Command:    "argocd app",
			Subcommand: action,
			Allowed:    allowedKeys(argocdAppAllowedSubcommands),
		}
	}

	return nil
}

// isFlagWithValue returns true for common flags that take a separate value argument.
func isFlagWithValue(flag string) bool {
	// Short flags that take values
	switch flag {
	case "-n", "-l", "-o", "-f", "-c", "-s", "--namespace", "--context",
		"--kubeconfig", "--output", "--selector", "--field-selector",
		"--server", "--grpc-web-root-path":
		return true
	}
	return false
}

// SubcommandDeniedError is returned when a subcommand is not in the allowlist.
type SubcommandDeniedError struct {
	Command    string
	Subcommand string
	Allowed    []string
}

func (e *SubcommandDeniedError) Error() string {
	return fmt.Sprintf("subcommand '%s' is not allowed for '%s'. Allowed subcommands: %v",
		e.Subcommand, e.Command, e.Allowed)
}

// allowedKeys returns the keys of a map as a sorted slice for error messages.
func allowedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// IsMutationCapable returns true if a command has subcommand restrictions,
// meaning it can potentially mutate external systems.
func IsMutationCapable(command string) bool {
	_, ok := subcommandAllowlists[command]
	return ok
}
