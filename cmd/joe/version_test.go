package main

import (
	"bytes"
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/buildinfo"
	"github.com/jaimegago/joe/internal/webui"
)

// TestRunVersionCommand_PrintsFourFields pins the output shape: exactly four
// lines, `key: value`, in the order version/commit/build_time/ui_digest — the
// same four fields GET /api/v1/version serializes, which is the point of the
// command (that endpoint needs a booted, authenticated server; an operator
// holding only a downloaded binary has neither).
func TestRunVersionCommand_PrintsFourFields(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithDeps(context.Background(), []string{"version"}, &stdout, &stderr, noWorkDeps(t))
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("nothing may go to stderr on success, got %q", stderr.String())
	}

	lines := strings.Split(strings.TrimRight(stdout.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("want exactly 4 lines, got %d: %q", len(lines), stdout.String())
	}
	wantKeys := []string{"version", "commit", "build_time", "ui_digest"}
	values := map[string]string{}
	for i, line := range lines {
		prefix := wantKeys[i] + ": "
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("line %d = %q, want prefix %q", i, line, prefix)
		}
		values[wantKeys[i]] = strings.TrimPrefix(line, prefix)
	}
	for _, k := range wantKeys {
		if values[k] == "" {
			t.Errorf("%s has an empty value; the command must never print a field it could not resolve", k)
		}
	}
}

// TestRunVersionCommand_UIDigestMatchesEmbeddedUI pins the offline digest to the
// canonical computation over the bytes this binary serves, rather than to a
// literal: the value legitimately differs between a placeholder-only build and
// one carrying a staged UI, and a literal would encode whichever tree the test
// last ran in.
func TestRunVersionCommand_UIDigestMatchesEmbeddedUI(t *testing.T) {
	uiFS, err := webui.DistFS()
	if err != nil {
		t.Fatalf("open embedded UI: %v", err)
	}
	want, err := buildinfo.Compute(uiFS)
	if err != nil {
		t.Fatalf("compute digest: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := runWithDeps(context.Background(), []string{"version"}, &stdout, &stderr, noWorkDeps(t)); code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}

	got := ""
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.HasPrefix(line, "ui_digest: ") {
			got = strings.TrimPrefix(line, "ui_digest: ")
		}
	}
	if got != want {
		t.Errorf("ui_digest = %q, want %q (the canonical Compute over webui.DistFS)", got, want)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(got) {
		t.Errorf("ui_digest = %q, want 64 lowercase hex characters", got)
	}
}
