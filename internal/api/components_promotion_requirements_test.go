package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/credential"
	"github.com/jaimegago/joe/internal/rbac"
)

// A002 promotion-requirements describe surface. These tests pin two things: the
// describe-only requirements table agrees, case-for-case, with buildArmedConfig's
// LIVE enforcement (the safety basis for leaving buildArmedConfig untouched), and
// the read endpoint exposes the reference SHAPE without leaking any config,
// locator value, or credential material.

// presentLocators maps a promoteComponentRequest to the set of supplied locator
// field names the requirements predicate consumes, from the SAME request the
// handler validates — so the table path and the enforcement path cannot drift on
// what "supplied" means.
func presentLocators(req promoteComponentRequest) map[string]bool {
	p := map[string]bool{}
	if req.Value != "" {
		p["value"] = true
	}
	if req.EnvVar != "" {
		p["env_var"] = true
	}
	if req.Kubeconfig != "" {
		p["kubeconfig"] = true
	}
	if req.Context != "" {
		p["context"] = true
	}
	if req.InCluster {
		p["in_cluster"] = true
	}
	if req.Audience != "" {
		p["audience"] = true
	}
	if req.CredentialProvider != "" {
		p["credential_provider"] = true
	}
	return p
}

// TestPromotionRequirements_TableMatchesEnforcement is the STRONG guard: it drives
// representative references through BOTH the describe-only table predicate AND the
// real buildArmedConfig, and fails if they ever disagree on accept/reject. This is
// what makes the describe-only table safe while buildArmedConfig stays the sole
// enforcement authority — if the table claims a rule the handler does not enforce
// (or vice versa), this test breaks.
func TestPromotionRequirements_TableMatchesEnforcement(t *testing.T) {
	cases := []struct {
		name string
		kind credential.Kind
		req  promoteComponentRequest
		want bool // expected accept
	}{
		// static
		{"static valid env_var", credential.KindStatic, promoteComponentRequest{EnvVar: "TOK"}, true},
		{"static inline value rejected", credential.KindStatic, promoteComponentRequest{EnvVar: "TOK", Value: "secret"}, false},
		{"static value-only rejected", credential.KindStatic, promoteComponentRequest{Value: "secret"}, false},
		{"static missing env_var rejected", credential.KindStatic, promoteComponentRequest{}, false},
		{"static kube-field contamination rejected", credential.KindStatic, promoteComponentRequest{EnvVar: "TOK", Kubeconfig: "/k"}, false},
		// kubeconfig-exec
		{"kexec in_cluster valid", credential.KindKubeconfigExec, promoteComponentRequest{InCluster: true}, true},
		{"kexec kubeconfig valid", credential.KindKubeconfigExec, promoteComponentRequest{Kubeconfig: "/k"}, true},
		// LIVE rule is at-least-one-of, NOT exactly-one — both set is accepted.
		{"kexec both valid", credential.KindKubeconfigExec, promoteComponentRequest{InCluster: true, Kubeconfig: "/k"}, true},
		{"kexec context-only rejected", credential.KindKubeconfigExec, promoteComponentRequest{Context: "ctx"}, false},
		{"kexec neither rejected", credential.KindKubeconfigExec, promoteComponentRequest{}, false},
		{"kexec static-field contamination rejected", credential.KindKubeconfigExec, promoteComponentRequest{InCluster: true, EnvVar: "TOK"}, false},
		// static-bearer (kubernetes): coordinates + an env_var OR in_cluster token source.
		{"bearer env_var valid", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443", EnvVar: "TOK"}, true},
		{"bearer in_cluster valid", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443", InCluster: true}, true},
		// LIVE rule is at-least-one-of token source — both set is accepted.
		{"bearer both sources valid", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443", EnvVar: "TOK", InCluster: true}, true},
		{"bearer neither source rejected", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443"}, false},
		{"bearer inline value rejected", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443", EnvVar: "TOK", Value: "secret"}, false},
		{"bearer kubeconfig contamination rejected", credential.KindStaticBearer, promoteComponentRequest{AuthMethod: "static-bearer", APIServer: "https://k8s:6443", EnvVar: "TOK", Kubeconfig: "/k"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqs, ok := credential.PromotionRequirements(tc.kind)
			if !ok {
				t.Fatalf("no requirements entry for kind %q", tc.kind)
			}
			tableErr := reqs.ValidateReference(presentLocators(tc.req))
			tableAccept := tableErr == nil

			// Real enforcement path — buildArmedConfig is UNTOUCHED by this node.
			_, _, enfErr := buildArmedConfig(json.RawMessage(`{}`), tc.kind, tc.req)
			enfAccept := enfErr == nil

			if tableAccept != enfAccept {
				t.Fatalf("table/enforcement DISAGREE: table accept=%v (err=%v); buildArmedConfig accept=%v (err=%v)",
					tableAccept, tableErr, enfAccept, enfErr)
			}
			if tableAccept != tc.want {
				t.Errorf("accept=%v, want %v (table err=%v, enforce err=%v)", tableAccept, tc.want, tableErr, enfErr)
			}
		})
	}
}

// fieldRequirement mirrors credential.FieldRequirement for decoding the endpoint
// response.
type fieldRequirement struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
}

type promotionRequirementsBody struct {
	Type          string             `json:"type"`
	Wired         bool               `json:"wired"`
	Kind          string             `json:"kind"`
	LocatorFields []fieldRequirement `json:"locator_fields"`
	Constraints   []struct {
		Rule    string   `json:"rule"`
		Fields  []string `json:"fields"`
		Message string   `json:"message"`
	} `json:"constraints"`
	ArmableTypes []string `json:"armable_types"`
}

func getPromotionRequirements(t *testing.T, f *llmadminFixture, id string, principal rbac.Principal) (*promotionRequirementsBody, string) {
	t.Helper()
	w := f.do(http.MethodGet, "/api/v1/components/"+id+"/promotion-requirements", "", principal)
	if w.Code != http.StatusOK {
		t.Fatalf("GET promotion-requirements %s: status=%d body=%s; want 200", id, w.Code, w.Body.String())
	}
	var body promotionRequirementsBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode promotion-requirements body: %v (raw=%s)", err, w.Body.String())
	}
	return &body, w.Body.String()
}

// TestPromotionRequirements_StaticShape proves a static-wired component describes
// kind=static with env_var required and the inline-value-forbidden constraint.
func TestPromotionRequirements_StaticShape(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	body, _ := getPromotionRequirements(t, f, "c-gh", "user:alice")
	if !body.Wired || body.Kind != "static" || body.Type != "github" {
		t.Fatalf("static shape: wired=%v kind=%q type=%q; want true/static/github", body.Wired, body.Kind, body.Type)
	}
	if len(body.LocatorFields) != 1 || body.LocatorFields[0].Name != "env_var" || !body.LocatorFields[0].Required {
		t.Errorf("static locator_fields=%+v; want exactly [{env_var required}]", body.LocatorFields)
	}
	// credential_provider and audience must never appear as locator fields.
	for _, lf := range body.LocatorFields {
		if lf.Name == "credential_provider" || lf.Name == "audience" || lf.Name == "value" {
			t.Errorf("static locator_fields leaked non-locator field %q", lf.Name)
		}
	}
	foundForbid := false
	for _, c := range body.Constraints {
		if c.Rule == credential.ConstraintForbidInlineValue {
			foundForbid = true
			if len(c.Fields) != 1 || c.Fields[0] != "value" {
				t.Errorf("forbid-inline-value constraint fields=%v; want [value]", c.Fields)
			}
		}
	}
	if !foundForbid {
		t.Errorf("static constraints missing %q: %+v", credential.ConstraintForbidInlineValue, body.Constraints)
	}
}

// TestPromotionRequirements_StaticBearerShape proves a static-bearer-wired
// component (kubernetes) describes its two optional token-source locators and the
// at-least-one-of rule over them.
func TestPromotionRequirements_StaticBearerShape(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-k8s", "kubernetes", `{}`)

	body, _ := getPromotionRequirements(t, f, "c-k8s", "user:alice")
	if !body.Wired || body.Kind != "static-bearer" || body.Type != "kubernetes" {
		t.Fatalf("static-bearer shape: wired=%v kind=%q type=%q; want true/static-bearer/kubernetes", body.Wired, body.Kind, body.Type)
	}
	got := map[string]bool{}
	for _, lf := range body.LocatorFields {
		if lf.Required {
			t.Errorf("static-bearer field %q marked required; the requirement is the cross-field at-least-one-of rule, not a per-field flag", lf.Name)
		}
		got[lf.Name] = true
	}
	for _, want := range []string{"env_var", "in_cluster"} {
		if !got[want] {
			t.Errorf("static-bearer locator_fields missing %q: %+v", want, body.LocatorFields)
		}
	}
	foundOneOf := false
	for _, c := range body.Constraints {
		if c.Rule == credential.ConstraintAtLeastOneOf {
			foundOneOf = true
			sort.Strings(c.Fields)
			if strings.Join(c.Fields, ",") != "env_var,in_cluster" {
				t.Errorf("at-least-one-of fields=%v; want [env_var in_cluster]", c.Fields)
			}
		}
	}
	if !foundOneOf {
		t.Errorf("static-bearer constraints missing %q: %+v", credential.ConstraintAtLeastOneOf, body.Constraints)
	}
}

// TestPromotionRequirements_NoValueLeakage proves the endpoint describes the SHAPE
// only: even after a component is armed with a real reference, the GET response
// echoes no stored locator VALUE (only field names) and no credential material.
func TestPromotionRequirements_NoValueLeakage(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-k8s", "kubernetes", `{}`)

	const canary = "CANARY_LOCATOR_VALUE"
	if w := f.do(http.MethodPost, "/api/v1/components/c-k8s/promote",
		`{"auth_method":"static-bearer","api_server":"https://k8s:6443","env_var":"`+canary+`","audience":"`+canary+`"}`, "user:alice"); w.Code != http.StatusOK {
		t.Fatalf("arm c-k8s: status=%d body=%s", w.Code, w.Body.String())
	}

	_, raw := getPromotionRequirements(t, f, "c-k8s", "user:alice")
	if strings.Contains(raw, canary) {
		t.Errorf("promotion-requirements leaked a stored locator value: response contained %q\n%s", canary, raw)
	}
}

// TestPromotionRequirements_UnwiredSorted proves an unwired type answers the
// capability question with 200, wired:false, and a SORTED armable_types list.
func TestPromotionRequirements_UnwiredSorted(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-dd", "datadog", `{"site":"datadoghq.com"}`)

	body, _ := getPromotionRequirements(t, f, "c-dd", "user:alice")
	if body.Wired || body.Type != "datadog" {
		t.Fatalf("unwired shape: wired=%v type=%q; want false/datadog", body.Wired, body.Type)
	}
	if len(body.ArmableTypes) == 0 {
		t.Fatalf("armable_types empty; want the sorted wired-type set")
	}
	if !sort.StringsAreSorted(body.ArmableTypes) {
		t.Errorf("armable_types not sorted: %v", body.ArmableTypes)
	}
	// armable_types must equal the wired-type registry, sorted.
	want := credential.WiredTypes()
	sort.Strings(want)
	if strings.Join(body.ArmableTypes, ",") != strings.Join(want, ",") {
		t.Errorf("armable_types=%v; want %v", body.ArmableTypes, want)
	}
}

// TestPromotionRequirements_AdminGated proves a non-admin caller is refused like
// the promote handler (403), so the describe surface is admin-gated too.
func TestPromotionRequirements_AdminGated(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	registerComponent(t, f, "c-gh", "github", `{}`)

	w := f.do(http.MethodGet, "/api/v1/components/c-gh/promotion-requirements", "", "user:bob")
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin promotion-requirements: status=%d body=%s; want 403", w.Code, w.Body.String())
	}
}

// TestPromotionRequirements_NotFound proves an unknown component is a 404.
func TestPromotionRequirements_NotFound(t *testing.T) {
	f := newLLMAdminFixture(t, true)
	f.markAdmin("user:alice")
	w := f.do(http.MethodGet, "/api/v1/components/does-not-exist/promotion-requirements", "", "user:alice")
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing component: status=%d body=%s; want 404", w.Code, w.Body.String())
	}
}
