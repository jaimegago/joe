package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
	"github.com/jaimegago/joe/internal/skills"
)

// This file pins the tree-wide CLI argument-rejection convention: every
// subcommand rejects an unknown flag and a surplus positional as a USAGE error
// (exit 2), distinct from an operational failure (exit 1) that happened after
// the invocation was understood. The two checks are asserted separately per
// command, because they are separate checks: a flag-parse failure and an arity
// failure fire on different inputs and one passing does not imply the other.

// rejectionDeps wires panic/unlock so that any work past the argument check is
// observable. A usage error must be refused before a config is loaded, a client
// is constructed, or the panic row is opened, so all three stubs fail the test
// if reached.
func rejectionDeps(t *testing.T) runDeps {
	t.Helper()
	deps := testDeps(t.TempDir())
	deps.loadConfig = func(string) (*config.Config, error) {
		t.Fatal("config must not be loaded on a usage error")
		return nil, nil
	}
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		t.Fatal("client must not be constructed on a usage error")
		return nil
	}
	deps.openPanicStore = func(*config.Config) (panicRowStore, func() error, error) {
		t.Fatal("panic store must not be opened on a usage error")
		return nil, nil, nil
	}
	return deps
}

// `joe version` takes neither flags nor positionals, so both checks below fire
// on the whole of what it can be given wrong. It is in this file rather than
// beside its output test because the convention it follows is this file's
// subject: an explicit -h is a request answered with 0 (help_test.go), anything
// else on the invocation is a usage error answered with 2.
func TestRunVersionCommand_UnknownFlagIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"version", "--nope"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("no build identity may be printed on a usage error, got %q", stdout.String())
	}
}

func TestRunVersionCommand_SurplusPositionalIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"version", "extra"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "version takes no positional arguments") {
		t.Errorf("stderr must explain the refusal, got %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("no build identity may be printed on a usage error, got %q", stdout.String())
	}
}

func TestRunPanicCommand_UnknownFlagIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"panic", "--nope"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
}

func TestRunPanicCommand_SurplusPositionalIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"panic", "extra"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "panic takes no positional arguments") {
		t.Errorf("stderr must explain the refusal, got %q", stderr.String())
	}
}

func TestRunUnlockCommand_UnknownFlagIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "--nope"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
}

func TestRunUnlockCommand_SurplusPositionalIsUsageError(t *testing.T) {
	deps := rejectionDeps(t)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"unlock", "extra"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "unlock takes no positional arguments") {
		t.Errorf("stderr must explain the refusal, got %q", stderr.String())
	}
}

// incidentRejectionDeps points the incident command at a server that fails the
// test if contacted: a usage error must be refused before the HTTP call, which
// is what makes the rejection meaningful rather than cosmetic.
func incidentRejectionDeps(t *testing.T) runDeps {
	t.Helper()
	server := regimeServer(t, false)
	deps := incidentDeps(t, server)
	deps.newClient = func(baseURL string, opts ...client.ClientOption) *client.Client {
		return client.New(baseURL, opts...)
	}
	return deps
}

func TestRunIncidentLeafCommands_RejectUnknownFlagAndSurplusPositional(t *testing.T) {
	// declare is invoked with its required --session so the arity case fails on
	// the positional rather than on the missing flag.
	cases := []struct {
		name       string
		unknown    []string
		surplus    []string
		wantSubstr string
	}{
		{
			name:       "status",
			unknown:    []string{"incident", "status", "--nope"},
			surplus:    []string{"incident", "status", "extra"},
			wantSubstr: "status takes no positional arguments",
		},
		{
			name:       "declare",
			unknown:    []string{"incident", "declare", "--session", "sess-1", "--nope"},
			surplus:    []string{"incident", "declare", "--session", "sess-1", "extra"},
			wantSubstr: "declare takes no positional arguments",
		},
		{
			name:       "resolve",
			unknown:    []string{"incident", "resolve", "--nope"},
			surplus:    []string{"incident", "resolve", "extra"},
			wantSubstr: "resolve takes no positional arguments",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := incidentRejectionDeps(t)

			var stdout, stderr bytes.Buffer
			if code := runWithDeps(context.Background(), tc.unknown, &stdout, &stderr, deps); code != 2 {
				t.Fatalf("unknown flag: exit = %d, want 2 (usage error)", code)
			}

			stdout.Reset()
			stderr.Reset()
			if code := runWithDeps(context.Background(), tc.surplus, &stdout, &stderr, deps); code != 2 {
				t.Fatalf("surplus positional: exit = %d, want 2 (usage error)", code)
			}
			if !strings.Contains(stderr.String(), tc.wantSubstr) {
				t.Errorf("stderr must explain the refusal, got %q", stderr.String())
			}
		})
	}
}

// TestRunIncidentStatus_CorrectInvocationUnchanged is the other half of the
// pair: adding the arity check must not disturb a correct invocation.
func TestRunIncidentStatus_CorrectInvocationUnchanged(t *testing.T) {
	deps := incidentRejectionDeps(t)
	var stdout, stderr bytes.Buffer
	if code := runWithDeps(context.Background(), []string{"incident", "status"}, &stdout, &stderr, deps); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "operating normally") {
		t.Errorf("stdout = %q", stdout.String())
	}
}

// TestRunSkillsCommand_List_RejectsUnknownFlag pins the primary gap this
// session closed: `joe skills list` parsed nothing at all, so a flag it does
// not implement was accepted and ignored, and the unfiltered listing was
// reported as success.
func TestRunSkillsCommand_List_RejectsUnknownFlag(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list", "--quarantined"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("no listing may be printed on a usage error, got %q", stdout.String())
	}
}

func TestRunSkillsCommand_List_RejectsSurplusPositional(t *testing.T) {
	mgr := &fakeSkillManager{}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list", "extra"}, &stdout, &stderr, deps)
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "list takes no positional arguments") {
		t.Errorf("stderr must explain the refusal, got %q", stderr.String())
	}
}

// TestSkillsFamilyArityExitCodeIsTwo pins the deliberate behavior change: the
// skills sub-subcommands returned 1 for a bad positional count while every
// other command returned 2. That split was drift, not a design — collapsing it
// toward 1 would have erased the distinction between "you invoked this wrong"
// and "it ran and failed". Each case below is an ARITY failure, not a
// flag-parse failure.
func TestSkillsFamilyArityExitCodeIsTwo(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"install missing repo-url", []string{"skills", "install"}},
		{"install surplus positional", []string{"skills", "install", "a", "b"}},
		{"remove missing name", []string{"skills", "remove"}},
		{"update surplus positional", []string{"skills", "update", "a", "b"}},
		{"approve missing name", []string{"skills", "approve"}},
		{"reject missing name", []string{"skills", "reject"}},
		{"reload surplus positional", []string{"skills", "reload", "extra"}},
		{"list surplus positional", []string{"skills", "list", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeSkillManager{}
			deps := skillsDeps(t, t.TempDir(), mgr)
			deps.loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
			var stdout, stderr bytes.Buffer
			code := runWithDeps(context.Background(), tc.args, &stdout, &stderr, deps)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error); stderr=%q", code, stderr.String())
			}
		})
	}
}

// TestSkillsFamilyUnknownFlagIsUsageError is the discriminating half: these
// invocations carry a CORRECT positional count and fail only on the unknown
// flag, so they exercise the flag-parse check with the arity check satisfied —
// the mirror of TestSkillsFamilyArityExitCodeIsTwo, whose cases all parse
// cleanly and fail only on arity.
func TestSkillsFamilyUnknownFlagIsUsageError(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"install", []string{"skills", "install", "https://example.com/foo.git", "--nope"}},
		{"remove", []string{"skills", "remove", "alpha", "--nope"}},
		{"update", []string{"skills", "update", "--nope"}},
		{"approve", []string{"skills", "approve", "--nope", "alpha"}},
		{"reject", []string{"skills", "reject", "--nope", "alpha"}},
		{"reload", []string{"skills", "reload", "--nope"}},
		{"list", []string{"skills", "list", "--nope"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := &fakeSkillManager{}
			deps := skillsDeps(t, t.TempDir(), mgr)
			deps.loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
			deps.newClient = func(string, ...client.ClientOption) *client.Client {
				t.Fatal("client must not be constructed on a usage error")
				return nil
			}
			var stdout, stderr bytes.Buffer
			code := runWithDeps(context.Background(), tc.args, &stdout, &stderr, deps)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (usage error); stderr=%q", code, stderr.String())
			}
		})
	}
}

// TestRunSkillsCommand_List_CorrectInvocationUnchanged confirms the new flag
// set did not disturb the working path: `joe skills list` with no arguments
// still lists and exits 0.
func TestRunSkillsCommand_List_CorrectInvocationUnchanged(t *testing.T) {
	mgr := &fakeSkillManager{listResp: []skills.Install{{
		Repo:   "https://example.com/foo.git",
		Ref:    "main",
		Skills: []skills.SkillRecord{{Name: "alpha"}},
	}}}
	deps := skillsDeps(t, t.TempDir(), mgr)
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"skills", "list"}, &stdout, &stderr, deps)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "alpha") {
		t.Errorf("stdout must still carry the listing, got %q", stdout.String())
	}
}
