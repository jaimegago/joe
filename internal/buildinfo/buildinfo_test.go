package buildinfo

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"testing/fstest"
)

// referenceDigest recomputes the digest the way an external harness would,
// following ONLY the documented canonical serialization (sorted relative
// paths; per file write path bytes then content bytes into one running sha256).
// If Compute drifts from the documented spec, this independent reimplementation
// diverges and the test fails.
func referenceDigest(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	// insertion sort to avoid depending on the same sort call Compute uses
	for i := 1; i < len(paths); i++ {
		for j := i; j > 0 && paths[j-1] > paths[j]; j-- {
			paths[j-1], paths[j] = paths[j], paths[j-1]
		}
	}
	h := sha256.New()
	for _, p := range paths {
		h.Write([]byte(p))
		h.Write(files[p])
	}
	return hex.EncodeToString(h.Sum(nil))
}

func TestComputeMatchesCanonicalSpec(t *testing.T) {
	files := map[string][]byte{
		"index.html":              []byte("<!doctype html><body>joe</body>"),
		"assets/index-ab12.js":    []byte("console.log('joe')"),
		"assets/index-cd34.css":   []byte("body{margin:0}"),
		".gitkeep":                {},
		"vite.svg":                []byte("<svg/>"),
		"assets/nested/deep.json": []byte(`{"k":"v"}`),
	}
	mapfs := make(fstest.MapFS, len(files))
	for p, c := range files {
		mapfs[p] = &fstest.MapFile{Data: c}
	}

	got, err := Compute(mapfs)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	want := referenceDigest(files)
	if got != want {
		t.Fatalf("digest mismatch:\n got=%s\nwant=%s", got, want)
	}
}

func TestComputeIsDeterministicAndContentSensitive(t *testing.T) {
	base := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("a")},
		"assets/app-1.js": &fstest.MapFile{Data: []byte("b")},
		".gitkeep":        &fstest.MapFile{Data: []byte{}},
	}
	d1, err := Compute(base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	d2, err := Compute(base)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if d1 != d2 {
		t.Fatalf("Compute not deterministic: %s != %s", d1, d2)
	}

	changed := fstest.MapFS{
		"index.html":      &fstest.MapFile{Data: []byte("a")},
		"assets/app-1.js": &fstest.MapFile{Data: []byte("B")}, // one byte differs
		".gitkeep":        &fstest.MapFile{Data: []byte{}},
	}
	d3, err := Compute(changed)
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	if d3 == d1 {
		t.Fatal("digest did not change when an embedded byte changed")
	}
}

func TestGetReportsInjectedDefaults(t *testing.T) {
	// On an unset build the injected fields carry their defaults.
	got := Get()
	if got.Version != Version || got.Commit != Commit || got.BuildTime != BuildTime {
		t.Fatalf("Get did not pass through injected vars: %+v", got)
	}
}
