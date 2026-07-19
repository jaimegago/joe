package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jaimegago/joe/internal/crypto"
	"github.com/jaimegago/joe/internal/observability"
)

// scanTestStore opens a migrated in-memory store, the shape every other
// migration test in this package uses.
func scanTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(DatabaseConfig{Driver: DriverSQLite, DSN: ":memory:"}, (*observability.Metrics)(nil))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Migrate(); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return s
}

// TestScanComponentConfigs_EmptyDatabaseIsFirstRun pins the answer the boot key
// loader depends on for a fresh install: nothing stored, nothing to lose, so
// minting a key is safe.
func TestScanComponentConfigs_EmptyDatabaseIsFirstRun(t *testing.T) {
	s := scanTestStore(t)

	scan, err := ScanComponentConfigs(context.Background(), s.DB())
	if err != nil {
		t.Fatalf("ScanComponentConfigs() error = %v", err)
	}
	if scan.Total != 0 || scan.Encrypted != 0 {
		t.Errorf("scan = %+v, want zero totals", scan)
	}
}

// TestScanComponentConfigs_DetectsEncryptedThroughTheRealWritePath is the test
// that matters: the encrypted rows are written by the production repository, not
// hand-crafted, so the scan is proven against the actual at-rest shape rather
// than against a fixture that happens to agree with it. The at-rest value is
// JSON-quoted AND lands with BLOB storage class despite the TEXT declaration,
// which is why the detection unmarshals before testing the marker instead of
// doing anything simpler.
func TestScanComponentConfigs_DetectsEncryptedThroughTheRealWritePath(t *testing.T) {
	s := scanTestStore(t)
	ctx := context.Background()

	repo, err := NewEncryptedComponentRepository(s.Components, testKey())
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c-one", "c-two"} {
		c := &Component{ID: id, Type: "prometheus", Name: id, Config: json.RawMessage(`{"token":"secret"}`)}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	scan, err := ScanComponentConfigs(ctx, s.DB())
	if err != nil {
		t.Fatalf("ScanComponentConfigs() error = %v", err)
	}
	if scan.Total != 2 || scan.Encrypted != 2 {
		t.Errorf("scan = %+v, want {Total:2 Encrypted:2}", scan)
	}
}

// TestScanComponentConfigs_PlaintextRowsAreNotEncryptedData pins the boundary
// that decides Rule A. A backward-compatibility install whose configs were never
// encrypted has rows but nothing to lose, so an absent key there is still a
// first run — counting rows instead of counting ENCRYPTED rows would wrongly
// refuse that boot forever.
func TestScanComponentConfigs_PlaintextRowsAreNotEncryptedData(t *testing.T) {
	s := scanTestStore(t)
	ctx := context.Background()

	// Write through the unwrapped repository: no encryption layer, so the config
	// lands as raw JSON — the pre-encryption at-rest shape.
	c := &Component{ID: "c-plain", Type: "prometheus", Name: "plain", Config: json.RawMessage(`{"url":"http://p:9090"}`)}
	if err := s.Components.Create(ctx, c); err != nil {
		t.Fatal(err)
	}

	scan, err := ScanComponentConfigs(ctx, s.DB())
	if err != nil {
		t.Fatalf("ScanComponentConfigs() error = %v", err)
	}
	if scan.Total != 1 {
		t.Errorf("Total = %d, want 1", scan.Total)
	}
	if scan.Encrypted != 0 {
		t.Errorf("Encrypted = %d, want 0 — a plaintext config is not encrypted data", scan.Encrypted)
	}
}

// TestVerifyConfigs_WrongKeyFailsEveryComponent is the coverage SITE-CLAIMS
// recorded as missing: that a wrong key fails to decrypt a component THROUGH THE
// REPOSITORY, not merely at the cipher. It also pins the diagnosability
// mitigation Rule B trades for — every failing component is named, because "all
// of them" is what tells an operator the key is wrong rather than a row damaged.
func TestVerifyConfigs_WrongKeyFailsEveryComponent(t *testing.T) {
	ctx := context.Background()
	inner := newMockRepo()

	keyA := testKey()
	keyB := make([]byte, 32)
	for i := range keyB {
		keyB[i] = byte(255 - i)
	}

	repoA, err := NewEncryptedComponentRepository(inner, keyA)
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{"c-one", "c-two", "c-three"}
	for _, id := range ids {
		c := &Component{ID: id, Type: "prometheus", Name: id, Config: json.RawMessage(`{"token":"secret"}`)}
		if err := repoA.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	// The same rows read under a different key: the lost-key disaster.
	repoB, err := NewEncryptedComponentRepository(inner, keyB)
	if err != nil {
		t.Fatal(err)
	}
	err = repoB.VerifyConfigs(ctx)
	if err == nil {
		t.Fatal("VerifyConfigs() with the wrong key returned nil; boot would have proceeded")
	}

	var authErr *ConfigAuthError
	if !errors.As(err, &authErr) {
		t.Fatalf("error = %v, want it to reach a *ConfigAuthError", err)
	}
	if !errors.Is(err, crypto.ErrAuthentication) {
		t.Errorf("error = %v, want it to carry crypto.ErrAuthentication", err)
	}
	for _, id := range ids {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("error %q does not name failing component %q — the enumeration is what makes the refusal diagnosable", err, id)
		}
	}
}

// TestVerifyConfigs_NamesOnlyTheDamagedRow pins the other half of the same
// mitigation. One failing component out of several is a damaged row, not a wrong
// key, and the error must say so by naming that row and only that row.
func TestVerifyConfigs_NamesOnlyTheDamagedRow(t *testing.T) {
	ctx := context.Background()
	inner := newMockRepo()
	key := testKey()

	repo, err := NewEncryptedComponentRepository(inner, key)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"c-good", "c-damaged"} {
		c := &Component{ID: id, Type: "prometheus", Name: id, Config: json.RawMessage(`{"token":"secret"}`)}
		if err := repo.Create(ctx, c); err != nil {
			t.Fatal(err)
		}
	}

	// Corrupt one stored ciphertext in place, leaving it structurally valid: a
	// well-formed JSON string carrying a well-formed enc: value that no longer
	// authenticates. This is the row-damage case, distinct from a wrong key.
	stored, err := inner.Get(ctx, "c-damaged")
	if err != nil {
		t.Fatal(err)
	}
	corrupted, err := crypto.Encrypt(otherKey(), []byte(`{"token":"secret"}`))
	if err != nil {
		t.Fatal(err)
	}
	requoted, err := json.Marshal(corrupted)
	if err != nil {
		t.Fatal(err)
	}
	stored.Config = requoted
	if err := inner.Update(ctx, stored); err != nil {
		t.Fatal(err)
	}

	err = repo.VerifyConfigs(ctx)
	if err == nil {
		t.Fatal("VerifyConfigs() returned nil for a damaged row")
	}
	if !strings.Contains(err.Error(), "c-damaged") {
		t.Errorf("error %q does not name the damaged component", err)
	}
	if strings.Contains(err.Error(), "c-good") {
		t.Errorf("error %q names the intact component; only failures belong in the enumeration", err)
	}
}

// TestVerifyConfigs_PassesWhenKeyMatches pins the success path — the condition
// under which boot proceeds and the "component credential encryption enabled"
// log line is true.
func TestVerifyConfigs_PassesWhenKeyMatches(t *testing.T) {
	ctx := context.Background()
	inner := newMockRepo()
	repo, err := NewEncryptedComponentRepository(inner, testKey())
	if err != nil {
		t.Fatal(err)
	}
	c := &Component{ID: "c-ok", Type: "prometheus", Name: "ok", Config: json.RawMessage(`{"token":"secret"}`)}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatal(err)
	}

	if err := repo.VerifyConfigs(ctx); err != nil {
		t.Errorf("VerifyConfigs() error = %v, want nil", err)
	}
}

// otherKey is a second valid 32-byte key, distinct from testKey().
func otherKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(255 - i)
	}
	return k
}
