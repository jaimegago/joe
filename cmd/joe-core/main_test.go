package main

import "testing"

// TestResolveConfigPath covers the precedence: --config flag > JOE_CONFIG env >
// "" (default-path sentinel).
func TestResolveConfigPath(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  string // JOE_CONFIG value; "" means unset/empty
		want string
	}{
		{name: "flag wins over env", args: []string{"--config", "/flag.yaml"}, env: "/env.yaml", want: "/flag.yaml"},
		{name: "flag equals form", args: []string{"--config=/flag.yaml"}, env: "", want: "/flag.yaml"},
		{name: "env when no flag", args: nil, env: "/env.yaml", want: "/env.yaml"},
		{name: "default sentinel when neither", args: nil, env: "", want: ""},
		{name: "unknown flag is non-fatal, env still honoured", args: []string{"--nope"}, env: "/env.yaml", want: "/env.yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("JOE_CONFIG", tc.env)
			if got := resolveConfigPath(tc.args); got != tc.want {
				t.Errorf("resolveConfigPath(%v) with JOE_CONFIG=%q = %q, want %q", tc.args, tc.env, got, tc.want)
			}
		})
	}
}
