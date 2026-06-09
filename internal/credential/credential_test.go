package credential

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

// ----- static provider -----

func TestStaticProvider_ResolveInlineValue(t *testing.T) {
	p := NewStaticProvider()
	res, err := p.Resolve(context.Background(), "comp-1", json.RawMessage(`{"value":"tok-abc","audience":"github"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.Stage != StageMintSucceeded || !res.Diagnostic.OK {
		t.Fatalf("want mint-succeeded/ok, got %s ok=%t", res.Diagnostic.Stage, res.Diagnostic.OK)
	}
	if res.Diagnostic.Provider != KindStatic {
		t.Fatalf("provider = %s", res.Diagnostic.Provider)
	}
	v, ok := res.StaticValue()
	if !ok || v != "tok-abc" {
		t.Fatalf("StaticValue = %q,%t", v, ok)
	}
	if _, ok := res.KubeSelection(); ok {
		t.Fatalf("static resolution should not yield a kube selection")
	}
}

func TestStaticProvider_ResolveFromEnvVar(t *testing.T) {
	p := &StaticProvider{lookupEnv: func(k string) (string, bool) {
		if k == "JOE_TEST_TOKEN" {
			return "from-env", true
		}
		return "", false
	}}
	res, err := p.Resolve(context.Background(), "comp-2", json.RawMessage(`{"env_var":"JOE_TEST_TOKEN"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if res.Diagnostic.Stage != StageMintSucceeded || !res.Diagnostic.OK {
		t.Fatalf("want mint-succeeded/ok, got %s ok=%t", res.Diagnostic.Stage, res.Diagnostic.OK)
	}
	if v, _ := res.StaticValue(); v != "from-env" {
		t.Fatalf("StaticValue = %q", v)
	}
}

func TestStaticProvider_ResolveMissingEnvVarFails(t *testing.T) {
	p := &StaticProvider{lookupEnv: func(string) (string, bool) { return "", false }}
	res, err := p.Resolve(context.Background(), "comp-3", json.RawMessage(`{"env_var":"NOPE"}`))
	if err != nil {
		t.Fatalf("Resolve returned error, want staged failure: %v", err)
	}
	if res.Diagnostic.OK {
		t.Fatalf("want OK=false")
	}
	if res.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("want stop at mint-attempted, got %s", res.Diagnostic.Stage)
	}
	if res.Diagnostic.Reason == "" {
		t.Fatalf("want a non-sensitive reason")
	}
	if v, ok := res.StaticValue(); ok && v != "" {
		t.Fatalf("failed resolution should carry no value, got %q", v)
	}
}

func TestStaticProvider_ProbeAdvancesToConnectivityProbed(t *testing.T) {
	p := NewStaticProvider()
	res, _ := p.Resolve(context.Background(), "comp-4", json.RawMessage(`{"value":"tok"}`))
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Diagnostic.Stage != StageConnectivityProbed || !probed.Diagnostic.OK {
		t.Fatalf("want connectivity-probed/ok, got %s ok=%t", probed.Diagnostic.Stage, probed.Diagnostic.OK)
	}
	if v, _ := probed.StaticValue(); v != "tok" {
		t.Fatalf("probe lost the value: %q", v)
	}
}

// TestMintSucceededWithoutProbeIsLegalTerminalSuccess asserts the
// lazy-connectivity posture: a resolution may terminate at mint-succeeded
// without ever being probed.
func TestMintSucceededWithoutProbeIsLegalTerminalSuccess(t *testing.T) {
	p := NewStaticProvider()
	res, err := p.Resolve(context.Background(), "comp-5", json.RawMessage(`{"value":"tok"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// No Probe call at all — this must be a legal, usable terminal success.
	if res.Diagnostic.Stage != StageMintSucceeded {
		t.Fatalf("want mint-succeeded, got %s", res.Diagnostic.Stage)
	}
	if !res.Diagnostic.OK {
		t.Fatalf("mint-succeeded without probe must be OK")
	}
	if _, ok := res.StaticValue(); !ok {
		t.Fatalf("source must be usable without probing")
	}
}

func TestStaticProvider_Describe(t *testing.T) {
	d, err := NewStaticProvider().Describe(json.RawMessage(`{"value":"x","audience":"github"}`))
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if d.Provider != KindStatic || d.Audience != "github" {
		t.Fatalf("descriptor = %+v", d)
	}
}

// ----- kubeconfig-exec provider -----

func TestKubeconfigExec_ResolveSelectsSource(t *testing.T) {
	p := NewKubeconfigExecProvider()
	cfg := json.RawMessage(`{"credential_provider":"kubeconfig-exec","kubeconfig":"/home/sre/.kube/config","context":"prod-eks","audience":"k8s"}`)
	res, err := p.Resolve(context.Background(), "comp-k", cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Resolve never contacts the backend: at most mint-succeeded.
	if res.Diagnostic.Stage != StageMintSucceeded || !res.Diagnostic.OK {
		t.Fatalf("want mint-succeeded/ok, got %s ok=%t", res.Diagnostic.Stage, res.Diagnostic.OK)
	}
	sel, ok := res.KubeSelection()
	if !ok {
		t.Fatalf("want a kube selection")
	}
	if sel.Kubeconfig != "/home/sre/.kube/config" || sel.Context != "prod-eks" {
		t.Fatalf("selection = %+v", sel)
	}
	if _, ok := res.StaticValue(); ok {
		t.Fatalf("kubeconfig-exec resolution must not yield a static value")
	}
}

func TestKubeconfigExec_ProbeSuccess(t *testing.T) {
	p := &KubeconfigExecProvider{prober: func(context.Context, KubeSelection) kubeProbeResult {
		return kubeProbeResult{}
	}}
	res, _ := p.Resolve(context.Background(), "comp-k", json.RawMessage(`{"context":"prod"}`))
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probed.Diagnostic.Stage != StageConnectivityProbed || !probed.Diagnostic.OK {
		t.Fatalf("want connectivity-probed/ok, got %s ok=%t", probed.Diagnostic.Stage, probed.Diagnostic.OK)
	}
}

func TestKubeconfigExec_ProbeMintFailureCapturesStderr(t *testing.T) {
	const stderr = "exec: aws-iam-authenticator: could not reach IdP at 10.0.0.1"
	p := &KubeconfigExecProvider{prober: func(context.Context, KubeSelection) kubeProbeResult {
		return kubeProbeResult{stderr: stderr, err: errors.New("exec plugin failed")}
	}}
	res, _ := p.Resolve(context.Background(), "comp-k", json.RawMessage(`{"context":"prod"}`))
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe returned error, want staged failure: %v", err)
	}
	if probed.Diagnostic.OK {
		t.Fatalf("want OK=false on exec failure")
	}
	if probed.Diagnostic.Stage != StageMintAttempted {
		t.Fatalf("want generic mint-failed (stop at mint-attempted), got %s", probed.Diagnostic.Stage)
	}
	// The diagnostic reason is generic — never the raw stderr.
	if strings.Contains(probed.Diagnostic.Reason, "aws-iam-authenticator") || probed.Diagnostic.Reason == "" {
		t.Fatalf("reason must be generic and non-sensitive, got %q", probed.Diagnostic.Reason)
	}
	// The raw stderr is reachable ONLY through the deliberate accessor.
	if got := probed.CapturedStderr(); got != stderr {
		t.Fatalf("CapturedStderr = %q, want verbatim stderr", got)
	}
}

// ----- selection / discriminator -----

func TestSelect_DefaultsToStaticWhenDiscriminatorAbsent(t *testing.T) {
	for _, cfg := range []json.RawMessage{nil, json.RawMessage(`{}`), json.RawMessage(`{"value":"x"}`)} {
		prov, err := Select(cfg)
		if err != nil {
			t.Fatalf("Select(%s): %v", cfg, err)
		}
		if _, ok := prov.(*StaticProvider); !ok {
			t.Fatalf("Select(%s) = %T, want *StaticProvider", cfg, prov)
		}
	}
}

func TestSelect_PicksKubeconfigExec(t *testing.T) {
	prov, err := Select(json.RawMessage(`{"credential_provider":"kubeconfig-exec"}`))
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if _, ok := prov.(*KubeconfigExecProvider); !ok {
		t.Fatalf("Select = %T, want *KubeconfigExecProvider", prov)
	}
}

func TestSelect_UnknownKindErrors(t *testing.T) {
	if _, err := Select(json.RawMessage(`{"credential_provider":"vault-magic"}`)); err == nil {
		t.Fatalf("want error for unknown provider kind")
	}
}

func TestKindFromConfig_Default(t *testing.T) {
	k, err := KindFromConfig(nil)
	if err != nil || k != KindStatic {
		t.Fatalf("KindFromConfig(nil) = %q,%v", k, err)
	}
}

// ----- break-test 1: the credential half can never leak -----

// TestBreakCredentialHalfNeverLeaks places a sentinel secret in the credential
// half, then asserts that JSON-marshaling the full result and rendering it
// through every fmt and structured-log path produces the diagnostic fields but
// NEVER the sentinel — while the explicit typed accessor still returns it. This
// MUST fail if a future change makes the credential leakable.
func TestBreakCredentialHalfNeverLeaks(t *testing.T) {
	const sentinel = "S3NTINEL-static-secret-zzz"
	p := NewStaticProvider()
	res, err := p.Resolve(context.Background(), "comp-leak", json.RawMessage(`{"value":"`+sentinel+`","audience":"github"}`))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// The accessor — the ONLY legitimate path — must return the secret.
	if v, ok := res.StaticValue(); !ok || v != sentinel {
		t.Fatalf("accessor must return the secret, got %q,%t", v, ok)
	}

	renderings := allRenderings(t, res)
	for name, out := range renderings {
		if strings.Contains(out, sentinel) {
			t.Fatalf("LEAK via %s: %q", name, out)
		}
	}
	// Sanity: the diagnostic identity DID make it through serialization, so the
	// test is actually exercising real output (not empty strings).
	if !strings.Contains(renderings["json(Resolution)"], "comp-leak") {
		t.Fatalf("diagnostic component id missing from JSON: %q", renderings["json(Resolution)"])
	}
}

// ----- break-test 2: captured stderr can never leak except via its accessor ---

// TestBreakCapturedStderrNeverLeaks places a sentinel in the captured-stderr
// field and asserts the diagnostic half and structured-log rendering never
// contain it, while the explicit human-facing accessor does. This MUST fail if a
// future change sweeps stderr into the diagnostic or any log path.
func TestBreakCapturedStderrNeverLeaks(t *testing.T) {
	const sentinel = "S3NTINEL-stderr-Bearer-eyJhbGci-zzz"
	p := &KubeconfigExecProvider{prober: func(context.Context, KubeSelection) kubeProbeResult {
		return kubeProbeResult{stderr: sentinel, err: errors.New("exec plugin failed")}
	}}
	res, _ := p.Resolve(context.Background(), "comp-leak2", json.RawMessage(`{"context":"prod"}`))
	probed, err := p.Probe(context.Background(), res)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	// The human-facing accessor — the ONLY legitimate path — must return it.
	if probed.CapturedStderr() != sentinel {
		t.Fatalf("CapturedStderr must return the captured text")
	}

	renderings := allRenderings(t, probed)
	// Also exercise marshaling the diagnostic half directly.
	diagJSON, err := json.Marshal(probed.Diagnostic)
	if err != nil {
		t.Fatalf("marshal diagnostic: %v", err)
	}
	renderings["json(Diagnostic)"] = string(diagJSON)

	for name, out := range renderings {
		if strings.Contains(out, sentinel) {
			t.Fatalf("LEAK via %s: %q", name, out)
		}
	}
	if !strings.Contains(renderings["json(Resolution)"], "comp-leak2") {
		t.Fatalf("diagnostic component id missing from JSON: %q", renderings["json(Resolution)"])
	}
}

// allRenderings collects every serialization/format/log path through which a
// Resolution (and its credential half) could conceivably escape.
func allRenderings(t *testing.T, res *Resolution) map[string]string {
	t.Helper()
	out := map[string]string{}

	jsonRes, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("json(Resolution): %v", err)
	}
	out["json(Resolution)"] = string(jsonRes)

	jsonVal, err := json.Marshal(*res)
	if err != nil {
		t.Fatalf("json(Resolution value): %v", err)
	}
	out["json(Resolution value)"] = string(jsonVal)

	// The credential half on its own.
	jsonCred, err := json.Marshal(res.cred)
	if err != nil {
		t.Fatalf("json(Credential): %v", err)
	}
	out["json(Credential)"] = string(jsonCred)

	// Every fmt verb, on pointer, value, and the bare credential.
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%q"} {
		out["fmt(ptr "+verb+")"] = fmt.Sprintf(verb, res)
		out["fmt(val "+verb+")"] = fmt.Sprintf(verb, *res)
		out["fmt(cred "+verb+")"] = fmt.Sprintf(verb, res.cred)
	}

	// slog through both built-in handlers.
	var jbuf, tbuf bytes.Buffer
	slog.New(slog.NewJSONHandler(&jbuf, nil)).Info("resolution", slog.Any("res", res))
	slog.New(slog.NewTextHandler(&tbuf, nil)).Info("resolution", slog.Any("res", res))
	out["slog(json)"] = jbuf.String()
	out["slog(text)"] = tbuf.String()

	return out
}
