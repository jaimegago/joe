package credential

import (
	"sort"
	"testing"
)

// validEnvSegment reports whether s is a valid POSIX env var name fragment:
// non-empty, uppercase ASCII letters/digits/underscore only, and not starting
// with a digit (so JOE_<SEGMENT>_<LABEL> composes to a legal name). Hyphens are
// rejected — POSIX env var names cannot contain them, which is exactly why the
// irregular type literals (nginx-ingress, oci_registry) need an explicit segment.
func validEnvSegment(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c == '_':
		case c >= '0' && c <= '9':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// TestEnvPrefixSegments_CoverAllStaticWiredTypes is the coverage guard: every
// KindStatic wired type MUST declare a prefix segment, and the map must declare a
// segment for no NON-static-wired type (kubernetes is wired but kubeconfig-exec,
// so it must have none). If a static wired type is added to wiring.go without a
// segment here, this fails.
func TestEnvPrefixSegments_CoverAllStaticWiredTypes(t *testing.T) {
	wantStatic := map[string]bool{}
	for _, typ := range WiredTypes() {
		kind, _ := WiredProvider(typ)
		if kind == KindStatic {
			wantStatic[typ] = true
		}
	}

	// Every static wired type has a segment.
	for typ := range wantStatic {
		if _, ok := EnvPrefixSegment(typ); !ok {
			t.Errorf("static wired type %q has no env prefix segment declared", typ)
		}
	}
	// Every declared segment belongs to a static wired type (no strays, no
	// kubeconfig-exec or unwired types).
	for typ := range envPrefixSegments {
		if !wantStatic[typ] {
			t.Errorf("env prefix segment declared for %q, which is not a KindStatic wired type", typ)
		}
	}
	// Exact-count cross-check so the two sets cannot silently diverge.
	if len(envPrefixSegments) != len(wantStatic) {
		t.Fatalf("envPrefixSegments has %d entries, want %d (the static wired set)", len(envPrefixSegments), len(wantStatic))
	}
}

// TestEnvPrefixSegments_POSIXValid asserts every declared segment is a valid
// POSIX env var name fragment, so JOE_<SEGMENT>_<LABEL> always composes to a legal
// env var name (uppercase, alnum + underscore, no leading digit, no hyphen).
func TestEnvPrefixSegments_POSIXValid(t *testing.T) {
	for typ, seg := range envPrefixSegments {
		if !validEnvSegment(seg) {
			t.Errorf("segment %q for type %q is not a valid POSIX env var name fragment", seg, typ)
		}
	}
}

// TestEnvPrefixComposition pins the documented convention: EnvPrefix and
// ComposeEnvVarName produce JOE_<SEGMENT>_ and JOE_<SEGMENT>_<LABEL>.
func TestEnvPrefixComposition(t *testing.T) {
	prefix, ok := EnvPrefix("github")
	if !ok || prefix != "JOE_GITHUB_" {
		t.Fatalf("EnvPrefix(github) = %q,%v; want JOE_GITHUB_,true", prefix, ok)
	}
	name, ok := ComposeEnvVarName("github", "PROD")
	if !ok || name != "JOE_GITHUB_PROD" {
		t.Fatalf("ComposeEnvVarName(github, PROD) = %q,%v; want JOE_GITHUB_PROD,true", name, ok)
	}
	if _, ok := EnvPrefix("datadog"); ok {
		t.Errorf("EnvPrefix(datadog) returned ok=true; datadog is unwired and must have no segment")
	}
	if _, ok := EnvPrefix("kubernetes"); ok {
		t.Errorf("EnvPrefix(kubernetes) returned ok=true; kubernetes is kubeconfig-exec, not a static env type")
	}
}

// staticWithEnviron builds a StaticProvider with an injected environment, so the
// enumeration is deterministic without touching the real process env.
func staticWithEnviron(env []string) *StaticProvider {
	return &StaticProvider{environ: func() []string { return env }}
}

// TestStaticAvailableReferences_ScopedToPrefix proves the static provider
// enumerates ONLY the type's prefix, returns NAMES (label + composed name) and
// NEVER a value, and excludes other prefixes and unprefixed variables.
func TestStaticAvailableReferences_ScopedToPrefix(t *testing.T) {
	p := staticWithEnviron([]string{
		"JOE_GITHUB_PROD=ghp_secretvalue",
		"JOE_GITHUB_FOOBAR=another_secret",
		"JOE_PROMETHEUS_MAIN=different_type",
		"SECRET_DB_PASSWORD=unrelated",
		"JOE_GITHUB_=novalueforbarelabel", // bare prefix, no label -> excluded
		"PATH=/usr/bin",
	})

	refs, err := p.AvailableReferences("github")
	if err != nil {
		t.Fatalf("AvailableReferences(github): %v", err)
	}
	if !refs.Applicable {
		t.Fatalf("static references should be applicable")
	}
	if refs.Prefix != "JOE_GITHUB_" {
		t.Errorf("prefix = %q; want JOE_GITHUB_", refs.Prefix)
	}

	// Exactly {PROD, FOOBAR}, sorted by name (FOOBAR < PROD).
	gotLabels := []string{}
	for _, c := range refs.Candidates {
		gotLabels = append(gotLabels, c.Label)
		// The composed name must be JOE_GITHUB_<LABEL>.
		if want := "JOE_GITHUB_" + c.Label; c.EnvVarName != want {
			t.Errorf("candidate %+v: env_var_name = %q; want %q", c, c.EnvVarName, want)
		}
	}
	sort.Strings(gotLabels)
	if len(gotLabels) != 2 || gotLabels[0] != "FOOBAR" || gotLabels[1] != "PROD" {
		t.Fatalf("labels = %v; want exactly [FOOBAR PROD]", gotLabels)
	}

	// No value, and no foreign name, may appear anywhere in the candidate set.
	for _, c := range refs.Candidates {
		for _, banned := range []string{
			"ghp_secretvalue", "another_secret", "different_type", "unrelated",
			"JOE_PROMETHEUS_MAIN", "SECRET_DB_PASSWORD", "PATH",
		} {
			if c.Label == banned || c.EnvVarName == banned {
				t.Errorf("candidate %+v leaked a value or foreign name %q", c, banned)
			}
		}
	}
}

// TestStaticAvailableReferences_EmptyWhenNoMatches proves a non-nil empty
// candidate slice when nothing matches (serializes as [] not null).
func TestStaticAvailableReferences_EmptyWhenNoMatches(t *testing.T) {
	p := staticWithEnviron([]string{"PATH=/usr/bin", "JOE_PROMETHEUS_X=y"})
	refs, err := p.AvailableReferences("github")
	if err != nil {
		t.Fatalf("AvailableReferences(github): %v", err)
	}
	if refs.Candidates == nil {
		t.Errorf("Candidates is nil; want non-nil empty slice")
	}
	if len(refs.Candidates) != 0 {
		t.Errorf("Candidates = %+v; want empty", refs.Candidates)
	}
}

// TestStaticAvailableReferences_UndeclaredTypeErrors proves the static provider
// refuses a type with no declared segment rather than enumerating the whole
// environment (defense in depth: the endpoint only reaches here for static wired
// types, but the provider never falls back to an unscoped scan).
func TestStaticAvailableReferences_UndeclaredTypeErrors(t *testing.T) {
	p := staticWithEnviron([]string{"JOE_GITHUB_PROD=x"})
	if _, err := p.AvailableReferences("datadog"); err == nil {
		t.Fatalf("want error for a type with no declared env prefix segment")
	}
}

// TestKubeconfigExecAvailableReferences_NotApplicable proves the kubeconfig-exec
// provider answers honestly not-applicable with no candidates.
func TestKubeconfigExecAvailableReferences_NotApplicable(t *testing.T) {
	refs, err := NewKubeconfigExecProvider().AvailableReferences("kubernetes")
	if err != nil {
		t.Fatalf("AvailableReferences(kubernetes): %v", err)
	}
	if refs.Applicable {
		t.Errorf("kubeconfig-exec references should be not-applicable")
	}
	if len(refs.Candidates) != 0 {
		t.Errorf("kubeconfig-exec candidates = %+v; want empty", refs.Candidates)
	}
}
