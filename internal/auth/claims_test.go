package auth

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/jaimegago/joe/internal/rbac"
)

// TestPrincipalFromClaims_VerifiedEmail proves the happy path: a verified email
// becomes a user:<email> principal carrying the reserved user: prefix. This is
// the static acceptance criterion ("the user principal carries the user:
// prefix"), expressed behaviourally.
func TestPrincipalFromClaims_VerifiedEmail(t *testing.T) {
	p, err := PrincipalFromClaims(Claims{Email: "alice@example.com", EmailVerified: true})
	if err != nil {
		t.Fatalf("verified email should mint a principal: %v", err)
	}
	if want := rbac.Principal("user:alice@example.com"); p != want {
		t.Fatalf("principal = %q, want %q", p, want)
	}
	if got := string(p); got[:len(rbac.PrefixUser)] != rbac.PrefixUser {
		t.Fatalf("minted principal %q must carry the %q prefix", got, rbac.PrefixUser)
	}
}

// TestPrincipalFromClaims_EmailVerifiedEnforced proves the hard requirement:
// email_verified that is false OR absent yields ErrEmailNotVerified and NO
// principal — the gate runs before any principal is minted (design §2.2).
func TestPrincipalFromClaims_EmailVerifiedEnforced(t *testing.T) {
	cases := []struct {
		name   string
		claims Claims
	}{
		{"explicit false", Claims{Email: "bob@example.com", EmailVerified: false}},
		{"absent (zero value)", Claims{Email: "bob@example.com"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := PrincipalFromClaims(tc.claims)
			if !errors.Is(err, ErrEmailNotVerified) {
				t.Fatalf("want ErrEmailNotVerified, got err=%v principal=%q", err, p)
			}
			if p != "" {
				t.Fatalf("no principal must be minted for an unverified email, got %q", p)
			}
		})
	}
}

// TestClaims_EmailVerifiedAbsentJSON confirms a token JSON with no
// email_verified field decodes to unverified (fail-closed), so the absent case
// is rejected just like explicit false.
func TestClaims_EmailVerifiedAbsentJSON(t *testing.T) {
	var c Claims
	if err := json.Unmarshal([]byte(`{"email":"x@example.com"}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if bool(c.EmailVerified) {
		t.Fatal("absent email_verified must decode to false")
	}
	if _, err := PrincipalFromClaims(c); !errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("absent email_verified must be rejected, got %v", err)
	}
}

// TestClaims_EmailVerifiedStringEncoded covers the Azure-style string-encoded
// boolean: "true"/"false" decode correctly, and an unrecognised string fails
// closed to unverified.
func TestClaims_EmailVerifiedStringEncoded(t *testing.T) {
	cases := map[string]bool{
		`{"email_verified":"true"}`:  true,
		`{"email_verified":"false"}`: false,
		`{"email_verified":"yes"}`:   false, // strconv.ParseBool rejects ⇒ fail closed
	}
	for body, want := range cases {
		var c Claims
		if err := json.Unmarshal([]byte(body), &c); err != nil {
			t.Fatalf("unmarshal %s: %v", body, err)
		}
		if bool(c.EmailVerified) != want {
			t.Errorf("%s: EmailVerified = %v, want %v", body, bool(c.EmailVerified), want)
		}
	}
}

// TestPrincipalFromClaims_ReservedPrefixCollision proves an IdP-supplied email
// that already carries a reserved kind prefix is rejected (impersonation guard,
// design §2.2) — it does not silently mint user:user:... .
func TestPrincipalFromClaims_ReservedPrefixCollision(t *testing.T) {
	_, err := PrincipalFromClaims(Claims{Email: "svc:robot", EmailVerified: true})
	if err == nil {
		t.Fatal("an email colliding with a reserved prefix must be rejected")
	}
	if errors.Is(err, ErrEmailNotVerified) {
		t.Fatalf("collision must fail on the prefix check, not the verified check: %v", err)
	}
}
