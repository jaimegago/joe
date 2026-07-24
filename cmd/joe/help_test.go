package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/client"
	"github.com/jaimegago/joe/internal/config"
)

// This file pins the requested-help rule: an explicit -h / --help / help is a
// REQUEST, not an invocation mistake, so it prints the usage text to STDOUT and
// exits 0. D-0136's exit-2 rule keeps covering invocation mistakes — an unknown
// flag, a surplus positional, an unknown command — which is asserted here beside
// the help cases so the two cannot drift into each other.

// noWorkDeps fails the test if a command reaches anything the CLI does once it
// has understood the invocation. Answering help (and `joe version`) must come
// out of the binary alone: no daemon boot, no config load, no client. Without
// these stubs the assertions would pass even if help booted the server first,
// which is precisely the defect this session fixed.
func noWorkDeps(t *testing.T) runDeps {
	t.Helper()
	deps := defaultRunDeps()
	deps.runServer = func(context.Context) int {
		t.Fatal("the server must not boot to answer help")
		return 0
	}
	deps.loadConfig = func(string) (*config.Config, error) {
		t.Fatal("no config may be loaded to answer help")
		return nil, nil
	}
	deps.joeDirPath = func() (string, error) {
		t.Fatal("no Joe directory may be resolved to answer help")
		return "", nil
	}
	deps.newClient = func(string, ...client.ClientOption) *client.Client {
		t.Fatal("no client may be constructed to answer help")
		return nil
	}
	return deps
}

// TestTopLevelHelp_PrintsUsageToStdoutAndExitsZero covers the three top-level
// entry points. Before this, a leading --help fell through to the daemon path,
// where the server's config flag set discards its own parse error on purpose, so
// the request was swallowed and Joe booted — printing help, then logs, then
// failing on a missing LLM provider.
func TestTopLevelHelp_PrintsUsageToStdoutAndExitsZero(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runWithDeps(context.Background(), args, &stdout, &stderr, noWorkDeps(t))
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (requested help is not a usage error)", code)
			}
			if stderr.Len() != 0 {
				t.Errorf("requested help must not write to stderr, got %q", stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, "Usage: joe [command]") {
				t.Errorf("stdout must carry the top-level usage, got %q", out)
			}
			// The list must advertise the two commands this session added, or the
			// help text describes a binary that no longer exists.
			for _, want := range []string{"version", "help", "--config"} {
				if !strings.Contains(out, want) {
					t.Errorf("usage text must mention %q, got %q", want, out)
				}
			}
		})
	}
}

// TestTopLevelHelp_AfterOtherFlagsStillRoutesToDaemon pins the deliberate
// boundary: the intercept keys on the FIRST argument only. Past it, an argument
// belongs to the server's own flag set, and re-reading it here would change what
// a leading server flag means (see TestRun_ServerFlags).
func TestTopLevelHelp_AfterOtherFlagsStillRoutesToDaemon(t *testing.T) {
	deps := defaultRunDeps()
	ran := false
	deps.runServer = func(context.Context) int {
		ran = true
		return 0
	}
	var stdout, stderr bytes.Buffer
	runWithDeps(context.Background(), []string{"--config", "/tmp/x.yaml", "--help"}, &stdout, &stderr, deps)
	if !ran {
		t.Fatal("a help token after a leading server flag must keep the daemon routing")
	}
}

// TestUnknownCommand_StaysAUsageError is the discriminating half of the help
// rule: the same text, the other stream, the other exit code.
func TestUnknownCommand_StaysAUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"bogus"}, &stdout, &stderr, noWorkDeps(t))
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (an unknown command is a usage error)", code)
	}
	if stdout.Len() != 0 {
		t.Errorf("a usage error must not write to stdout, got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "Usage: joe [command]") {
		t.Errorf("stderr must carry the usage text, got %q", stderr.String())
	}
}

// TestRequestedHelp_ExitsZeroAcrossCommands covers every command whose help can
// be answered with no work at all: the leaf flag sets, plus the four group
// dispatchers whose intercept sits ahead of their config load. The skills and
// incident SUB-subcommands are absent deliberately — their config load still
// precedes their flag set, which is the residue recorded in
// docs/backlog/cli-version-and-help.md and asserted as-is below.
func TestRequestedHelp_ExitsZeroAcrossCommands(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"version", []string{"version", "-h"}, "Usage of joe version:"},
		{"panic", []string{"panic", "-h"}, "Usage of joe panic:"},
		{"unlock", []string{"unlock", "--help"}, "Usage of joe unlock:"},
		{"mcp", []string{"mcp", "-h"}, "Usage of joe mcp:"},
		{"slack", []string{"slack", "--help"}, "Usage of joe slack:"},
		{"db group", []string{"db", "-h"}, "Usage: joe db <backup|restore>"},
		{"db backup", []string{"db", "backup", "-h"}, "Usage of joe db backup:"},
		{"db restore", []string{"db", "restore", "--help"}, "Usage of joe db restore:"},
		{"admin group", []string{"admin", "--help"}, "Usage: joe admin <bootstrap>"},
		{"admin bootstrap", []string{"admin", "bootstrap", "-h"}, "Usage of joe admin bootstrap:"},
		{"skills group", []string{"skills", "-h"}, "Usage: joe skills <install|"},
		{"incident group", []string{"incident", "help"}, "Usage: joe incident <status|"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := runWithDeps(context.Background(), tc.args, &stdout, &stderr, noWorkDeps(t))
			if code != 0 {
				t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Errorf("stdout must carry %q, got %q", tc.want, stdout.String())
			}
			if stderr.Len() != 0 {
				t.Errorf("requested help must not write to stderr, got %q", stderr.String())
			}
		})
	}
}

// TestRequestedHelp_SkillsAndIncidentLeaves records the residue honestly: these
// two families load their config before dispatching, so their leaf help exits 0
// and prints to stdout as required, but only once the config load has succeeded.
// A broken config still fails ahead of the help text; see the backlog item.
func TestRequestedHelp_SkillsAndIncidentLeaves(t *testing.T) {
	t.Run("skills install", func(t *testing.T) {
		deps := skillsDeps(t, t.TempDir(), &fakeSkillManager{})
		deps.loadConfig = func(string) (*config.Config, error) { return &config.Config{}, nil }
		var stdout, stderr bytes.Buffer
		if code := runWithDeps(context.Background(), []string{"skills", "install", "-h"}, &stdout, &stderr, deps); code != 0 {
			t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage of joe skills install:") {
			t.Errorf("stdout = %q", stdout.String())
		}
	})

	t.Run("incident status", func(t *testing.T) {
		deps := incidentRejectionDeps(t)
		var stdout, stderr bytes.Buffer
		if code := runWithDeps(context.Background(), []string{"incident", "status", "--help"}, &stdout, &stderr, deps); code != 0 {
			t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Usage of joe incident status:") {
			t.Errorf("stdout = %q", stdout.String())
		}
	})
}
