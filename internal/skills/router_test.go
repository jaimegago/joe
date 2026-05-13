package skills

import (
	"strings"
	"testing"
)

func mustParse(t *testing.T, name, desc string) *Skill {
	t.Helper()
	data := []byte("---\nname: " + name + "\ndescription: " + desc + "\n---\nbody for " + name + "\n")
	s, err := ParseSkill(name+".md", data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func buildRegistry(t *testing.T, skills ...*Skill) *Registry {
	t.Helper()
	reg := NewRegistry()
	for _, s := range skills {
		reg.byName[s.Name] = s
		reg.skills = append(reg.skills, s)
	}
	return reg
}

func TestRouter_NilSafe(t *testing.T) {
	var r *Router
	if got := r.Match("anything"); got != nil {
		t.Errorf("nil router should return nil, got %v", got)
	}
	r = NewRouter(nil)
	if got := r.Match("anything"); got != nil {
		t.Errorf("router over nil registry should return nil, got %v", got)
	}
}

func TestRouter_NoMatch(t *testing.T) {
	reg := buildRegistry(t,
		mustParse(t, "restart-loop", "Diagnose pod CrashLoopBackOff before another restart"),
	)
	r := NewRouter(reg)
	if got := r.Match("the weather today"); len(got) != 0 {
		t.Errorf("expected no match, got %d skills", len(got))
	}
}

func TestRouter_MatchesByDescription(t *testing.T) {
	reg := buildRegistry(t,
		mustParse(t, "restart-loop", "Diagnose pod CrashLoopBackOff and identify failure mode"),
		mustParse(t, "scaling", "Check downstream dependencies before scaling upstream"),
	)
	r := NewRouter(reg)
	got := r.Match("why is my pod in CrashLoopBackOff")
	if len(got) == 0 {
		t.Fatal("expected at least one match")
	}
	if got[0].Name != "restart-loop" {
		t.Errorf("top match = %s, want restart-loop", got[0].Name)
	}
}

func TestRouter_RanksByOverlapAndCapsAtMax(t *testing.T) {
	reg := buildRegistry(t,
		mustParse(t, "alpha", "scaling replicas database saturated"),
		mustParse(t, "beta", "scaling replicas"),
		mustParse(t, "gamma", "completely unrelated topic xyzzy"),
		mustParse(t, "delta", "scaling database"),
		mustParse(t, "epsilon", "scaling"),
	)
	r := NewRouter(reg)
	got := r.Match("scaling replicas of a saturated database")
	if len(got) > MaxActiveSkills {
		t.Fatalf("expected at most %d matches, got %d", MaxActiveSkills, len(got))
	}
	if got[0].Name != "alpha" {
		t.Errorf("top match = %s, want alpha (highest overlap)", got[0].Name)
	}
	for _, s := range got {
		if s.Name == "gamma" {
			t.Error("gamma has no overlap and should not match")
		}
	}
}

func TestRouter_TieBreakByName(t *testing.T) {
	reg := buildRegistry(t,
		mustParse(t, "zulu", "scaling"),
		mustParse(t, "alpha", "scaling"),
		mustParse(t, "mike", "scaling"),
	)
	r := NewRouter(reg)
	got := r.Match("scaling")
	if len(got) != 3 {
		t.Fatalf("expected 3 matches, got %d", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "mike" || got[2].Name != "zulu" {
		t.Errorf("expected alpha,mike,zulu name order on tie; got %s,%s,%s",
			got[0].Name, got[1].Name, got[2].Name)
	}
}

func TestRouter_IgnoresStopWordsAndShortTokens(t *testing.T) {
	reg := buildRegistry(t,
		mustParse(t, "the", "the and for are"), // pure stop words → no real tokens
	)
	r := NewRouter(reg)
	got := r.Match("the and for are")
	if len(got) != 0 {
		t.Errorf("stop-word-only match should yield no hits, got %v", got)
	}
}

func TestRenderPromptSection_Empty(t *testing.T) {
	if RenderPromptSection(nil) != "" {
		t.Error("empty match should render to empty string")
	}
}

func TestRenderPromptSection_IncludesNameDescriptionAndBody(t *testing.T) {
	s := mustParse(t, "restart-loop", "diagnose pod crash loops")
	out := RenderPromptSection([]*Skill{s})
	if !strings.Contains(out, "restart-loop") {
		t.Error("output missing skill name")
	}
	if !strings.Contains(out, "diagnose pod crash loops") {
		t.Error("output missing description")
	}
	if !strings.Contains(out, "body for restart-loop") {
		t.Error("output missing body")
	}
}
